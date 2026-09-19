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
)

type fakeProfileStore struct {
	configuration config.Configuration
	loadErr       error
	selectErr     error
	selectors     []string
}

func (f *fakeProfileStore) Load(context.Context) (config.Configuration, error) {
	configuration := f.configuration
	configuration.Profiles = append([]profile.Profile(nil), configuration.Profiles...)
	return configuration, f.loadErr
}

func (f *fakeProfileStore) SetActiveProfile(_ context.Context, selector string) error {
	f.selectors = append(f.selectors, selector)
	if f.selectErr != nil {
		return f.selectErr
	}
	selected, err := profile.NewService(f.configuration.Profiles).Resolve(selector)
	if err != nil {
		return err
	}
	f.configuration.ActiveProfile = selected.Name
	return nil
}

type fakeProfileMutator struct {
	added     []profile.Profile
	updated   []profileUpdate
	removed   []string
	addErr    error
	updateErr error
	removeErr error
}

type profileUpdate struct {
	name    string
	changes profile.ProfileChanges
}

func (f *fakeProfileMutator) Add(_ context.Context, candidate profile.Profile) error {
	f.added = append(f.added, candidate)
	return f.addErr
}

func (f *fakeProfileMutator) Update(_ context.Context, name string, changes profile.ProfileChanges) error {
	f.updated = append(f.updated, profileUpdate{name: name, changes: changes})
	return f.updateErr
}

func (f *fakeProfileMutator) Remove(_ context.Context, name string) error {
	f.removed = append(f.removed, name)
	return f.removeErr
}

func TestProfileHandlerAddParsesRequiredAndOptionalFields(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      profile.Profile
	}{
		{
			name:      "defaults auth path and namespace",
			arguments: []string{"add", "team-a", "--address", "https://vault.example.com", "--username", "alice"},
			want: profile.Profile{
				Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc",
			},
		},
		{
			name: "accepts optional fields",
			arguments: []string{
				"add", "team-b", "--address=https://vault.example.com", "--username=bob",
				"--auth-path=company-oidc", "--namespace=engineering",
			},
			want: profile.Profile{
				Name: "team-b", Address: "https://vault.example.com", Username: "bob",
				AuthPath: "company-oidc", Namespace: "engineering",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutator := &fakeProfileMutator{}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{Mutations: mutator, Output: &output})

			if err := handler(context.Background(), tt.arguments); err != nil {
				t.Fatalf("profile add error = %v", err)
			}
			if !reflect.DeepEqual(mutator.added, []profile.Profile{tt.want}) {
				t.Fatalf("added profiles = %#v, want %#v", mutator.added, []profile.Profile{tt.want})
			}
			if got, want := output.String(), "Added profile \""+tt.want.Name+"\".\n"; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestProfileHandlerListUsesStableNumberedOrder(t *testing.T) {
	store := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{
		managementTestProfile("team-b"),
		managementTestProfile("Alpha"),
		managementTestProfile("team-a"),
	}}}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Profiles: store, Output: &output})

	if err := handler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("profile list error = %v", err)
	}
	if got, want := output.String(), "1. Alpha\n2. team-a\n3. team-b\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestProfileHandlerListPrintsNothingWhenEmpty(t *testing.T) {
	store := &fakeProfileStore{}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Profiles: store, Output: &output})

	if err := handler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("profile list error = %v", err)
	}
	if output.Len() != 0 {
		t.Errorf("output = %q, want empty", output.String())
	}
}

func TestProfileHandlerShowPrintsOnlyProfileMetadata(t *testing.T) {
	store := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{{
		Name: "team-a", Address: "https://vault.example.com", Username: "alice",
		AuthPath: "company-oidc", Namespace: "engineering",
	}}}}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Profiles: store, Output: &output})

	if err := handler(context.Background(), []string{"show", "team-a"}); err != nil {
		t.Fatalf("profile show error = %v", err)
	}
	want := "Name: team-a\nAddress: https://vault.example.com\nUsername: alice\nAuth path: company-oidc\nNamespace: engineering\n"
	if got := output.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"token", "keyring", "credential"} {
		if strings.Contains(strings.ToLower(output.String()), forbidden) {
			t.Errorf("output exposed %q: %q", forbidden, output.String())
		}
	}
}

