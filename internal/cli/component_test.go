package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"

	"vlt/internal/config"
	"vlt/internal/credential"
	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

func TestFakeBackedProfileFlowUsesActiveAndOneCommandProfiles(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "profiles.json")
	profiles := config.NewStore(configPath)
	credentials := &componentCredentialStore{values: make(map[string]string)}
	vaultRunner := &componentVaultRunner{tokensByAddress: map[string]string{
		"https://team-a.example": "synthetic-team-a-token",
		"https://team-b.example": "synthetic-team-b-token",
	}}
	vault := vaultexec.NewExecutor(vaultexec.Dependencies{
		LookPath:    func(string) (string, error) { return "/test/vault", nil },
		Environment: func() []string { return []string{"PARENT=preserved", "VAULT_NAMESPACE=inherited"} },
		Runner:      vaultRunner,
	})
	authenticator := credential.NewAuthenticator(vault, credentials)
	mutations := profile.NewMutationService(profiles, credentials, authenticator)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	preflight := credential.NewPreflight(vault, credentials, authenticator, time.Now, &stderr)
	dispatcher := NewDispatcher(Dependencies{
		Output:  &stdout,
		Profile: NewProfileHandler(ProfileDependencies{Profiles: profiles, Mutations: mutations, Output: &stdout}),
		Switch:  NewSwitchHandler(SwitchDependencies{Profiles: profiles, Output: &stdout}),
		Vault: NewDelegateHandler(DelegateDependencies{
			Profiles: profiles, Preflight: preflight, Vault: vault,
			Stdin: strings.NewReader("input"), Stdout: &stdout, Stderr: &stderr,
		}),
	})

	commands := [][]string{
		{"profile", "add", "team-a", "--address", "https://team-a.example", "--username", "alice"},
		{"profile", "add", "team-b", "--address", "https://team-b.example", "--username", "bob", "--namespace", "engineering"},
		{"switch", "team-a"},
		{"read", "secret/active"},
		{"--profile", "team-b", "read", "secret/override"},
	}
	for _, arguments := range commands {
		if err := dispatcher.Dispatch(ctx, arguments); err != nil {
			t.Fatalf("Dispatch(%q) error = %v", arguments, err)
		}
	}

	configuration, err := profiles.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := configuration.ActiveProfile, "team-a"; got != want {
		t.Errorf("active profile = %q, want unchanged %q", got, want)
	}
	if got, want := credentials.values, vaultRunner.tokensByAddress; !reflect.DeepEqual(got, map[string]string{
		"team-a": want["https://team-a.example"],
		"team-b": want["https://team-b.example"],
	}) {
		t.Errorf("stored credentials = %#v, want one token per profile", got)
	}

	delegated := vaultRunner.delegatedCommands()
	if got, want := len(delegated), 2; got != want {
		t.Fatalf("delegated commands = %d, want %d", got, want)
	}
	assertComponentCommand(t, delegated[0], []string{"read", "secret/active"}, "https://team-a.example", "synthetic-team-a-token", "")
	assertComponentCommand(t, delegated[1], []string{"read", "secret/override"}, "https://team-b.example", "synthetic-team-b-token", "engineering")
	if got, want := vaultRunner.loginCalls, 2; got != want {
		t.Errorf("OIDC login calls = %d, want %d", got, want)
	}
	if got, want := vaultRunner.lookupCalls, 2; got != want {
		t.Errorf("preflight lookup calls = %d, want %d", got, want)
	}

	configContents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	combinedOutput := stdout.String() + stderr.String() + string(configContents)
	for _, token := range vaultRunner.tokensByAddress {
		if strings.Contains(combinedOutput, token) {
			t.Errorf("token %q appeared in config or command output", token)
		}
	}
}

type componentCredentialStore struct {
	values map[string]string
}

func (s *componentCredentialStore) Get(_ context.Context, profileName string) (string, error) {
	value, found := s.values[profileName]
	if !found {
		return "", credential.ErrNotFound
	}
	return value, nil
}

func (s *componentCredentialStore) Set(_ context.Context, profileName, token string) error {
	s.values[profileName] = token
	return nil
}

func (s *componentCredentialStore) Delete(_ context.Context, profileName string) error {
	if _, found := s.values[profileName]; !found {
		return credential.ErrNotFound
	}
	delete(s.values, profileName)
	return nil
}

