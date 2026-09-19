package cli

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"vlt/internal/credential"
	"vlt/internal/vaultexec"
)

func TestFakeBackedPreflightRenewsBeforeDelegation(t *testing.T) {
	const token = "synthetic-renewal-token"
	now := time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC)
	harness := newComponentHarness(t, now)
	harness.addStoredProfile(t, "team-a", "https://team-a.example", token)
	harness.vaultRunner.run = func(command vaultexec.Command) (vaultexec.Result, error) {
		switch command.Arguments[0] {
		case "token":
			if command.Arguments[1] == "lookup" {
				return vaultexec.Result{Stdout: []byte(`{"data":{"ttl":60,"renewable":true}}`)}, nil
			}
			return vaultexec.Result{Stdout: []byte(`{"auth":{"client_token":"synthetic-renewal-token"}}`)}, nil
		default:
			return vaultexec.Result{}, nil
		}
	}

	err := harness.dispatcher.Dispatch(context.Background(), []string{"read", "secret/example"})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if got, want := harness.vaultRunner.commandArguments(), [][]string{
		{"token", "lookup", "-format=json"},
		{"token", "renew", "-format=json"},
		{"read", "secret/example"},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("Vault commands = %#v, want %#v", got, want)
	}
	harness.assertNoCredentialLeaks(t, err, token)
}

func TestFakeBackedPreflightAuthenticatesInvalidCredentialBeforeDelegation(t *testing.T) {
	const oldToken = "synthetic-expired-token"
	const newToken = "synthetic-replacement-token"
	harness := newComponentHarness(t, time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC))
	harness.addStoredProfile(t, "team-a", "https://team-a.example", oldToken)
	harness.vaultRunner.run = func(command vaultexec.Command) (vaultexec.Result, error) {
		switch command.Arguments[0] {
		case "token":
			return vaultexec.Result{Stderr: []byte("invalid token: " + oldToken)}, componentExitError(2)
		case "login":
			return vaultexec.Result{Stdout: []byte(`{"auth":{"client_token":"` + newToken + `"}}`)}, nil
		default:
			return vaultexec.Result{}, nil
		}
	}

	err := harness.dispatcher.Dispatch(context.Background(), []string{"read", "secret/example"})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if got, want := harness.credentials.values["team-a"], newToken; got != want {
		t.Errorf("stored credential = %q, want replacement credential", got)
	}
	if got, want := harness.vaultRunner.commandArguments(), [][]string{
		{"token", "lookup", "-format=json"},
		{"login", "-no-store", "-format=json", "-method=oidc", "-path=oidc", "username=example-user"},
		{"read", "secret/example"},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("Vault commands = %#v, want %#v", got, want)
	}
	delegated := harness.vaultRunner.delegatedCommands()
	if got := componentEnvironmentValue(delegated[0].Environment, "VAULT_TOKEN"); got != newToken {
		t.Errorf("delegated token = %q, want replacement credential", got)
	}
	harness.assertNoCredentialLeaks(t, err, oldToken, newToken)
}

func TestFakeBackedPreflightStopsOnTransportFailureWithoutLogin(t *testing.T) {
	const token = "synthetic-transport-token"
	harness := newComponentHarness(t, time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC))
	harness.addStoredProfile(t, "team-a", "https://team-a.example", token)
	harness.vaultRunner.run = func(command vaultexec.Command) (vaultexec.Result, error) {
		return vaultexec.Result{Stderr: []byte("dial tcp failed for " + token)}, componentExitError(1)
	}

	err := harness.dispatcher.Dispatch(context.Background(), []string{"read", "secret/example"})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want transport failure")
	}
	if !strings.Contains(err.Error(), "dial tcp") || !strings.Contains(err.Error(), vaultexec.RedactedValue) {
		t.Errorf("Dispatch() error = %q, want actionable redacted transport failure", err)
	}
	if got, want := harness.vaultRunner.commandArguments(), [][]string{{"token", "lookup", "-format=json"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Vault commands = %#v, want lookup only %#v", got, want)
	}
	if got := harness.credentials.values["team-a"]; got != token {
		t.Errorf("stored credential = %q, want unchanged", got)
	}
	harness.assertNoCredentialLeaks(t, err, token)
}

