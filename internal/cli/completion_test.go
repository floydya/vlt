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
	"vlt/internal/favorite"
	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

type completionVaultStub struct {
	result     vaultexec.Result
	err        error
	calls      int
	invocation vaultexec.Invocation
}

func (s *completionVaultStub) Execute(_ context.Context, invocation vaultexec.Invocation) (vaultexec.Result, error) {
	s.calls++
	s.invocation = invocation
	return s.result, s.err
}

func TestCompletionHandlerFiltersVaultRootCommands(t *testing.T) {
	vault := &completionVaultStub{result: vaultexec.Result{Stdout: []byte("read\nkv\nprofile\nkv\n--help\nbad name\nfavorite\n")}}
	loader := &completionProfileLoader{}
	var output bytes.Buffer
	handler := NewCompletionHandler(CompletionDependencies{Profiles: loader, Vault: vault, Output: &output})

	if err := handler(context.Background(), []string{"__vault_commands", ""}); err != nil {
		t.Fatalf("Vault command completion error = %v", err)
	}
	if got, want := output.String(), "kv\nread\n"; got != want {
		t.Errorf("Vault commands = %q, want %q", got, want)
	}
	if vault.calls != 1 || loader.loads != 0 {
		t.Errorf("Vault calls = %d, profile loads = %d; want 1 and 0", vault.calls, loader.loads)
	}
	if len(vault.invocation.Arguments) != 0 || vault.invocation.Mode != vaultexec.Captured {
		t.Errorf("Vault invocation has arguments or wrong stream mode: %#v", vault.invocation)
	}
	if got := vault.invocation.Environment.Set["COMP_LINE"]; got != "vault " {
		t.Errorf("COMP_LINE = %q, want %q", got, "vault ")
	}
	if got := vault.invocation.Environment.Set["COMP_POINT"]; got != "6" {
		t.Errorf("COMP_POINT = %q, want 6", got)
	}
	if got := vault.invocation.Environment.Set["VAULT_ADDR"]; got != "not-a-url" {
		t.Errorf("VAULT_ADDR = %q, want invalid local-completion address", got)
	}
}

func TestCompletionHandlerSilencesUnavailableVault(t *testing.T) {
	for _, vault := range []*completionVaultStub{nil, {err: errors.New("private failure")}} {
		var output bytes.Buffer
		dependencies := CompletionDependencies{Output: &output}
		if vault != nil {
			dependencies.Vault = vault
		}
		handler := NewCompletionHandler(dependencies)
		if err := handler(context.Background(), []string{"__vault_commands", "k"}); err != nil {
			t.Errorf("Vault command completion error = %v, want none", err)
		}
		if output.Len() != 0 {
			t.Errorf("Vault command completion output = %q, want empty", output.String())
		}
	}
}

func TestCompletionHandlerRejectsUnsafeVaultRootPrefix(t *testing.T) {
	vault := &completionVaultStub{result: vaultexec.Result{Stdout: []byte("kv\n")}}
	var output bytes.Buffer
	handler := NewCompletionHandler(CompletionDependencies{Vault: vault, Output: &output})
	for _, prefix := range []string{"-h", "kv get", "kv\nread", "$(echo unsafe)"} {
		if err := handler(context.Background(), []string{"__vault_commands", prefix}); err != nil {
			t.Errorf("prefix %q returned error = %v", prefix, err)
		}
	}
	if vault.calls != 0 || output.Len() != 0 {
		t.Errorf("unsafe prefix called Vault %d times or wrote %q", vault.calls, output.String())
	}
}

func TestCompletionHandlerRequestsVaultArgumentCandidates(t *testing.T) {
	for _, tt := range []struct {
		name       string
		line       string
		vaultLine  string
		candidates string
	}{
		{name: "nested command", line: "vlt kv g", vaultLine: "vault kv g", candidates: "get\n"},
		{name: "profile override", line: "vlt --profile team-a kv g", vaultLine: "vault kv g", candidates: "get\n"},
		{name: "flag", line: "vlt kv get -m", vaultLine: "vault kv get -m", candidates: "-mfa\n-mount\n"},
		{name: "local flag value", line: "vlt kv get -format=j", vaultLine: "vault kv get -format=j", candidates: "json\n"},
		{name: "quoted argument", line: `vlt kv get -field="a b" j`, vaultLine: `vault kv get -field="a b" j`, candidates: "json\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			vault := &completionVaultStub{result: vaultexec.Result{Stdout: []byte(tt.candidates)}}
			var output bytes.Buffer
			handler := NewCompletionHandler(CompletionDependencies{Vault: vault, Output: &output})

			if err := handler(context.Background(), []string{"__vault_args", tt.line}); err != nil {
				t.Fatalf("Vault argument completion error = %v", err)
			}
			if got := output.String(); got != tt.candidates {
				t.Errorf("Vault arguments = %q, want %q", got, tt.candidates)
			}
			if vault.calls != 1 || len(vault.invocation.Arguments) != 0 || vault.invocation.Mode != vaultexec.Captured {
				t.Errorf("Vault invocation = %#v, calls = %d", vault.invocation, vault.calls)
			}
			if got := vault.invocation.Environment.Set["COMP_LINE"]; got != tt.vaultLine {
				t.Errorf("COMP_LINE = %q, want %q", got, tt.vaultLine)
			}
			if got := vault.invocation.Environment.Set["COMP_POINT"]; got != fmt.Sprint(len(tt.vaultLine)) {
				t.Errorf("COMP_POINT = %q, want %d", got, len(tt.vaultLine))
			}
		})
	}
}

