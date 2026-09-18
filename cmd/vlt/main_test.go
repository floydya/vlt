package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"vlt/internal/cli"
)

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

type mainExitError int

func (e mainExitError) Error() string { return "delegated failure" }
func (e mainExitError) ExitCode() int { return int(e) }