type componentVaultRunner struct {
	tokensByAddress map[string]string
	commands        []vaultexec.Command
	loginCalls      int
	lookupCalls     int
	run             func(vaultexec.Command) (vaultexec.Result, error)
}

func (r *componentVaultRunner) Run(_ context.Context, command vaultexec.Command) (vaultexec.Result, error) {
	r.commands = append(r.commands, command)
	if len(command.Arguments) == 0 {
		return vaultexec.Result{}, errors.New("missing Vault arguments")
	}
	if r.run != nil {
		return r.run(command)
	}

	switch command.Arguments[0] {
	case "login":
		r.loginCalls++
		token := r.tokensByAddress[componentEnvironmentValue(command.Environment, "VAULT_ADDR")]
		response, err := json.Marshal(map[string]any{"auth": map[string]string{"client_token": token}})
		return vaultexec.Result{Stdout: response}, err
	case "token":
		r.lookupCalls++
		return vaultexec.Result{Stdout: []byte(`{"data":{"ttl":3600,"renewable":false}}`)}, nil
	default:
		if command.Mode != vaultexec.Delegated {
			return vaultexec.Result{}, fmt.Errorf("Vault command %q was not delegated", command.Arguments)
		}
		_, err := fmt.Fprintf(command.Stdout, "delegated %s\n", strings.Join(command.Arguments, " "))
		return vaultexec.Result{}, err
	}
}

func (r *componentVaultRunner) delegatedCommands() []vaultexec.Command {
	var commands []vaultexec.Command
	for _, command := range r.commands {
		if command.Mode == vaultexec.Delegated {
			commands = append(commands, command)
		}
	}
	return commands
}

func (r *componentVaultRunner) commandArguments() [][]string {
	arguments := make([][]string, 0, len(r.commands))
	for _, command := range r.commands {
		arguments = append(arguments, append([]string(nil), command.Arguments...))
	}
	return arguments
}

func assertComponentCommand(t *testing.T, command vaultexec.Command, arguments []string, address, token, namespace string) {
	t.Helper()
	if !reflect.DeepEqual(command.Arguments, arguments) {
		t.Errorf("arguments = %#v, want %#v", command.Arguments, arguments)
	}
	if got := componentEnvironmentValue(command.Environment, "VAULT_ADDR"); got != address {
		t.Errorf("VAULT_ADDR = %q, want %q", got, address)
	}
	if got := componentEnvironmentValue(command.Environment, "VAULT_TOKEN"); got != token {
		t.Errorf("VAULT_TOKEN = %q, want selected profile token", got)
	}
	if got := componentEnvironmentValue(command.Environment, "VAULT_NAMESPACE"); got != namespace {
		t.Errorf("VAULT_NAMESPACE = %q, want %q", got, namespace)
	}
	for _, argument := range command.Arguments {
		if strings.Contains(argument, token) {
			t.Errorf("token appeared in command argument %q", argument)
		}
	}
}

