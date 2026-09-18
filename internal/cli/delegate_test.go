package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vlt/internal/config"
	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

type fakeConfigurationLoader struct {
	configuration config.Configuration
	err           error
	calls         int
	events        *[]string
}

func (f *fakeConfigurationLoader) Load(context.Context) (config.Configuration, error) {
	f.calls++
	*f.events = append(*f.events, "load")
	return f.configuration, f.err
}

type fakeCredentialPreflight struct {
	token    string
	err      error
	calls    int
	selected profile.Profile
	events   *[]string
}

func (f *fakeCredentialPreflight) Prepare(_ context.Context, selected profile.Profile) (string, error) {
	f.calls++
	f.selected = selected
	*f.events = append(*f.events, "preflight")
	return f.token, f.err
}

type fakeDelegateVault struct {
	findErr      error
	executeErr   error
	findCalls    int
	executeCalls int
	invocation   vaultexec.Invocation
	events       *[]string
}

func (f *fakeDelegateVault) FindVault() (string, error) {
	f.findCalls++
	*f.events = append(*f.events, "find")
	return "/test/vault", f.findErr
}

func (f *fakeDelegateVault) Execute(_ context.Context, invocation vaultexec.Invocation) (vaultexec.Result, error) {
	f.executeCalls++
	f.invocation = invocation
	*f.events = append(*f.events, "execute")
	return vaultexec.Result{}, f.executeErr
}

func TestDelegateUsesActiveProfileAndPreservesOpaqueInvocation(t *testing.T) {
	events := []string{}
	teamA := delegateTestProfile("team-a", "https://team-a.example", "")
	stdin := strings.NewReader("input")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	loader := &fakeConfigurationLoader{
		configuration: config.Configuration{
			Profiles:      []profile.Profile{teamA, delegateTestProfile("team-b", "https://team-b.example", "engineering")},
			ActiveProfile: teamA.Name,
		},
		events: &events,
	}
	preflight := &fakeCredentialPreflight{token: "synthetic-team-a-token", events: &events}
	vault := &fakeDelegateVault{events: &events}
	handler := NewDelegateHandler(DelegateDependencies{
		Profiles:  loader,
		Preflight: preflight,
		Vault:     vault,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
	})
	arguments := []string{"kv", "put", "secret/a b", "value=$(not-a-shell)", "; exit 99"}

	if err := handler(context.Background(), arguments); err != nil {
		t.Fatalf("delegate error = %v", err)
	}

	if got, want := events, []string{"load", "find", "preflight", "execute"}; !reflect.DeepEqual(got, want) {
		t.Errorf("events = %#v, want %#v", got, want)
	}
	if preflight.selected != teamA {
		t.Errorf("selected profile = %#v, want %#v", preflight.selected, teamA)
	}
	if !reflect.DeepEqual(vault.invocation.Arguments, arguments) {
		t.Errorf("arguments = %#v, want unchanged %#v", vault.invocation.Arguments, arguments)
	}
	if got, want := vault.invocation.Environment, vaultexec.ProfileEnvironment(teamA.Address, preflight.token, teamA.Namespace); !reflect.DeepEqual(got, want) {
		t.Errorf("environment = %#v, want %#v", got, want)
	}
	if vault.invocation.Mode != vaultexec.Delegated {
		t.Errorf("stream mode = %v, want Delegated", vault.invocation.Mode)
	}
	if vault.invocation.Stdin != stdin || vault.invocation.Stdout != stdout || vault.invocation.Stderr != stderr {
		t.Fatal("delegate did not attach the caller streams")
	}
}

