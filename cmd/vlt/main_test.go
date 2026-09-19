package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"vlt/internal/cli"
)

func TestNewDispatcherWiresProfileManagement(t *testing.T) {
	var stdout bytes.Buffer
	dispatcher := newDispatcherAt(t.TempDir(), strings.NewReader(""), &stdout, io.Discard)

	if err := dispatcher.Dispatch(context.Background(), []string{"profile", "list"}); err != nil {
		t.Fatalf("profile list error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("profile list output = %q, want empty", stdout.String())
	}

	if err := dispatcher.Dispatch(context.Background(), []string{"switch"}); err != nil {
		t.Fatalf("switch error = %v", err)
	}
	if got, want := stdout.String(), "Active profile: none\n"; got != want {
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

type mainExitError int

func (e mainExitError) Error() string { return "delegated failure" }
func (e mainExitError) ExitCode() int { return int(e) }
