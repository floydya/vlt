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
	loads         int
}

func (f *fakeProfileStore) Load(context.Context) (config.Configuration, error) {
	f.loads++
	configuration := f.configuration
	configuration.Profiles = append([]profile.Profile(nil), configuration.Profiles...)
	return configuration, f.loadErr
}

func TestProfileHandlerDisplaysContextualHelpWithoutCallingServices(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      []string
	}{
		{name: "profile", arguments: []string{"--help"}, want: []string{"Manage Vault profiles", "Usage:", "Commands:", "Examples:"}},
		{name: "add", arguments: []string{"add", "--help"}, want: []string{"Add a Vault profile", "--address", "--username", "Examples:"}},
		{name: "list", arguments: []string{"list", "--help"}, want: []string{"List Vault profiles", "Usage:", "Examples:"}},
		{name: "show", arguments: []string{"show", "--help"}, want: []string{"Show a Vault profile", "Usage:", "Examples:"}},
		{name: "update", arguments: []string{"update", "--help"}, want: []string{"Update a Vault profile", "--namespace", "Examples:"}},
		{name: "remove", arguments: []string{"remove", "--help"}, want: []string{"Remove a Vault profile", "Usage:", "Examples:"}},
	}

	for _, tt := range tests {
		for _, helpFlag := range []string{"-h", "--help"} {
			t.Run(tt.name+helpFlag, func(t *testing.T) {
				store := &fakeProfileStore{}
				mutator := &fakeProfileMutator{}
				var output bytes.Buffer
				arguments := append([]string(nil), tt.arguments...)
				arguments[len(arguments)-1] = helpFlag
				handler := NewProfileHandler(ProfileDependencies{Profiles: store, Mutations: mutator, Output: &output})

				if err := handler(context.Background(), arguments); err != nil {
					t.Fatalf("profile help error = %v", err)
				}
				for _, want := range tt.want {
					if !strings.Contains(output.String(), want) {
						t.Errorf("help output = %q, want text %q", output.String(), want)
					}
				}
				if store.loads != 0 || len(mutator.added)+len(mutator.updated)+len(mutator.removed) != 0 {
					t.Fatalf("help called a service: loads=%d mutator=%#v", store.loads, mutator)
				}
			})
		}
	}
}

func TestProfileHandlerDisplaysHelpAfterExplicitArguments(t *testing.T) {
	mutator := &fakeProfileMutator{}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Mutations: mutator, Output: &output})

	if err := handler(context.Background(), []string{"add", "team-a", "--help"}); err != nil {
		t.Fatalf("profile add help error = %v", err)
	}
	if !strings.Contains(output.String(), "Add a Vault profile") {
		t.Errorf("help output = %q, want add purpose", output.String())
	}
	if len(mutator.added) != 0 {
		t.Fatalf("help added profiles = %#v, want none", mutator.added)
	}
}

func TestSwitchHandlerDisplaysHelpWithoutLoadingProfiles(t *testing.T) {
	for _, helpFlag := range []string{"-h", "--help"} {
		t.Run(helpFlag, func(t *testing.T) {
			store := &fakeProfileStore{}
			var output bytes.Buffer
			handler := NewSwitchHandler(SwitchDependencies{Profiles: store, Output: &output})

			if err := handler(context.Background(), []string{helpFlag}); err != nil {
				t.Fatalf("switch help error = %v", err)
			}
			for _, want := range []string{"Select the active Vault profile", "Usage:", "Examples:"} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("help output = %q, want text %q", output.String(), want)
				}
			}
			if store.loads != 0 {
				t.Fatalf("switch help profile loads = %d, want 0", store.loads)
			}
		})
	}
}

func TestProfileHandlerSuggestsOneClearSubcommandTypo(t *testing.T) {
	handler := NewProfileHandler(ProfileDependencies{Output: &bytes.Buffer{}})

	err := handler(context.Background(), []string{"udpate"})
	if err == nil {
		t.Fatal("profile typo error = nil, want failure")
	}
	for _, want := range []string{"unknown profile command", "Did you mean \"update\"?", "Usage: vlt profile"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("profile typo error = %q, want text %q", err, want)
		}
	}
	if strings.Count(err.Error(), "Did you mean") != 1 {
		t.Errorf("profile typo suggestions = %q, want exactly one", err)
	}
}

func TestProfileHandlerDoesNotSuggestUnrelatedSubcommand(t *testing.T) {
	handler := NewProfileHandler(ProfileDependencies{Output: &bytes.Buffer{}})

	err := handler(context.Background(), []string{"delete"})
	if err == nil {
		t.Fatal("unknown profile command error = nil, want failure")
	}
	if strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("unknown profile command error = %q, want no suggestion", err)
	}
}