func TestDelegateProfileOverrideAppliesOnceWithoutChangingActiveSelection(t *testing.T) {
	events := []string{}
	teamA := delegateTestProfile("team-a", "https://team-a.example", "")
	teamB := delegateTestProfile("team-b", "https://team-b.example", "engineering")
	configuration := config.Configuration{
		Profiles:      []profile.Profile{teamA, teamB},
		ActiveProfile: teamA.Name,
	}
	loader := &fakeConfigurationLoader{configuration: configuration, events: &events}
	preflight := &fakeCredentialPreflight{token: "synthetic-team-b-token", events: &events}
	vault := &fakeDelegateVault{events: &events}
	handler := NewDelegateHandler(DelegateDependencies{Profiles: loader, Preflight: preflight, Vault: vault})

	if err := handler(context.Background(), []string{"--profile", "team-b", "read", "secret/example"}); err != nil {
		t.Fatalf("delegate error = %v", err)
	}

	if preflight.selected != teamB {
		t.Errorf("selected profile = %#v, want %#v", preflight.selected, teamB)
	}
	if got, want := vault.invocation.Arguments, []string{"read", "secret/example"}; !reflect.DeepEqual(got, want) {
		t.Errorf("arguments = %#v, want %#v", got, want)
	}
	if loader.configuration.ActiveProfile != teamA.Name {
		t.Errorf("active profile = %q, want unchanged %q", loader.configuration.ActiveProfile, teamA.Name)
	}
}

func TestDelegateRejectsInvalidSelectionBeforeCredentialAccess(t *testing.T) {
	tests := []struct {
		name          string
		arguments     []string
		configuration config.Configuration
		loadErr       error
		wantError     []string
		wantLoads     int
	}{
		{
			name:      "missing Vault command",
			wantError: []string{"Vault command"},
		},
		{
			name:      "profile flag without a name",
			arguments: []string{"--profile"},
			wantError: []string{"--profile", "name"},
		},
		{
			name:      "profile override without a Vault command",
			arguments: []string{"--profile", "team-a"},
			wantError: []string{"Vault command"},
		},
		{
			name:      "empty profile override",
			arguments: []string{"--profile", "", "status"},
			wantError: []string{"--profile", "invalid"},
		},
		{
			name:      "numeric profile override",
			arguments: []string{"--profile", "1", "status"},
			wantError: []string{"--profile", "invalid"},
		},
		{
			name:          "no active profile",
			arguments:     []string{"status"},
			configuration: config.Configuration{Profiles: []profile.Profile{delegateTestProfile("team-a", "https://team-a.example", "")}},
			wantError:     []string{"profile add", "switch"},
			wantLoads:     1,
		},
		{
			name:          "unknown override",
			arguments:     []string{"--profile", "missing", "status"},
			configuration: config.Configuration{Profiles: []profile.Profile{delegateTestProfile("team-a", "https://team-a.example", "")}, ActiveProfile: "team-a"},
			wantError:     []string{"profile", "not found"},
			wantLoads:     1,
		},
		{
			name:      "configuration load failure",
			arguments: []string{"status"},
			loadErr:   errors.New("configuration unavailable"),
			wantError: []string{"load", "configuration unavailable"},
			wantLoads: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := []string{}
			loader := &fakeConfigurationLoader{configuration: tt.configuration, err: tt.loadErr, events: &events}
			preflight := &fakeCredentialPreflight{events: &events}
			vault := &fakeDelegateVault{events: &events}
			handler := NewDelegateHandler(DelegateDependencies{Profiles: loader, Preflight: preflight, Vault: vault})

			err := handler(context.Background(), tt.arguments)
			if err == nil {
				t.Fatal("delegate error = nil, want failure")
			}
			for _, want := range tt.wantError {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("delegate error = %q, want text %q", err, want)
				}
			}
			if loader.calls != tt.wantLoads {
				t.Errorf("configuration loads = %d, want %d", loader.calls, tt.wantLoads)
			}
			if vault.findCalls != 0 || preflight.calls != 0 || vault.executeCalls != 0 {
				t.Errorf("external calls: find=%d preflight=%d execute=%d, want none", vault.findCalls, preflight.calls, vault.executeCalls)
			}
		})
	}
}