func TestCompletionHandlerFiltersVaultArgumentFailuresAndControlText(t *testing.T) {
	vault := &completionVaultStub{result: vaultexec.Result{Stdout: []byte("get\nget\nbad\x1b[31m\njson\n")}}
	var output bytes.Buffer
	handler := NewCompletionHandler(CompletionDependencies{Vault: vault, Output: &output})
	if err := handler(context.Background(), []string{"__vault_args", "vlt kv g"}); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "get\njson\n"; got != want {
		t.Errorf("filtered Vault arguments = %q, want %q", got, want)
	}

	output.Reset()
	vault.err = errors.New("private failure")
	if err := handler(context.Background(), []string{"__vault_args", "vlt kv g"}); err != nil || output.Len() != 0 {
		t.Errorf("failed Vault completion returned error %v or output %q", err, output.String())
	}
	for _, line := range []string{"vlt --profile team-a", "vlt profile show", "vlt kv\nget", "other kv get"} {
		if err := handler(context.Background(), []string{"__vault_args", line}); err != nil {
			t.Errorf("invalid completion line %q returned %v", line, err)
		}
	}
	if vault.calls != 2 {
		t.Errorf("Vault calls = %d, want 2 valid requests", vault.calls)
	}
}

func TestBashCompletionRequestsVaultArguments(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	for _, tt := range []struct {
		name     string
		line     string
		words    string
		index    int
		point    int
		wantLine string
		want     string
	}{
		{name: "nested command", line: "vlt kv g", words: "vlt kv g", index: 2, want: "get"},
		{name: "profile override", line: "vlt --profile team-a kv g", words: "vlt --profile team-a kv g", index: 4, want: "get"},
		{name: "flag", line: "vlt kv get -m", words: "vlt kv get -m", index: 3, want: "-mount"},
		{name: "local flag value", line: "vlt kv get -format=j", words: "vlt kv get -format = j", index: 5, want: "json"},
		{name: "cursor before later words", line: "vlt kv g ignored", words: "vlt kv g ignored", index: 2, point: len("vlt kv g"), wantLine: "vlt kv g", want: "get"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			point := tt.point
			if point == 0 {
				point = len(tt.line)
			}
			wantLine := tt.wantLine
			if wantLine == "" {
				wantLine = tt.line
			}
			invocation := bashCompletionScript + `
vlt() {
    if [[ "$1 $2" == "completion __vault_args" && "$3" == ` + fmt.Sprintf("%q", wantLine) + ` ]]; then
        printf '%s\n' ` + fmt.Sprintf("%q", tt.want) + `
    fi
}
COMP_LINE=` + fmt.Sprintf("%q", tt.line) + `
COMP_POINT=` + fmt.Sprint(point) + `
COMP_WORDS=(` + tt.words + `)
COMP_CWORD=` + fmt.Sprint(tt.index) + `
_vlt_completion
printf '%s\n' "${COMPREPLY[@]}"
`
			output, err := exec.Command(bash, "-c", invocation).CombinedOutput()
			if err != nil {
				t.Fatalf("Bash completion error = %v: %s", err, output)
			}
			if got := strings.TrimSpace(string(output)); got != tt.want {
				t.Errorf("Bash completion = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBashCompletionSurvivesErrexitWhenOnlyVaultMatches(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	for _, tt := range []struct {
		name  string
		words string
		index int
	}{
		{name: "root", words: "vlt k", index: 1},
		{name: "profile override", words: "vlt --profile team-a k", index: 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			invocation := `set -e
` + bashCompletionScript + `
vlt() {
    if [[ "$1 $2" == "completion __vault_commands" ]]; then
        printf 'kv\n'
    fi
}
COMP_WORDS=(` + tt.words + `)
COMP_CWORD=` + fmt.Sprint(tt.index) + `
_vlt_completion
printf '%s\n' "${COMPREPLY[@]}"
`
			output, err := exec.Command(bash, "-c", invocation).CombinedOutput()
			if err != nil || string(output) != "kv\n" {
				t.Fatalf("Bash completion under errexit = %q, error = %v; want kv", output, err)
			}
		})
	}
}

func TestBashCompletionCombinesManagementAndVaultCommands(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	for _, tt := range []struct {
		name     string
		words    string
		index    int
		want     []string
		vaultErr bool
	}{
		{name: "root", words: "vlt ''", index: 1, want: []string{"profile", "switch", "favorite", "completion", "--profile", "-h", "--help", "kv"}},
		{name: "root prefix", words: "vlt k", index: 1, want: []string{"kv"}},
		{name: "after profile", words: "vlt --profile team-a k", index: 3, want: []string{"kv"}},
		{name: "Vault failure", words: "vlt ''", index: 1, want: []string{"profile", "switch", "favorite", "completion", "--profile", "-h", "--help"}, vaultErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			failure := ""
			if tt.vaultErr {
				failure = "printf 'private failure\\n' >&2; return 1"
			}
			invocation := bashCompletionScript + `
vlt() {
    if [[ "$1 $2" == "completion __vault_commands" ]]; then
        ` + failure + `
        printf 'kv\nprofile\nkv\n'
    fi
}
COMP_WORDS=(` + tt.words + `)
COMP_CWORD=` + fmt.Sprint(tt.index) + `
_vlt_completion
printf '%s\n' "${COMPREPLY[@]}"
`
			output, err := exec.Command(bash, "-c", invocation).CombinedOutput()
			if err != nil {
				t.Fatalf("Bash completion error = %v: %s", err, output)
			}
			got := strings.Fields(string(output))
			if len(got) != len(tt.want) {
				t.Errorf("Bash candidates = %q, want %q", got, tt.want)
			}
			for _, want := range tt.want {
				if count := countCompletionCandidate(got, want); count != 1 {
					t.Errorf("candidate %q appears %d times in %q, want once", want, count, got)
				}
			}
			if tt.vaultErr && strings.Contains(string(output), "private failure") {
				t.Errorf("Bash completion printed a Vault diagnostic: %q", output)
			}
		})
	}
}

func countCompletionCandidate(candidates []string, wanted string) int {
	count := 0
	for _, candidate := range candidates {
		if candidate == wanted {
			count++
		}
	}
	return count
}

func TestCompletionListsFavoriteIDsWithoutPaths(t *testing.T) {
	store := &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{
		{ID: "f_0123456789abcdef", Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/private"},
	}}}
	var output bytes.Buffer
	handler := NewCompletionHandler(CompletionDependencies{Favorites: store, Output: &output})
	if err := handler(context.Background(), []string{"__favorites"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "f_0123456789abcdef\n" {
		t.Fatalf("favorite completion = %q", got)
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, err := completionScript(shell)
		if err != nil || !strings.Contains(script, "completion __favorites") || !strings.Contains(script, "current") {
			t.Errorf("%s completion misses favorite IDs or current: %v", shell, err)
		}
	}
}

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

func TestCompletionScriptsSuggestProfileColorForAddAndUpdate(t *testing.T) {
	for _, tt := range []struct {
		shell string
		add   string
		edit  string
	}{
		{shell: "bash", add: `--namespace --allow-insecure --color -h --help`, edit: `--namespace --allow-insecure --color -h --help`},
		{shell: "zsh", add: `'--namespace' '--allow-insecure' '--color' '-h' '--help'`, edit: `'--namespace' '--allow-insecure' '--color' '-h' '--help'`},
		{shell: "fish", add: `__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l color -r`, edit: `__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l color -r`},
	} {
		t.Run(tt.shell, func(t *testing.T) {
			script, err := completionScript(tt.shell)
			if err != nil {
				t.Fatalf("completionScript(%q) error = %v", tt.shell, err)
			}
			if tt.shell == "fish" {
				for _, clause := range []string{tt.add, tt.edit} {
					if !strings.Contains(script, clause) {
						t.Errorf("%s completion lacks profile color clause %q", tt.shell, clause)
					}
				}
			} else if got := strings.Count(script, tt.add); got != 2 {
				t.Errorf("%s profile add and update color clauses = %d, want 2", tt.shell, got)
			}
		})
	}
}

func TestBashCompletionOffersColorOnlyForProfileCommands(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	for _, tt := range []struct {
		name  string
		words string
		index int
		want  string
	}{
		{name: "add", words: "vlt profile add --co", index: 3, want: "--color\n"},
		{name: "update", words: "vlt profile update team-a --co", index: 4, want: "--color\n"},
		{name: "delegated", words: "vlt status --co", index: 2, want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			invocation := bashCompletionScript + `
COMP_WORDS=(` + tt.words + `)
COMP_CWORD=` + fmt.Sprint(tt.index) + `
_vlt_completion
for candidate in "${COMPREPLY[@]}"; do printf '%s\n' "$candidate"; done
`
			output, err := exec.Command(bash, "-c", invocation).CombinedOutput()
			if err != nil {
				t.Fatalf("bash completion error = %v: %s", err, output)
			}
			if got := string(output); got != tt.want {
				t.Fatalf("bash completion = %q, want %q", got, tt.want)
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

func TestZshRootCompletionInsertsOnlyCommandNames(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	script, err := completionScript("zsh")
	if err != nil {
		t.Fatalf("completionScript(zsh) error = %v", err)
	}
	for _, tt := range []struct {
		name    string
		current string
		words   string
		prefix  string
		want    string
	}{
		{name: "first command", current: "2", words: "vlt ''", want: "profile\nswitch\nfavorite\ncompletion\n--profile\n-h\n--help\nkv\n"},
		{name: "typed prefix", current: "2", words: "vlt k", prefix: "k", want: "kv\n"},
		{name: "empty command fallback", current: "3", words: "vlt '' ''", want: "profile\nswitch\nfavorite\ncompletion\n--profile\n-h\n--help\nkv\n"},
		{name: "after profile", current: "4", words: "vlt --profile team-a ''", want: "profile\nswitch\nfavorite\ncompletion\n--profile\n-h\n--help\nkv\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			invocation := `compdef() { :; }
_describe() {
    local entry candidate
    for entry in "${commands[@]}"; do
        candidate="${entry%%:*}"
        if [[ -z "$PREFIX" || "$candidate" == "$PREFIX"* ]]; then
            print -r -- "$candidate"
        fi
    done
}
vlt() {
    if [[ "$1 $2" == "completion __vault_commands" ]]; then
        print -rl -- kv profile kv
    fi
}
` + script + `
words=(` + tt.words + `)
CURRENT=` + tt.current + `
PREFIX=` + fmt.Sprintf("%q", tt.prefix) + `
_vlt
`
			output, err := exec.Command(zsh, "-fc", invocation).CombinedOutput()
			if err != nil {
				t.Fatalf("Zsh completion error = %v: %s", err, output)
			}
			if got := string(output); got != tt.want {
				t.Fatalf("root completion = %q, want exact command names %q", got, tt.want)
			}
		})
	}
}

func TestZshCompletionRequestsVaultArguments(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	for _, tt := range []struct {
		name    string
		words   string
		current int
		line    string
		want    string
	}{
		{name: "nested command", words: "vlt kv g", current: 3, line: "vlt kv g", want: "get\n"},
		{name: "flag", words: "vlt kv get -m", current: 4, line: "vlt kv get -m", want: "-mount\n"},
		{name: "local value", words: "vlt kv get -format=j", current: 4, line: "vlt kv get -format=j", want: "-format=json\n"},
		{name: "profile override", words: "vlt --profile team-a kv get -format=j", current: 6, line: "vlt --profile team-a kv get -format=j", want: "-format=json\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			invocation := `compdef() { :; }
compadd() {
    shift
    print -rl -- "$@"
}
vlt() {
    if [[ "$1 $2" == "completion __vault_args" ]]; then
        case "$3" in
            'vlt kv g') print -r -- get ;;
            'vlt kv get -m') print -r -- -mount ;;
            'vlt kv get -format=j'|'vlt --profile team-a kv get -format=j') print -r -- json ;;
        esac
    fi
}
` + zshCompletionScript + `
words=(` + tt.words + `)
CURRENT=` + fmt.Sprint(tt.current) + `
LBUFFER=` + fmt.Sprintf("%q", tt.line) + `
_vlt
`
			output, err := exec.Command(zsh, "-fc", invocation).CombinedOutput()
			if err != nil || string(output) != tt.want {
				t.Fatalf("Zsh completion = %q, error = %v; want %q", output, err, tt.want)
			}
		})
	}
}

func TestZshCompletionSilencesUnavailableVault(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	invocation := `compdef() { :; }
compadd() { print -r -- unexpected; }
vlt() { print -r -- private-failure >&2; return 1; }
` + zshCompletionScript + `
words=(vlt kv g)
CURRENT=3
LBUFFER='vlt kv g'
_vlt
`
	output, err := exec.Command(zsh, "-fc", invocation).CombinedOutput()
	if err != nil || len(output) != 0 {
		t.Fatalf("unavailable Vault completion = %q, error = %v; want silence", output, err)
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
