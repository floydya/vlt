package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vlt/internal/config"
	"vlt/internal/profile"
)

type completionErrorWriter struct {
	err error
}

func (w completionErrorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type completionProfileLoader struct {
	configuration config.Configuration
	err           error
	loads         int
}

func (l *completionProfileLoader) Load(context.Context) (config.Configuration, error) {
	l.loads++
	return l.configuration, l.err
}

func TestCompletionHandlerWritesScripts(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewCompletionHandler(CompletionDependencies{Output: &output})

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
		loader := &completionProfileLoader{}
		var output bytes.Buffer
		handler := NewCompletionHandler(CompletionDependencies{Profiles: loader, Output: &output})

		if err := handler(context.Background(), arguments); err != nil {
			t.Fatalf("completion help %v error = %v", arguments, err)
		}
		if got := output.String(); got != completionHelpText {
			t.Errorf("completion help = %q, want %q", got, completionHelpText)
		}
		if loader.loads != 0 {
			t.Errorf("completion help profile loads = %d, want 0", loader.loads)
		}
	}
}

func TestCompletionHandlerReturnsAutomaticHelpWhenShellMissing(t *testing.T) {
	loader := &completionProfileLoader{}
	var output bytes.Buffer
	handler := NewCompletionHandler(CompletionDependencies{Profiles: loader, Output: &output})

	err := handler(context.Background(), nil)

	requireAutomaticHelp(t, err, completionHelpText)
	if loader.loads != 0 {
		t.Errorf("profile loads = %d, want 0", loader.loads)
	}
	if output.Len() != 0 {
		t.Errorf("completion output = %q, want empty", output.String())
	}
}