func componentEnvironmentValue(environment []string, name string) string {
	prefix := name + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

type componentHarness struct {
	configPath  string
	profiles    *config.Store
	credentials *componentCredentialStore
	vaultRunner *componentVaultRunner
	mutations   ProfileMutator
	vault       Handler
	dispatcher  *Dispatcher
	stdin       *bytes.Buffer
	stdout      bytes.Buffer
	stderr      bytes.Buffer
}

func newComponentHarness(t *testing.T, now time.Time) *componentHarness {
	t.Helper()
	harness := &componentHarness{
		configPath:  filepath.Join(t.TempDir(), "profiles.json"),
		credentials: &componentCredentialStore{values: make(map[string]string)},
		vaultRunner: &componentVaultRunner{},
		stdin:       bytes.NewBufferString("input"),
	}
	harness.profiles = config.NewStore(harness.configPath)
	vault := vaultexec.NewExecutor(vaultexec.Dependencies{
		LookPath:    func(string) (string, error) { return "/test/vault", nil },
		Environment: func() []string { return []string{"PARENT=preserved", "VAULT_NAMESPACE=inherited"} },
		Runner:      harness.vaultRunner,
	})
	authenticator := credential.NewAuthenticator(vault, harness.credentials)
	harness.mutations = profile.NewMutationService(harness.profiles, harness.credentials, authenticator)
	preflight := credential.NewPreflight(vault, harness.credentials, authenticator, func() time.Time { return now }, &harness.stderr)
	harness.vault = NewDelegateHandler(DelegateDependencies{
		Profiles: harness.profiles, Preflight: preflight, Vault: vault,
		Stdin: harness.stdin, Stdout: &harness.stdout, Stderr: &harness.stderr,
	})
	harness.wireHandlers(nil, nil, nil, nil)
	return harness
}

func (h *componentHarness) wireHandlers(
	terminal Terminal,
	selector ProfileSelector,
	form ProfileForm,
	confirmer ProfileRemovalConfirmer,
) {
	h.dispatcher = NewDispatcher(Dependencies{
		Output: &h.stdout,
		Profile: NewProfileHandler(ProfileDependencies{
			Profiles: h.profiles, Mutations: h.mutations, Output: &h.stdout, Terminal: terminal,
			Selector: selector, Form: form, RemovalConfirmer: confirmer,
		}),
		Switch:     NewSwitchHandler(SwitchDependencies{Profiles: h.profiles, Output: &h.stdout, Terminal: terminal, Selector: selector}),
		Completion: NewCompletionHandler(CompletionDependencies{Profiles: h.profiles, Output: &h.stdout}),
		Vault:      h.vault,
	})
}

type guidedComponentHarness struct {
	*componentHarness
	selector  *fakeProfileSelector
	form      *fakeProfileForm
	confirmer *fakeProfileRemovalConfirmer
}

func newGuidedComponentHarness(t *testing.T) *guidedComponentHarness {
	t.Helper()
	harness := &guidedComponentHarness{
		componentHarness: newComponentHarness(t, time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC)),
		selector:         &fakeProfileSelector{},
		form:             &fakeProfileForm{},
		confirmer:        &fakeProfileRemovalConfirmer{},
	}
	harness.wireHandlers(switchTerminal{prompts: true}, harness.selector, harness.form, harness.confirmer)
	return harness
}

func (h *componentHarness) addStoredProfile(t *testing.T, name, address, token string) {
	t.Helper()
	candidate := profile.Profile{Name: name, Address: address, Username: "example-user", AuthPath: "oidc"}
	if err := h.profiles.Save(context.Background(), config.Configuration{Profiles: []profile.Profile{candidate}, ActiveProfile: name}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	if err := h.credentials.Set(context.Background(), name, token); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
}

func (h *componentHarness) assertNoCredentialLeaks(t *testing.T, operationErr error, tokens ...string) {
	t.Helper()
	configContents, err := os.ReadFile(h.configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read config: %v", err)
	}
	surfaces := []string{string(configContents), h.stdout.String(), h.stderr.String()}
	if operationErr != nil {
		surfaces = append(surfaces, operationErr.Error())
	}
	for _, command := range h.vaultRunner.commands {
		surfaces = append(surfaces, strings.Join(command.Arguments, "\x00"))
	}
	for _, token := range tokens {
		for _, surface := range surfaces {
			if strings.Contains(surface, token) {
				t.Errorf("credential %q appeared outside the keyring or Vault environment", token)
			}
		}
	}
}

func TestFakeBackedGuidedManagementFlowUsesExistingServices(t *testing.T) {
	harness := newGuidedComponentHarness(t)
	harness.vaultRunner.tokensByAddress = map[string]string{
		"https://team-a.example":         "synthetic-team-a-token",
		"https://team-a-updated.example": "synthetic-team-a-updated-token",
		"https://team-b.example":         "synthetic-team-b-token",
	}
	harness.form.result = profile.Profile{
		Name: "team-a", Address: "https://team-a.example", Username: "alice", AuthPath: "oidc",
	}

	if err := harness.dispatcher.Dispatch(context.Background(), []string{"profile", "add"}); err != nil {
		t.Fatalf("interactive add error = %v", err)
	}
	if err := harness.dispatcher.Dispatch(context.Background(), []string{
		"profile", "add", "team-b", "--address", "https://team-b.example", "--username", "bob",
	}); err != nil {
		t.Fatalf("explicit add error = %v", err)
	}

	harness.selector.selected = "team-a"
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"switch"}); err != nil {
		t.Fatalf("interactive switch error = %v", err)
	}
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"profile", "show"}); err != nil {
		t.Fatalf("interactive show error = %v", err)
	}

	harness.form.result = profile.Profile{
		Name: "team-a", Address: "https://team-a-updated.example", Username: "alice", AuthPath: "oidc",
		Namespace: "engineering",
	}
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"profile", "update"}); err != nil {
		t.Fatalf("interactive update error = %v", err)
	}

	harness.selector.selected = "team-b"
	harness.confirmer.result = true
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"profile", "remove"}); err != nil {
		t.Fatalf("interactive remove error = %v", err)
	}

	configuration, err := harness.profiles.Load(context.Background())
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	wantProfile := harness.form.result
	if !reflect.DeepEqual(configuration, config.Configuration{
		Profiles: []profile.Profile{wantProfile}, ActiveProfile: "team-a",
	}) {
		t.Errorf("configuration = %#v, want updated active team-a only", configuration)
	}
	wantCredentials := map[string]string{"team-a": "synthetic-team-a-updated-token"}
	if !reflect.DeepEqual(harness.credentials.values, wantCredentials) {
		t.Errorf("credentials = %#v, want %#v", harness.credentials.values, wantCredentials)
	}
	if harness.form.calls != 2 || harness.selector.calls != 4 || harness.confirmer.calls != 1 {
		t.Errorf("interactive calls: form=%d selector=%d confirmer=%d, want 2/4/1", harness.form.calls, harness.selector.calls, harness.confirmer.calls)
	}
	if harness.vaultRunner.loginCalls != 3 {
		t.Errorf("login calls = %d, want add, explicit add, and update", harness.vaultRunner.loginCalls)
	}
	harness.assertNoCredentialLeaks(t, nil,
		"synthetic-team-a-token", "synthetic-team-a-updated-token", "synthetic-team-b-token",
	)
}