func TestProfileHandlerUpdatePassesOnlySuppliedFields(t *testing.T) {
	mutator := &fakeProfileMutator{}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Mutations: mutator, Output: &output})

	arguments := []string{"update", "team-a", "--address", "https://new.example.com", "--namespace="}
	if err := handler(context.Background(), arguments); err != nil {
		t.Fatalf("profile update error = %v", err)
	}
	if len(mutator.updated) != 1 {
		t.Fatalf("updates = %#v, want one", mutator.updated)
	}
	got := mutator.updated[0]
	if got.name != "team-a" {
		t.Errorf("updated name = %q, want team-a", got.name)
	}
	if got.changes.Address == nil || *got.changes.Address != "https://new.example.com" {
		t.Errorf("address change = %#v, want supplied address", got.changes.Address)
	}
	if got.changes.Namespace == nil || *got.changes.Namespace != "" {
		t.Errorf("namespace change = %#v, want explicit empty value", got.changes.Namespace)
	}
	if got.changes.Username != nil || got.changes.AuthPath != nil {
		t.Errorf("omitted changes = %#v, want nil", got.changes)
	}
	if got, want := output.String(), "Updated profile \"team-a\".\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestProfileHandlerRemoveCallsMutation(t *testing.T) {
	mutator := &fakeProfileMutator{}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Mutations: mutator, Output: &output})

	if err := handler(context.Background(), []string{"remove", "team-a"}); err != nil {
		t.Fatalf("profile remove error = %v", err)
	}
	if !reflect.DeepEqual(mutator.removed, []string{"team-a"}) {
		t.Errorf("removed profiles = %#v, want team-a", mutator.removed)
	}
	if got, want := output.String(), "Removed profile \"team-a\".\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestProfileHandlerRejectsInvalidCommandForms(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      string
	}{
		{name: "missing subcommand", want: "profile command"},
		{name: "unknown subcommand", arguments: []string{"rename"}, want: "unknown profile command"},
		{name: "add missing name", arguments: []string{"add"}, want: "profile add NAME"},
		{name: "add missing address", arguments: []string{"add", "team-a", "--username", "alice"}, want: "--address"},
		{name: "add missing username", arguments: []string{"add", "team-a", "--address", "https://vault.example.com"}, want: "--username"},
		{name: "add extra argument", arguments: []string{"add", "team-a", "extra", "--address", "x", "--username", "y"}, want: "profile add NAME"},
		{name: "list extra argument", arguments: []string{"list", "extra"}, want: "profile list"},
		{name: "show missing name", arguments: []string{"show"}, want: "profile show NAME"},
		{name: "show extra argument", arguments: []string{"show", "team-a", "extra"}, want: "profile show NAME"},
		{name: "update missing name", arguments: []string{"update"}, want: "profile update NAME"},
		{name: "update unknown flag", arguments: []string{"update", "team-a", "--token", "secret"}, want: "token"},
		{name: "remove missing name", arguments: []string{"remove"}, want: "profile remove NAME"},
		{name: "remove extra argument", arguments: []string{"remove", "team-a", "extra"}, want: "profile remove NAME"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProfileStore{}
			mutator := &fakeProfileMutator{}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{Profiles: store, Mutations: mutator, Output: &output})

			err := handler(context.Background(), tt.arguments)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want text %q", err, tt.want)
			}
			if len(mutator.added)+len(mutator.updated)+len(mutator.removed) != 0 {
				t.Fatalf("invalid command called mutation: %#v", mutator)
			}
			if output.Len() != 0 {
				t.Errorf("output = %q, want empty", output.String())
			}
		})
	}
}