func TestProfileHandlerRedactsUnknownSubcommand(t *testing.T) {
	const token = "hvs.synthetic-profile-command-token"
	handler := NewProfileHandler(ProfileDependencies{Output: &bytes.Buffer{}})

	err := handler(context.Background(), []string{token})
	if err == nil {
		t.Fatal("unknown profile command error = nil, want failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("unknown profile command error exposed token: %q", err)
	}
}

func TestProfileHandlerRedactsInvalidArguments(t *testing.T) {
	const token = "hvs.synthetic-invalid-argument-token"
	handler := NewProfileHandler(ProfileDependencies{Output: &bytes.Buffer{}})

	err := handler(context.Background(), []string{"add", "team-a", token})
	if err == nil {
		t.Fatal("invalid profile argument error = nil, want failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("invalid profile argument error exposed token: %q", err)
	}
}

func TestManagementErrorsIncludeContextualGuidance(t *testing.T) {
	tests := []struct {
		name    string
		handler Handler
		args    []string
		want    []string
	}{
		{
			name:    "missing profile subcommand",
			handler: NewProfileHandler(ProfileDependencies{Output: &bytes.Buffer{}}),
			want:    []string{"profile command is required", "Usage: vlt profile", "Run 'vlt profile --help'"},
		},
		{
			name:    "missing add values",
			handler: NewProfileHandler(ProfileDependencies{Output: &bytes.Buffer{}}),
			args:    []string{"add"},
			want:    []string{"NAME is required", "Usage: vlt profile add", "Run 'vlt profile add --help'"},
		},
		{
			name:    "invalid switch arguments",
			handler: NewSwitchHandler(SwitchDependencies{Output: &bytes.Buffer{}}),
			args:    []string{"team-a", "extra"},
			want:    []string{"unexpected argument", "Usage: vlt switch", "Run 'vlt switch --help'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.handler(context.Background(), tt.args)
			if err == nil {
				t.Fatal("management error = nil, want failure")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("management error = %q, want text %q", err, want)
				}
			}
		})
	}
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
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles: []profile.Profile{
			{Name: "team-b", Address: "https://vault.example.net", Username: "bob", AuthPath: "oidc"},
			{Name: "Alpha", Address: "https://vault.example.org", Username: "hvs.synthetic-list-token", AuthPath: "oidc"},
			{Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc", Namespace: "platform"},
		},
		ActiveProfile: "team-a",
	}}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Profiles: store, Output: &output})

	if err := handler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("profile list error = %v", err)
	}
	want := "#  ACTIVE  NAME    ADDRESS                    NAMESPACE\n" +
		"1          Alpha   https://vault.example.org  -\n" +
		"2  *       team-a  https://vault.example.com  platform\n" +
		"3          team-b  https://vault.example.net  -\n"
	if got := output.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	if strings.Contains(output.String(), "synthetic-list-token") {
		t.Fatalf("profile list exposed username token canary: %q", output.String())
	}
}

func TestProfileHandlerListPrintsColumnsWhenEmpty(t *testing.T) {
	store := &fakeProfileStore{}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Profiles: store, Output: &output})

	if err := handler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("profile list error = %v", err)
	}
	if got, want := output.String(), "#  ACTIVE  NAME  ADDRESS  NAMESPACE\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestProfileHandlerShowPrintsOnlyProfileMetadata(t *testing.T) {
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles: []profile.Profile{{
			Name: "team-a", Address: "https://vault.example.com", Username: "alice",
			AuthPath: "company-oidc", Namespace: "engineering",
		}},
		ActiveProfile: "team-a",
	}}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Profiles: store, Output: &output})

	if err := handler(context.Background(), []string{"show", "team-a"}); err != nil {
		t.Fatalf("profile show error = %v", err)
	}
	want := "Name:      team-a\nAddress:   https://vault.example.com\nUsername:  alice\nAuth path: company-oidc\nNamespace: engineering\nActive:    yes\n"
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
			want: "Active profile: team-b\n" +
				"#  ACTIVE  NAME    ADDRESS                           NAMESPACE\n" +
				"1          team-a  https://vault.example.com/team-a  -\n" +
				"2  *       team-b  https://vault.example.com/team-b  -\n",
		},
		{name: "no active profile or profiles", want: "Active profile: none\n#  ACTIVE  NAME  ADDRESS  NAMESPACE\n"},
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
