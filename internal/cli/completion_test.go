package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type completionErrorWriter struct {
	err error
}

func (w completionErrorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestCompletionHandlerWritesScripts(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewCompletionHandler(&output)

			if err := handler(context.Background(), []string{shell}); err != nil {
				t.Fatalf("completion %s error = %v", shell, err)
			}
			want, err := completionScript(shell)
			if err != nil {
				t.Fatalf("completionScript(%q) error = %v", shell, err)
			}
			if got := output.String(); got != want {
				t.Errorf("completion output differs for %s", shell)
			}
		})
	}
}

func TestCompletionHandlerDisplaysHelp(t *testing.T) {
	for _, arguments := range [][]string{{"-h"}, {"--help"}, {"bash", "--help"}} {
		var output bytes.Buffer
		handler := NewCompletionHandler(&output)

		if err := handler(context.Background(), arguments); err != nil {
			t.Fatalf("completion help %v error = %v", arguments, err)
		}
		for _, want := range []string{"Generate shell completion", "Usage:", "bash", "zsh", "fish", "Examples:"} {
			if !strings.Contains(output.String(), want) {
				t.Errorf("completion help = %q, want text %q", output.String(), want)
			}
		}
	}
}

func TestCompletionHandlerRejectsInvalidFormsWithoutOutput(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      string
	}{
		{name: "missing shell", want: "SHELL is required"},
		{name: "unsupported shell", arguments: []string{"powershell"}, want: "unsupported shell"},
		{name: "extra argument", arguments: []string{"bash", "extra"}, want: "unexpected argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewCompletionHandler(&output)

			err := handler(context.Background(), tt.arguments)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "Usage: vlt completion") {
				t.Fatalf("completion error = %v, want %q and usage", err, tt.want)
			}
			if output.Len() != 0 {
				t.Errorf("completion output = %q, want empty", output.String())
			}
		})
	}
}

func TestCompletionHandlerReturnsOutputFailure(t *testing.T) {
	wantErr := errors.New("synthetic write failure")
	handler := NewCompletionHandler(completionErrorWriter{err: wantErr})

	err := handler(context.Background(), []string{"bash"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("completion error = %v, want %v", err, wantErr)
	}
}

func TestCompletionScriptsDescribeOnlyVLTCommands(t *testing.T) {
	tests := []struct {
		shell        string
		registration string
		stopMarker   string
		flags        []string
	}{
		{
			shell: "bash", registration: "complete -F _vlt_completion vlt", stopMarker: "return 0",
			flags: []string{"--profile", "--address", "--username", "--auth-path", "--namespace", "-h", "--help"},
		},
		{
			shell: "zsh", registration: "compdef _vlt vlt", stopMarker: "return 0",
			flags: []string{"--profile", "--address", "--username", "--auth-path", "--namespace", "-h", "--help"},
		},
		{
			shell: "fish", registration: "complete -c vlt", stopMarker: "__vlt_needs_command",
			flags: []string{"-l profile", "-l address", "-l username", "-l auth-path", "-l namespace", "-s h -l help"},
		},
	}
	required := []string{
		"profile", "switch", "completion", "add", "list", "show", "update", "remove",
		"bash", "zsh", "fish", "completion __profiles", "2>/dev/null",
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			script, err := completionScript(tt.shell)
			if err != nil {
				t.Fatalf("completionScript(%q) error = %v", tt.shell, err)
			}
			if script == "" {
				t.Fatal("completion script is empty")
			}
			wantTokens := append(append([]string(nil), required...), tt.flags...)
			wantTokens = append(wantTokens, tt.registration, tt.stopMarker)
			for _, token := range wantTokens {
				if !strings.Contains(script, token) {
					t.Errorf("completion script missing %q", token)
				}
			}
			for _, forbidden := range []string{".bashrc", ".zshrc", "config.fish", ">>", " vault ", "VAULT_TOKEN", "keyring"} {
				if strings.Contains(script, forbidden) {
					t.Errorf("completion script contains forbidden text %q", forbidden)
				}
			}
			second, err := completionScript(tt.shell)
			if err != nil || second != script {
				t.Errorf("second completion script differs or failed: error=%v", err)
			}
		})
	}
}

func TestCompletionScriptsHaveValidInstalledShellSyntax(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			executable, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s is not installed", shell)
			}
			script, err := completionScript(shell)
			if err != nil {
				t.Fatalf("completionScript(%q) error = %v", shell, err)
			}
			path := filepath.Join(t.TempDir(), "vlt-completion."+shell)
			if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
				t.Fatalf("write completion script: %v", err)
			}
			if output, err := exec.Command(executable, "-n", path).CombinedOutput(); err != nil {
				t.Fatalf("%s syntax check error = %v: %s", shell, err, output)
			}
		})
	}
}

func TestCompletionScriptRejectsUnsupportedShell(t *testing.T) {
	for _, shell := range []string{"", "powershell", "bash; touch unexpected"} {
		t.Run(shell, func(t *testing.T) {
			script, err := completionScript(shell)
			if err == nil {
				t.Fatal("completionScript() error = nil, want failure")
			}
			if script != "" {
				t.Errorf("completion script = %q, want empty", script)
			}
			for _, want := range []string{"unsupported shell", "Usage: vlt completion", "bash, zsh, or fish"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("completion error = %q, want text %q", err, want)
				}
			}
		})
	}
}
