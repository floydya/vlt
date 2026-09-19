package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"vlt/internal/cli"
	"vlt/internal/config"
	"vlt/internal/profile"
)

func TestNewDispatcherWiresProfileManagement(t *testing.T) {
	var stdout bytes.Buffer
	dispatcher := newDispatcherAt(t.TempDir(), strings.NewReader(""), &stdout, io.Discard)

	if err := dispatcher.Dispatch(context.Background(), []string{"profile", "list"}); err != nil {
		t.Fatalf("profile list error = %v", err)
	}
	if got, want := stdout.String(), "#  ACTIVE  NAME  ADDRESS  NAMESPACE\n"; got != want {
		t.Errorf("profile list output = %q, want %q", got, want)
	}
	stdout.Reset()

	if err := dispatcher.Dispatch(context.Background(), []string{"switch"}); err != nil {
		t.Fatalf("switch error = %v", err)
	}
	if got, want := stdout.String(), "Active profile: none\n#  ACTIVE  NAME  ADDRESS  NAMESPACE\n"; got != want {
		t.Errorf("switch output = %q, want %q", got, want)
	}
}

func TestDispatchReturnsDelegatedExitCodeWithoutRetry(t *testing.T) {
	calls := 0
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output:  &bytes.Buffer{},
		Profile: unavailable("profile management"),
		Switch:  unavailable("profile switching"),
		Vault: func(context.Context, []string) error {
			calls++
			return mainExitError(23)
		},
	})
	var stderr bytes.Buffer

	exitCode := dispatch(context.Background(), dispatcher, []string{"read", "secret/example"}, &stderr)

	if exitCode != 23 {
		t.Errorf("exit code = %d, want 23", exitCode)
	}
	if calls != 1 {
		t.Errorf("delegated calls = %d, want exactly 1", calls)
	}
	if got := stderr.String(); !strings.Contains(got, "vlt: delegated failure") {
		t.Errorf("stderr = %q, want delegated failure", got)
	}
}

func TestDispatchReturnsSuccessWithoutErrorOutput(t *testing.T) {
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output:  &bytes.Buffer{},
		Profile: unavailable("profile management"),
		Switch:  unavailable("profile switching"),
		Vault:   func(context.Context, []string) error { return nil },
	})
	var stderr bytes.Buffer

	exitCode := dispatch(context.Background(), dispatcher, []string{"status"}, &stderr)

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestDispatchWritesProfileHelpToStdout(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output:  &stdout,
		Profile: cli.NewProfileHandler(cli.ProfileDependencies{Output: &stdout}),
		Switch:  unavailable("profile switching"),
		Vault:   unavailable("Vault delegation"),
	})

	exitCode := dispatch(context.Background(), dispatcher, []string{"profile", "--help"}, &stderr)

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout.String(), "Manage Vault profiles") {
		t.Errorf("stdout = %q, want profile help", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestDispatchWritesProfileGuidanceToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output:  &stdout,
		Profile: cli.NewProfileHandler(cli.ProfileDependencies{Output: &stdout}),
		Switch:  unavailable("profile switching"),
		Vault:   unavailable("Vault delegation"),
	})

	exitCode := dispatch(context.Background(), dispatcher, []string{"profile", "udpate"}, &stderr)

	if exitCode == 0 {
		t.Fatal("exit code = 0, want failure")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{"Did you mean \"update\"?", "Usage: vlt profile"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want text %q", stderr.String(), want)
		}
	}
}

func TestNewDispatcherWiresCompletionScripts(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var stdout bytes.Buffer
			dispatcher := newDispatcherAt(t.TempDir(), strings.NewReader(""), &stdout, io.Discard)

			if err := dispatcher.Dispatch(context.Background(), []string{"completion", shell}); err != nil {
				t.Fatalf("completion %s error = %v", shell, err)
			}
			if stdout.Len() == 0 || !strings.Contains(stdout.String(), "vlt") {
				t.Errorf("completion %s output = %q, want script", shell, stdout.String())
			}
		})
	}
}

func TestDispatchWritesCompletionFailureToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output:     &stdout,
		Profile:    unavailable("profile management"),
		Switch:     unavailable("profile switching"),
		Completion: cli.NewCompletionHandler(cli.CompletionDependencies{Output: &stdout}),
		Vault:      unavailable("Vault delegation"),
	})

	exitCode := dispatch(context.Background(), dispatcher, []string{"completion", "powershell"}, &stderr)

	if exitCode == 0 {
		t.Fatal("exit code = 0, want failure")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{"unsupported shell", "Usage: vlt completion"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want text %q", stderr.String(), want)
		}
	}
}

func TestNewDispatcherWiresCompletionCandidates(t *testing.T) {
	configDirectory := t.TempDir()
	store := config.NewStore(filepath.Join(configDirectory, "vlt", "profiles.json"))
	if err := store.Save(context.Background(), config.Configuration{Profiles: []profile.Profile{
		{Name: "team-b", Address: "https://vault.example.net", Username: "bob", AuthPath: "oidc"},
		{Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc"},
	}}); err != nil {
		t.Fatalf("save profiles: %v", err)
	}
	var stdout bytes.Buffer
	dispatcher := newDispatcherAt(configDirectory, strings.NewReader(""), &stdout, io.Discard)

	if err := dispatcher.Dispatch(context.Background(), []string{"completion", "__profiles"}); err != nil {
		t.Fatalf("completion candidates error = %v", err)
	}
	if got, want := stdout.String(), "team-a\nteam-b\n"; got != want {
		t.Errorf("completion candidates = %q, want %q", got, want)
	}
}

type mainExitError int

func (e mainExitError) Error() string { return "delegated failure" }
func (e mainExitError) ExitCode() int { return int(e) }