func TestDelegateDiscoversVaultBeforePreflightAndStopsOnFailure(t *testing.T) {
	events := []string{}
	teamA := delegateTestProfile("team-a", "https://team-a.example", "")
	loader := &fakeConfigurationLoader{
		configuration: config.Configuration{Profiles: []profile.Profile{teamA}, ActiveProfile: teamA.Name},
		events:        &events,
	}
	preflight := &fakeCredentialPreflight{events: &events}
	vault := &fakeDelegateVault{findErr: errors.New("Vault CLI missing"), events: &events}
	handler := NewDelegateHandler(DelegateDependencies{Profiles: loader, Preflight: preflight, Vault: vault})

	err := handler(context.Background(), []string{"status"})
	if err == nil || !strings.Contains(err.Error(), "Vault CLI missing") {
		t.Fatalf("delegate error = %v, want missing Vault failure", err)
	}
	if got, want := events, []string{"load", "find"}; !reflect.DeepEqual(got, want) {
		t.Errorf("events = %#v, want %#v", got, want)
	}
	if preflight.calls != 0 || vault.executeCalls != 0 {
		t.Errorf("calls after failed discovery: preflight=%d execute=%d, want none", preflight.calls, vault.executeCalls)
	}
}

func TestDelegateStopsAfterPreflightFailure(t *testing.T) {
	events := []string{}
	teamA := delegateTestProfile("team-a", "https://team-a.example", "")
	loader := &fakeConfigurationLoader{
		configuration: config.Configuration{Profiles: []profile.Profile{teamA}, ActiveProfile: teamA.Name},
		events:        &events,
	}
	preflight := &fakeCredentialPreflight{err: errors.New("credential unavailable"), events: &events}
	vault := &fakeDelegateVault{events: &events}
	handler := NewDelegateHandler(DelegateDependencies{Profiles: loader, Preflight: preflight, Vault: vault})

	err := handler(context.Background(), []string{"read", "secret/example"})
	if err == nil || !strings.Contains(err.Error(), "credential unavailable") {
		t.Fatalf("delegate error = %v, want preflight failure", err)
	}
	if got, want := events, []string{"load", "find", "preflight"}; !reflect.DeepEqual(got, want) {
		t.Errorf("events = %#v, want %#v", got, want)
	}
	if vault.executeCalls != 0 {
		t.Errorf("delegated calls = %d, want 0", vault.executeCalls)
	}
}

func TestDelegateReturnsDelegatedFailureWithoutRetry(t *testing.T) {
	events := []string{}
	teamA := delegateTestProfile("team-a", "https://team-a.example", "")
	wantErr := delegateExitError(23)
	loader := &fakeConfigurationLoader{
		configuration: config.Configuration{Profiles: []profile.Profile{teamA}, ActiveProfile: teamA.Name},
		events:        &events,
	}
	preflight := &fakeCredentialPreflight{token: "synthetic-token", events: &events}
	vault := &fakeDelegateVault{executeErr: wantErr, events: &events}
	handler := NewDelegateHandler(DelegateDependencies{Profiles: loader, Preflight: preflight, Vault: vault})

	err := handler(context.Background(), []string{"write", "secret/example", "value=test"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("delegate error = %v, want %v", err, wantErr)
	}
	if got := vaultexec.ExitCode(err); got != 23 {
		t.Errorf("exit code = %d, want 23", got)
	}
	if vault.executeCalls != 1 {
		t.Errorf("delegated calls = %d, want exactly 1", vault.executeCalls)
	}
}

func delegateTestProfile(name, address, namespace string) profile.Profile {
	return profile.Profile{
		Name:      name,
		Address:   address,
		Username:  "example-user",
		AuthPath:  "oidc",
		Namespace: namespace,
	}
}

type delegateExitError int

func (e delegateExitError) Error() string { return "delegated Vault failure" }
func (e delegateExitError) ExitCode() int { return int(e) }