func TestFakeBackedNonTTYManagementFlowsStopBeforeServices(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		wantHelp  string
	}{
		{name: "profile", arguments: []string{"profile"}, wantHelp: profileHelpText},
		{name: "add", arguments: []string{"profile", "add"}, wantHelp: profileAddHelpText},
		{name: "switch", arguments: []string{"switch"}, wantHelp: switchHelpText},
		{name: "show", arguments: []string{"profile", "show"}, wantHelp: profileShowHelpText},
		{name: "update", arguments: []string{"profile", "update"}, wantHelp: profileUpdateHelpText},
		{name: "remove", arguments: []string{"profile", "remove"}, wantHelp: profileRemoveHelpText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newComponentHarness(t, time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC))

			err := harness.dispatcher.Dispatch(context.Background(), tt.arguments)
			requireAutomaticHelp(t, err, tt.wantHelp)
			configuration, loadErr := harness.profiles.Load(context.Background())
			if loadErr != nil {
				t.Fatalf("load profiles: %v", loadErr)
			}
			if len(configuration.Profiles) != 0 || configuration.ActiveProfile != "" || len(harness.credentials.values) != 0 {
				t.Errorf("non-TTY command changed state: config=%#v credentials=%#v", configuration, harness.credentials.values)
			}
			if len(harness.vaultRunner.commands) != 0 || harness.stdout.Len() != 0 {
				t.Errorf("non-TTY command used services: Vault=%#v stdout=%q", harness.vaultRunner.commandArguments(), harness.stdout.String())
			}
		})
	}
}