func TestProfileHandlerRedactsMutationFailure(t *testing.T) {
	const token = "synthetic-management-token"
	mutator := &fakeProfileMutator{removeErr: errors.New("keyring delete failed with VAULT_TOKEN=" + token)}
	handler := NewProfileHandler(ProfileDependencies{Mutations: mutator, Output: &bytes.Buffer{}})

	err := handler(context.Background(), []string{"remove", "team-a"})
	if err == nil {
		t.Fatal("profile remove error = nil, want failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("profile remove error exposed token: %q", err)
	}
}

func TestSwitchHandlerDisplaysActiveProfileAndProfileList(t *testing.T) {
	tests := []struct {
		name          string
		configuration config.Configuration
		want          string
	}{
		{
			name: "active profile and sorted list",
			configuration: config.Configuration{
				Profiles:      []profile.Profile{managementTestProfile("team-b"), managementTestProfile("team-a")},
				ActiveProfile: "team-b",
			},
			want: "Active profile: team-b\n1. team-a\n2. team-b\n",
		},
		{name: "no active profile or profiles", want: "Active profile: none\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProfileStore{configuration: tt.configuration}
			var output bytes.Buffer
			handler := NewSwitchHandler(SwitchDependencies{Profiles: store, Output: &output})

			if err := handler(context.Background(), nil); err != nil {
				t.Fatalf("switch error = %v", err)
			}
			if got := output.String(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSwitchHandlerSelectsNamesAndNumbers(t *testing.T) {
	for _, tt := range []struct {
		selector string
		wantName string
	}{
		{selector: "team-b", wantName: "team-b"},
		{selector: "1", wantName: "team-a"},
	} {
		t.Run(tt.selector, func(t *testing.T) {
			store := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{
				managementTestProfile("team-b"), managementTestProfile("team-a"),
			}}}
			var output bytes.Buffer
			handler := NewSwitchHandler(SwitchDependencies{Profiles: store, Output: &output})

			if err := handler(context.Background(), []string{tt.selector}); err != nil {
				t.Fatalf("switch %q error = %v", tt.selector, err)
			}
			if !reflect.DeepEqual(store.selectors, []string{tt.wantName}) {
				t.Errorf("persisted selectors = %#v, want stable name %q", store.selectors, tt.wantName)
			}
			if store.configuration.ActiveProfile != tt.wantName {
				t.Errorf("active profile = %q, want %q", store.configuration.ActiveProfile, tt.wantName)
			}
			if got, want := output.String(), "Switched to profile \""+tt.wantName+"\".\n"; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestSwitchHandlerRejectsInvalidSelectorsWithoutChangingSelection(t *testing.T) {
	for _, arguments := range [][]string{{"missing"}, {"0"}, {"2"}, {"team-a", "extra"}} {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			store := &fakeProfileStore{configuration: config.Configuration{
				Profiles: []profile.Profile{managementTestProfile("team-a")}, ActiveProfile: "team-a",
			}}
			handler := NewSwitchHandler(SwitchDependencies{Profiles: store, Output: &bytes.Buffer{}})

			if err := handler(context.Background(), arguments); err == nil {
				t.Fatalf("switch %v error = nil, want rejection", arguments)
			}
			if len(store.selectors) != 0 {
				t.Errorf("invalid selector persisted selections = %#v", store.selectors)
			}
			if store.configuration.ActiveProfile != "team-a" {
				t.Errorf("active profile = %q, want unchanged", store.configuration.ActiveProfile)
			}
		})
	}
}

func TestSwitchHandlerRedactsTokenLikeInvalidSelector(t *testing.T) {
	const token = "hvs.synthetic-switch-token"
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles: []profile.Profile{managementTestProfile("team-a")}, ActiveProfile: "team-a",
	}}
	handler := NewSwitchHandler(SwitchDependencies{Profiles: store, Output: &bytes.Buffer{}})

	err := handler(context.Background(), []string{token})
	if err == nil {
		t.Fatal("switch error = nil, want rejection")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("switch error exposed token-like selector: %q", err)
	}
}

func managementTestProfile(name string) profile.Profile {
	return profile.Profile{
		Name: name, Address: "https://vault.example.com/" + name, Username: "user-" + name, AuthPath: "oidc",
	}
}