func TestCompletionHandlerRejectsInvalidFormsWithoutOutput(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      string
	}{
		{name: "unsupported shell", arguments: []string{"powershell"}, want: "unsupported shell"},
		{name: "extra argument", arguments: []string{"bash", "extra"}, want: "unexpected argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewCompletionHandler(CompletionDependencies{Output: &output})

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
	handler := NewCompletionHandler(CompletionDependencies{Output: completionErrorWriter{err: wantErr}})

	err := handler(context.Background(), []string{"bash"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("completion error = %v, want %v", err, wantErr)
	}
}

func TestCompletionHandlerEmitsSortedProfileNamesOnly(t *testing.T) {
	loader := &completionProfileLoader{configuration: config.Configuration{Profiles: []profile.Profile{
		{Name: "team-b", Address: "https://address-token.example", Username: "username-token", AuthPath: "keyring-token", Namespace: "namespace-token"},
		{Name: "Alpha", Address: "https://other.example", Username: "other-user", AuthPath: "oidc"},
		{Name: "team-a", Address: "https://third.example", Username: "third-user", AuthPath: "oidc"},
	}}}
	var output bytes.Buffer
	handler := NewCompletionHandler(CompletionDependencies{Profiles: loader, Output: &output})

	if err := handler(context.Background(), []string{"__profiles"}); err != nil {
		t.Fatalf("completion candidates error = %v", err)
	}
	if got, want := output.String(), "Alpha\nteam-a\nteam-b\n"; got != want {
		t.Errorf("completion candidates = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"address-token", "username-token", "keyring-token", "namespace-token", "https://"} {
		if strings.Contains(output.String(), forbidden) {
			t.Errorf("completion candidates exposed %q: %q", forbidden, output.String())
		}
	}
	if loader.loads != 1 {
		t.Errorf("profile loads = %d, want 1", loader.loads)
	}
}

func TestCompletionHandlerSilencesProfileLoadFailure(t *testing.T) {
	loader := &completionProfileLoader{err: errors.New("load failed with hvs.synthetic-token")}
	var output bytes.Buffer
	handler := NewCompletionHandler(CompletionDependencies{Profiles: loader, Output: &output})

	if err := handler(context.Background(), []string{"__profiles"}); err != nil {
		t.Fatalf("completion candidates error = %v, want silent success", err)
	}
	if output.Len() != 0 {
		t.Errorf("completion candidates = %q, want empty", output.String())
	}
}

func TestBashCompletionUsesProfilesOnlyInManagementSelectors(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	script, err := completionScript("bash")
	if err != nil {
		t.Fatalf("completionScript(bash) error = %v", err)
	}
	tests := []struct {
		name       string
		words      string
		wordIndex  int
		wantNames  bool
		wantOutput bool
	}{
		{name: "profile override", words: "vlt --profile ''", wordIndex: 2, wantNames: true, wantOutput: true},
		{name: "switch", words: "vlt switch ''", wordIndex: 2, wantNames: true, wantOutput: true},
		{name: "show", words: "vlt profile show ''", wordIndex: 3, wantNames: true, wantOutput: true},
		{name: "update", words: "vlt profile update ''", wordIndex: 3, wantNames: true, wantOutput: true},
		{name: "remove", words: "vlt profile remove ''", wordIndex: 3, wantNames: true, wantOutput: true},
		{name: "add", words: "vlt profile add ''", wordIndex: 3, wantOutput: true},
		{name: "delegated command", words: "vlt status ''", wordIndex: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invocation := script + `
vlt() {
    if [[ "$1 $2" == "completion __profiles" ]]; then
        printf 'team-a\nteam-b\n'
    else
        printf 'unexpected-invocation\n'
    fi
}
COMP_WORDS=(` + tt.words + `)
COMP_CWORD=` + fmt.Sprint(tt.wordIndex) + `
_vlt_completion
printf '%s\n' "${COMPREPLY[@]}"
`
			output, err := exec.Command(bash, "-c", invocation).CombinedOutput()
			if err != nil {
				t.Fatalf("bash completion error = %v: %s", err, output)
			}
			got := string(output)
			hasNames := strings.Contains(got, "team-a") || strings.Contains(got, "team-b")
			if hasNames != tt.wantNames {
				t.Errorf("bash completion output = %q, wantNames=%t", got, tt.wantNames)
			}
			if strings.Contains(got, "unexpected-invocation") {
				t.Errorf("bash completion invoked unexpected command: %q", got)
			}
			if (got != "\n") != tt.wantOutput {
				t.Errorf("bash completion output = %q, wantOutput=%t", got, tt.wantOutput)
			}
		})
	}
}

func TestBashCompletionProvidesFavoriteCommandsAndValues(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	script, err := completionScript("bash")
	if err != nil {
		t.Fatalf("completionScript(bash) error = %v", err)
	}
	tests := []struct {
		name      string
		words     string
		wordIndex int
		want      []string
		forbidden []string
	}{
		{name: "top-level prefix", words: "vlt fav", wordIndex: 1, want: []string{"favorite"}},
		{name: "namespace", words: "vlt favorite ''", wordIndex: 2, want: []string{"add", "list", "update", "remove"}},
		{name: "namespace prefix", words: "vlt favorite up", wordIndex: 2, want: []string{"update"}, forbidden: []string{"add", "list", "remove"}},
		{name: "add flags", words: "vlt favorite add secret/app ''", wordIndex: 4, want: []string{"--profile", "--operation", "--note", "-h", "--help"}},
		{name: "add profile", words: "vlt favorite add secret/app --profile ''", wordIndex: 5, want: []string{"team-a", "team-b"}, forbidden: []string{"secret/app", "daily"}},
		{name: "add operation", words: "vlt favorite add secret/app --operation ''", wordIndex: 5, want: []string{"read", "kv-get"}},
		{name: "update flags", words: "vlt favorite update 1 ''", wordIndex: 4, want: []string{"--profile", "--operation", "--path", "--note", "-h", "--help"}},
		{name: "update profile", words: "vlt favorite update 1 --profile ''", wordIndex: 5, want: []string{"team-a", "team-b"}, forbidden: []string{"secret/app", "daily"}},
		{name: "update operation", words: "vlt favorite update 1 --operation ''", wordIndex: 5, want: []string{"read", "kv-get"}},
		{name: "path value", words: "vlt favorite update 1 --path ''", wordIndex: 5, forbidden: []string{"team-a", "team-b", "read", "kv-get", "secret/app", "daily"}},
		{name: "note value", words: "vlt favorite update 1 --note ''", wordIndex: 5, forbidden: []string{"team-a", "team-b", "read", "kv-get", "secret/app", "daily"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invocation := script + `
vlt() {
    if [[ "$1 $2" == "completion __profiles" ]]; then
        printf 'team-a\nteam-b\n'
    else
        printf 'unexpected-invocation\n'
    fi
}
COMP_WORDS=(` + tt.words + `)
COMP_CWORD=` + fmt.Sprint(tt.wordIndex) + `
_vlt_completion
printf '%s\n' "${COMPREPLY[@]}"
`
			output, err := exec.Command(bash, "-c", invocation).CombinedOutput()
			if err != nil {
				t.Fatalf("bash completion error = %v: %s", err, output)
			}
			got := string(output)
			for _, want := range tt.want {
				if !strings.Contains(got, want+"\n") {
					t.Errorf("bash completion output = %q, want %q", got, want)
				}
			}
			for _, forbidden := range append(tt.forbidden, "unexpected-invocation") {
				if strings.Contains(got, forbidden) {
					t.Errorf("bash completion output = %q, want no %q", got, forbidden)
				}
			}
		})
	}
}

func TestCompletionScriptsDescribeFavoriteCommandsAndValues(t *testing.T) {
	tests := []struct {
		shell string
		want  []string
	}{
		{shell: "bash", want: []string{"favorite", "add list update remove", "--profile --operation --note", "--profile --operation --path --note", "read kv-get"}},
		{shell: "zsh", want: []string{"favorite:manage favorites", "'add' 'list' 'update' 'remove'", "'--profile' '--operation' '--note'", "'--profile' '--operation' '--path' '--note'", "'read' 'kv-get'"}},
		{shell: "fish", want: []string{"-a 'profile switch favorite completion'", "-a 'add list update remove'", "-l profile", "-l operation", "-l path", "-l note", "-a 'read kv-get'"}},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			script, err := completionScript(tt.shell)
			if err != nil {
				t.Fatalf("completionScript(%q) error = %v", tt.shell, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(script, want) {
					t.Errorf("completion script missing %q", want)
				}
			}
		})
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
			flags: []string{"--profile", "--address", "--username", "--auth-path", "--namespace", "--allow-insecure", "-h", "--help"},
		},
		{
			shell: "zsh", registration: "compdef _vlt vlt", stopMarker: "return 0",
			flags: []string{"--profile", "--address", "--username", "--auth-path", "--namespace", "--allow-insecure", "-h", "--help"},
		},
		{
			shell: "fish", registration: "complete -c vlt", stopMarker: "__vlt_needs_command",
			flags: []string{"-l profile", "-l address", "-l username", "-l auth-path", "-l namespace", "-l allow-insecure", "-s h -l help"},
		},
	}
	required := []string{
		"profile", "switch", "favorite", "completion", "add", "list", "show", "update", "remove",
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