func TestFakeBackedInteractiveCancellationAndDeclineLeaveStateUnchanged(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		configure func(*guidedComponentHarness)
		wantError bool
	}{
		{
			name: "add cancellation", arguments: []string{"profile", "add"}, wantError: true,
			configure: func(h *guidedComponentHarness) { h.form.err = context.Canceled },
		},
		{
			name: "switch cancellation", arguments: []string{"switch"}, wantError: true,
			configure: func(h *guidedComponentHarness) { h.selector.err = context.Canceled },
		},
		{
			name: "show cancellation", arguments: []string{"profile", "show"}, wantError: true,
			configure: func(h *guidedComponentHarness) { h.selector.err = context.Canceled },
		},
		{
			name: "update cancellation", arguments: []string{"profile", "update"}, wantError: true,
			configure: func(h *guidedComponentHarness) {
				h.selector.selected = "team-a"
				h.form.err = context.Canceled
			},
		},
		{
			name: "remove cancellation", arguments: []string{"profile", "remove"}, wantError: true,
			configure: func(h *guidedComponentHarness) {
				h.selector.selected = "team-a"
				h.confirmer.err = context.Canceled
			},
		},
		{
			name: "remove decline", arguments: []string{"profile", "remove"},
			configure: func(h *guidedComponentHarness) { h.selector.selected = "team-a" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const token = "synthetic-cancelled-flow-token"
			harness := newGuidedComponentHarness(t)
			original := config.Configuration{
				Profiles: []profile.Profile{{
					Name: "team-a", Address: "https://team-a.example", Username: "alice", AuthPath: "oidc",
				}},
				ActiveProfile: "team-a",
			}
			if err := harness.profiles.Save(context.Background(), original); err != nil {
				t.Fatalf("seed profiles: %v", err)
			}
			if err := harness.credentials.Set(context.Background(), "team-a", token); err != nil {
				t.Fatalf("seed credential: %v", err)
			}
			tt.configure(harness)

			err := harness.dispatcher.Dispatch(context.Background(), tt.arguments)
			if (err != nil) != tt.wantError {
				t.Fatalf("Dispatch(%q) error = %v, wantError=%t", tt.arguments, err, tt.wantError)
			}
			configuration, loadErr := harness.profiles.Load(context.Background())
			if loadErr != nil {
				t.Fatalf("load profiles: %v", loadErr)
			}
			if !reflect.DeepEqual(configuration, original) || !reflect.DeepEqual(harness.credentials.values, map[string]string{"team-a": token}) {
				t.Errorf("cancelled command changed state: config=%#v credentials=%#v", configuration, harness.credentials.values)
			}
			if len(harness.vaultRunner.commands) != 0 || harness.stdout.Len() != 0 {
				t.Errorf("cancelled command used services: Vault=%#v stdout=%q", harness.vaultRunner.commandArguments(), harness.stdout.String())
			}
			harness.assertNoCredentialLeaks(t, err, token)
		})
	}
}

func TestFakeBackedCompletionNoColorAndDelegationStayIndependent(t *testing.T) {
	const token = "synthetic-completion-boundary-token"
	harness := newComponentHarness(t, time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC))
	harness.addStoredProfile(t, "team-a", "https://team-a.example", token)
	noColorTerminal := newTerminal(
		terminalTestReader(10), terminalTestWriter(20), []string{"NO_COLOR=1"},
		func(uintptr) bool { return true },
		func(io.Writer, []string) colorprofile.Profile { return colorprofile.TrueColor },
	)
	harness.wireHandlers(noColorTerminal, nil, nil, nil)

	for _, shell := range []string{"bash", "zsh", "fish"} {
		harness.stdout.Reset()
		if err := harness.dispatcher.Dispatch(context.Background(), []string{"completion", shell}); err != nil {
			t.Fatalf("completion %s error = %v", shell, err)
		}
		if harness.stdout.Len() == 0 {
			t.Errorf("completion %s output is empty", shell)
		}
		if len(harness.vaultRunner.commands) != 0 {
			t.Fatalf("completion %s invoked Vault: %#v", shell, harness.vaultRunner.commandArguments())
		}
	}

	harness.stdout.Reset()
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"completion", "__profiles"}); err != nil {
		t.Fatalf("completion candidates error = %v", err)
	}
	if got, want := harness.stdout.String(), "team-a\n"; got != want {
		t.Errorf("completion candidates = %q, want %q", got, want)
	}
	if len(harness.vaultRunner.commands) != 0 {
		t.Fatalf("dynamic completion invoked Vault: %#v", harness.vaultRunner.commandArguments())
	}

	harness.stdout.Reset()
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"profile", "list"}); err != nil {
		t.Fatalf("profile list error = %v", err)
	}
	if strings.Contains(harness.stdout.String(), "\x1b[") {
		t.Errorf("NO_COLOR profile list contains ANSI: %q", harness.stdout.String())
	}

	harness.stdout.Reset()
	arguments := []string{"read", "secret/example", "-format=json"}
	if err := harness.dispatcher.Dispatch(context.Background(), arguments); err != nil {
		t.Fatalf("delegated command error = %v", err)
	}
	if got, want := harness.vaultRunner.commandArguments(), [][]string{
		{"token", "lookup", "-format=json"}, arguments,
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("Vault commands = %#v, want unchanged delegation %#v", got, want)
	}
	if got, want := harness.stdout.String(), "delegated read secret/example -format=json\n"; got != want {
		t.Errorf("delegated output = %q, want %q", got, want)
	}
	harness.assertNoCredentialLeaks(t, nil, token)
}
