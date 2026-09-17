package vaultexec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

type recordingRunner struct {
	command Command
	result  Result
	err     error
	calls   int
}

func (r *recordingRunner) Run(_ context.Context, command Command) (Result, error) {
	r.calls++
	r.command = command
	return r.result, r.err
}

func TestExecutorPreservesArgumentVectorAndProfileEnvironment(t *testing.T) {
	runner := &recordingRunner{}
	executor := NewExecutor(Dependencies{
		LookPath: func(name string) (string, error) {
			if name != "vault" {
				t.Fatalf("LookPath(%q), want vault", name)
			}
			return "/opt/bin/vault", nil
		},
		Environment: func() []string {
			return []string{
				"PATH=/opt/bin",
				"VAULT_ADDR=https://inherited.example",
				"VAULT_TOKEN=inherited-token",
				"VAULT_NAMESPACE=inherited-namespace",
				"OTHER=preserved",
			}
		},
		Runner: runner,
	})
	arguments := []string{"kv", "put", "secret/a b", "value=$(touch /tmp/not-run)", "; echo unsafe"}

	_, err := executor.Execute(context.Background(), Invocation{
		Arguments: arguments,
		Environment: ProfileEnvironment(
			"https://profile.example",
			"test-profile-token",
			"",
		),
		Mode: Delegated,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if runner.command.Path != "/opt/bin/vault" {
		t.Errorf("command path = %q, want /opt/bin/vault", runner.command.Path)
	}
	if !reflect.DeepEqual(runner.command.Arguments, arguments) {
		t.Errorf("arguments = %#v, want unchanged %#v", runner.command.Arguments, arguments)
	}
	wantEnvironment := []string{
		"PATH=/opt/bin",
		"OTHER=preserved",
		"VAULT_ADDR=https://profile.example",
		"VAULT_TOKEN=test-profile-token",
	}
	if !reflect.DeepEqual(runner.command.Environment, wantEnvironment) {
		t.Errorf("environment = %#v, want %#v", runner.command.Environment, wantEnvironment)
	}
	if got := strings.Join(runner.command.Arguments, " "); !strings.Contains(got, "$(touch /tmp/not-run)") {
		t.Errorf("shell metacharacters changed: %q", got)
	}
}

func TestExecutorEnvironmentOverlayIsCaseInsensitive(t *testing.T) {
	runner := &recordingRunner{}
	executor := NewExecutor(Dependencies{
		LookPath: func(string) (string, error) { return `C:\\vault.exe`, nil },
		Environment: func() []string {
			return []string{
				"Vault_Addr=https://inherited.example",
				"vault_token=inherited-token",
				"VaUlT_NaMeSpAcE=inherited-namespace",
				"OTHER=preserved",
			}
		},
		Runner: runner,
	})

	_, err := executor.Execute(context.Background(), Invocation{
		Environment: ProfileEnvironment("https://profile.example", "profile-token", ""),
		Mode:        Delegated,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := []string{
		"OTHER=preserved",
		"VAULT_ADDR=https://profile.example",
		"VAULT_TOKEN=profile-token",
	}
	if !reflect.DeepEqual(runner.command.Environment, want) {
		t.Fatalf("environment = %#v, want %#v", runner.command.Environment, want)
	}
}

func TestExecutorDelegatesStreams(t *testing.T) {
	runner := &recordingRunner{}
	executor := newTestExecutor(runner)
	stdin := strings.NewReader("input")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	_, err := executor.Execute(context.Background(), Invocation{
		Arguments: []string{"read", "secret/example"},
		Mode:      Delegated,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if runner.command.Stdin != stdin || runner.command.Stdout != stdout || runner.command.Stderr != stderr {
		t.Fatal("Execute() did not pass delegated streams to the runner")
	}
}

func TestExecutorReturnsCapturedOutputAndPreservesNonZeroExit(t *testing.T) {
	wantErr := exitError(23)
	runner := &recordingRunner{
		result: Result{Stdout: []byte("output"), Stderr: []byte("permission denied")},
		err:    wantErr,
	}
	executor := newTestExecutor(runner)

	result, err := executor.Execute(context.Background(), Invocation{
		Arguments: []string{"token", "lookup", "-format=json"},
		Mode:      Captured,
	})
	if errors.Is(err, wantErr) {
		t.Fatalf("Execute() error retained original runner error: %v", err)
	}
	if ExitCode(err) != 23 {
		t.Errorf("ExitCode(error) = %d, want 23", ExitCode(err))
	}
	if string(result.Stdout) != "output" || string(result.Stderr) != "permission denied" {
		t.Errorf("result = %#v, want captured output", result)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %q, want captured diagnostic", err)
	}
}

func TestRedactDiagnosticRemovesTokenLikeValues(t *testing.T) {
	const knownSecret = "opaque-secret-value"
	input := `VAULT_TOKEN=hvs.environment-secret token='s.legacy-secret' {"client_token":"json-secret"} ` + knownSecret
	got := RedactDiagnostic(input, knownSecret)
	for _, secret := range []string{"hvs.environment-secret", "s.legacy-secret", "json-secret", knownSecret} {
		if strings.Contains(got, secret) {
			t.Errorf("RedactDiagnostic() leaked %q in %q", secret, got)
		}
	}
	if count := strings.Count(got, RedactedValue); count != 4 {
		t.Errorf("RedactDiagnostic() markers = %d, want 4 in %q", count, got)
	}
}

func TestRedactDiagnosticRemovesOpaqueCredentialsAfterArrowSeparators(t *testing.T) {
	canaries := []string{"opaque-arrow-secret", "quoted-arrow-secret", "client-arrow-secret"}
	input := "token -> " + canaries[0] + ` VAULT_TOKEN->'` + canaries[1] + `' client_token -> "` + canaries[2] + `"`
	got := RedactDiagnostic(input)
	for _, canary := range canaries {
		if strings.Contains(got, canary) {
			t.Errorf("RedactDiagnostic() leaked %q in %q", canary, got)
		}
	}
	if count := strings.Count(got, RedactedValue); count != len(canaries) {
		t.Errorf("RedactDiagnostic() markers = %d, want %d in %q", count, len(canaries), got)
	}
}

func TestExecutorRedactsSensitiveFailureDiagnostics(t *testing.T) {
	const token = "hvs.test-profile-secret"
	runner := &recordingRunner{
		result: Result{Stderr: []byte("request failed: VAULT_TOKEN=" + token + ` client_token":"another-secret"`)},
		err:    exitError(2),
	}
	executor := newTestExecutor(runner)

	result, err := executor.Execute(context.Background(), Invocation{
		Arguments:       []string{"token", "lookup"},
		Mode:            Captured,
		SensitiveValues: []string{token},
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want failure")
	}
	for _, secret := range []string{token, "another-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %q", secret, err)
		}
		if strings.Contains(string(result.Stderr), secret) {
			t.Fatalf("captured diagnostic leaked %q: %q", secret, result.Stderr)
		}
	}
	if !strings.Contains(err.Error(), RedactedValue) {
		t.Errorf("error = %q, want redaction marker", err)
	}
	// Captured output remains available to its machine-readable consumer; only
	// diagnostics derived from it are safe for display.
	if got := RedactDiagnostic(string(runner.result.Stderr), token); strings.Contains(got, token) {
		t.Errorf("RedactDiagnostic() leaked token: %q", got)
	}
}

func TestProcessErrorDoesNotExposeOriginalRunnerError(t *testing.T) {
	const canary = "opaque-runner-error-canary"
	runnerErr := &sensitiveExitError{code: 31, message: "runner failed with " + canary}
	executor := newTestExecutor(&recordingRunner{err: runnerErr})

	_, err := executor.Execute(context.Background(), Invocation{
		Mode:            Captured,
		SensitiveValues: []string{canary},
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want failure")
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("Execute() error leaked runner canary: %q", err)
	}
	for unwrapped := errors.Unwrap(err); unwrapped != nil; unwrapped = errors.Unwrap(unwrapped) {
		if strings.Contains(unwrapped.Error(), canary) {
			t.Fatalf("unwrap chain leaked runner canary: %q", unwrapped)
		}
	}
	if errors.Is(err, runnerErr) {
		t.Fatal("ProcessError retained the original runner error in its unwrap chain")
	}
	if got := ExitCode(err); got != 31 {
		t.Fatalf("ExitCode(error) = %d, want 31", got)
	}
}

func TestExecutorAutomaticallyRedactsProfileToken(t *testing.T) {
	const token = "opaque-profile-secret"
	runner := &recordingRunner{
		result: Result{Stderr: []byte("request failed with " + token)},
		err:    exitError(2),
	}
	executor := newTestExecutor(runner)

	result, err := executor.Execute(context.Background(), Invocation{
		Environment: ProfileEnvironment("https://vault.example.com", token, ""),
		Mode:        Captured,
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want failure")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(string(result.Stderr), token) {
		t.Fatalf("Execute() leaked profile token: error=%q stderr=%q", err, result.Stderr)
	}
}

func TestExecutorAutomaticallyRedactsCaseInsensitiveVaultTokenOverlay(t *testing.T) {
	for _, key := range []string{"vault_token", "VaUlT_ToKeN"} {
		t.Run(key, func(t *testing.T) {
			const token = "opaque-overlay-token-canary"
			const runnerCanary = "runner included opaque-overlay-token-canary"
			const stderrCanary = "stderr included opaque-overlay-token-canary"
			runner := &recordingRunner{
				result: Result{Stderr: []byte(stderrCanary)},
				err:    &sensitiveExitError{code: 2, message: runnerCanary},
			}
			executor := newTestExecutor(runner)

			result, err := executor.Execute(context.Background(), Invocation{
				Environment: EnvironmentOverlay{Set: map[string]string{key: token}},
				Mode:        Captured,
			})
			if err == nil {
				t.Fatal("Execute() error = nil, want failure")
			}
			if strings.Contains(err.Error(), token) {
				t.Fatalf("Execute() error leaked mixed-case overlay token: %q", err)
			}
			if strings.Contains(string(result.Stderr), token) {
				t.Fatalf("captured stderr leaked mixed-case overlay token: %q", result.Stderr)
			}
		})
	}
}

func TestExecutorFailsActionablyBeforeStartingWhenVaultIsMissing(t *testing.T) {
	runner := &recordingRunner{}
	executor := NewExecutor(Dependencies{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Environment: func() []string {
			t.Fatal("environment read before Vault discovery succeeded")
			return nil
		},
		Runner: runner,
	})

	_, err := executor.Execute(context.Background(), Invocation{Arguments: []string{"status"}})
	if err == nil {
		t.Fatal("Execute() error = nil, want missing Vault error")
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
	for _, text := range []string{"Vault CLI", "PATH", "install"} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("error = %q, want actionable text %q", err, text)
		}
	}
}

func TestOSRunnerUsesArgumentVectorAndSupportsStreamModes(t *testing.T) {
	runner := OSRunner{}
	path, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	arguments := []string{"-test.run=TestVaultExecHelperProcess", "--", "first", "a b", "$(echo not-a-shell)", "; exit 99"}

	captured, err := runner.Run(context.Background(), Command{
		Path:        path,
		Arguments:   arguments,
		Environment: append(os.Environ(), "GO_WANT_VAULTEXEC_HELPER=1"),
		Mode:        Captured,
	})
	if err != nil {
		t.Fatalf("captured Run() error = %v; stderr = %q", err, captured.Stderr)
	}
	if got, want := string(captured.Stdout), "first\na b\n$(echo not-a-shell)\n; exit 99\n"; got != want {
		t.Errorf("captured stdout = %q, want %q", got, want)
	}

	var stdout, stderr bytes.Buffer
	_, err = runner.Run(context.Background(), Command{
		Path:        path,
		Arguments:   arguments,
		Environment: append(os.Environ(), "GO_WANT_VAULTEXEC_HELPER=1"),
		Mode:        Delegated,
		Stdin:       strings.NewReader("delegated input\n"),
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	if err != nil {
		t.Fatalf("delegated Run() error = %v; stderr = %q", err, stderr.String())
	}
	if got, want := stdout.String(), "first\na b\n$(echo not-a-shell)\n; exit 99\nstdin: delegated input\n"; got != want {
		t.Errorf("delegated stdout = %q, want %q", got, want)
	}
	if got, want := stderr.String(), "helper stderr\n"; got != want {
		t.Errorf("delegated stderr = %q, want %q", got, want)
	}
}

func TestOSRunnerPreservesExplicitlyEmptyEnvironment(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	t.Setenv("GO_VAULTEXEC_PARENT_ONLY", "must-not-be-inherited")

	result, err := (OSRunner{}).Run(context.Background(), Command{
		Path:        path,
		Arguments:   []string{"-test.run=^TestVaultExecEmptyEnvironmentHelper$", "--", "empty-environment-helper"},
		Environment: []string{},
		Mode:        Captured,
	})
	if err != nil {
		t.Fatalf("Run() error = %v; stderr = %q", err, result.Stderr)
	}
}

func TestVaultExecEmptyEnvironmentHelper(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "empty-environment-helper" {
		return
	}
	if _, inherited := os.LookupEnv("GO_VAULTEXEC_PARENT_ONLY"); inherited {
		os.Exit(42)
	}
	os.Exit(0)
}

func TestOSRunnerPreservesNonZeroExitStatus(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	result, err := (OSRunner{}).Run(context.Background(), Command{
		Path:        path,
		Arguments:   []string{"-test.run=TestVaultExecNonZeroHelper"},
		Environment: append(os.Environ(), "GO_WANT_VAULTEXEC_NONZERO_HELPER=1"),
		Mode:        Captured,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want non-zero exit")
	}
	if got := ExitCode(err); got != 19 {
		t.Errorf("ExitCode(error) = %d, want 19; stderr = %q", got, result.Stderr)
	}
}

func TestVaultExecNonZeroHelper(t *testing.T) {
	if os.Getenv("GO_WANT_VAULTEXEC_NONZERO_HELPER") == "1" {
		os.Exit(19)
	}
}

func TestVaultExecHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_VAULTEXEC_HELPER") != "1" {
		return
	}
	separator := 0
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index + 1
			break
		}
	}
	for _, argument := range os.Args[separator:] {
		_, _ = os.Stdout.WriteString(argument + "\n")
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		_, _ = os.Stderr.WriteString("read stdin: " + err.Error() + "\n")
		os.Exit(97)
	}
	if len(input) > 0 {
		_, _ = os.Stdout.WriteString("stdin: " + string(input))
	}
	_, _ = os.Stderr.WriteString("helper stderr\n")
	os.Exit(0)
}

type exitError int

func (e exitError) Error() string { return "test process failed" }
func (e exitError) ExitCode() int { return int(e) }

type sensitiveExitError struct {
	code    int
	message string
}

func (e *sensitiveExitError) Error() string { return e.message }
func (e *sensitiveExitError) ExitCode() int { return e.code }

func newTestExecutor(runner Runner) *Executor {
	return NewExecutor(Dependencies{
		LookPath:    func(string) (string, error) { return "/test/vault", nil },
		Environment: func() []string { return []string{"BASE=value"} },
		Runner:      runner,
	})
}