func TestFakeBackedDelegatedFailurePreservesStreamsAndExitWithoutRetry(t *testing.T) {
	const token = "synthetic-delegated-token"
	harness := newComponentHarness(t, time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC))
	harness.addStoredProfile(t, "team-a", "https://team-a.example", token)
	var delegatedInput string
	harness.vaultRunner.run = func(command vaultexec.Command) (vaultexec.Result, error) {
		if command.Arguments[0] == "token" {
			return vaultexec.Result{Stdout: []byte(`{"data":{"ttl":3600,"renewable":false}}`)}, nil
		}
		input, err := io.ReadAll(command.Stdin)
		if err != nil {
			return vaultexec.Result{}, err
		}
		delegatedInput = string(input)
		if _, err := io.WriteString(command.Stdout, "delegated stdout\n"); err != nil {
			return vaultexec.Result{}, err
		}
		if _, err := io.WriteString(command.Stderr, "delegated stderr\n"); err != nil {
			return vaultexec.Result{}, err
		}
		return vaultexec.Result{Stderr: []byte("permission denied for " + token)}, componentExitError(23)
	}

	err := harness.dispatcher.Dispatch(context.Background(), []string{"write", "secret/example", "value=test"})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want delegated failure")
	}
	if got, want := vaultexec.ExitCode(err), 23; got != want {
		t.Errorf("exit code = %d, want %d", got, want)
	}
	if got, want := harness.vaultRunner.commandArguments(), [][]string{
		{"token", "lookup", "-format=json"},
		{"write", "secret/example", "value=test"},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("Vault commands = %#v, want one delegated attempt %#v", got, want)
	}
	if delegatedInput != "input" {
		t.Errorf("delegated stdin = %q, want attached input", delegatedInput)
	}
	if !strings.Contains(harness.stdout.String(), "delegated stdout") || !strings.Contains(harness.stderr.String(), "delegated stderr") {
		t.Errorf("delegated streams were not attached: stdout=%q stderr=%q", harness.stdout.String(), harness.stderr.String())
	}
	harness.assertNoCredentialLeaks(t, err, token)
}

func TestFakeBackedMalformedLoginRemovesIncompleteAdd(t *testing.T) {
	const token = "synthetic-failed-add-token"
	harness := newComponentHarness(t, time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC))
	harness.vaultRunner.run = func(command vaultexec.Command) (vaultexec.Result, error) {
		return vaultexec.Result{Stdout: []byte(`{"auth":{"client_token":"` + token)}, nil
	}

	err := harness.dispatcher.Dispatch(context.Background(), []string{
		"profile", "add", "team-a", "--address", "https://team-a.example", "--username", "alice",
	})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want malformed login failure")
	}
	configuration, loadErr := harness.profiles.Load(context.Background())
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if len(configuration.Profiles) != 0 || configuration.ActiveProfile != "" {
		t.Errorf("configuration = %#v, want no incomplete profile", configuration)
	}
	if len(harness.credentials.values) != 0 {
		t.Errorf("credentials = %#v, want no incomplete credential", harness.credentials.values)
	}
	harness.assertNoCredentialLeaks(t, err, token)
}

func TestFakeBackedFailedUpdatePreservesOriginalState(t *testing.T) {
	const oldToken = "synthetic-original-token"
	const rejectedToken = "synthetic-rejected-token"
	harness := newComponentHarness(t, time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC))
	harness.addStoredProfile(t, "team-a", "https://team-a.example", oldToken)
	harness.vaultRunner.run = func(command vaultexec.Command) (vaultexec.Result, error) {
		return vaultexec.Result{
			Stdout: []byte(`{"auth":{"client_token":"` + rejectedToken + `"}}`),
			Stderr: []byte("login rejected " + rejectedToken),
		}, componentExitError(2)
	}

	err := harness.dispatcher.Dispatch(context.Background(), []string{
		"profile", "update", "team-a", "--address", "https://replacement.example",
	})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want failed reauthentication")
	}
	configuration, loadErr := harness.profiles.Load(context.Background())
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if got, want := configuration.Profiles[0].Address, "https://team-a.example"; got != want {
		t.Errorf("profile address = %q, want original %q", got, want)
	}
	if got := harness.credentials.values["team-a"]; got != oldToken {
		t.Errorf("stored credential = %q, want original credential", got)
	}
	harness.assertNoCredentialLeaks(t, err, oldToken, rejectedToken)
}

func TestFakeBackedRemoveDeletesMetadataAndCredential(t *testing.T) {
	const token = "synthetic-removed-token"
	harness := newComponentHarness(t, time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC))
	harness.addStoredProfile(t, "team-a", "https://team-a.example", token)

	err := harness.dispatcher.Dispatch(context.Background(), []string{"profile", "remove", "team-a"})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	configuration, loadErr := harness.profiles.Load(context.Background())
	if loadErr != nil {
		t.Fatalf("Load() error = %v", loadErr)
	}
	if len(configuration.Profiles) != 0 || configuration.ActiveProfile != "" {
		t.Errorf("configuration = %#v, want profile and active selection removed", configuration)
	}
	if _, credentialErr := harness.credentials.Get(context.Background(), "team-a"); !errors.Is(credentialErr, credential.ErrNotFound) {
		t.Errorf("credential error = %v, want ErrNotFound", credentialErr)
	}
	harness.assertNoCredentialLeaks(t, err, token)
}

type componentExitError int

func (e componentExitError) Error() string { return "synthetic Vault failure" }
func (e componentExitError) ExitCode() int { return int(e) }
