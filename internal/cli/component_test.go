package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
	mutations := profile.NewMutationService(harness.profiles, harness.credentials, authenticator)
	preflight := credential.NewPreflight(vault, harness.credentials, authenticator, func() time.Time { return now }, &harness.stderr)
	harness.dispatcher = NewDispatcher(Dependencies{
		Output:  &harness.stdout,
		Profile: NewProfileHandler(ProfileDependencies{Profiles: harness.profiles, Mutations: mutations, Output: &harness.stdout}),
		Switch:  NewSwitchHandler(SwitchDependencies{Profiles: harness.profiles, Output: &harness.stdout}),
		Vault: NewDelegateHandler(DelegateDependencies{
			Profiles: harness.profiles, Preflight: preflight, Vault: vault,
			Stdin: harness.stdin, Stdout: &harness.stdout, Stderr: &harness.stderr,
		}),
	})
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
