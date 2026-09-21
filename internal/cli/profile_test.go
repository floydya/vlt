package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"

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

type fakeProfileSelector struct {
	profiles []profile.Profile
	names    []string
	active   string
	selected string
	err      error
	calls    int
}

type fakeProfileForm struct {
	request ProfileFormRequest
	result  profile.Profile
	err     error
	calls   int
}

func (f *fakeProfileForm) Run(_ context.Context, request ProfileFormRequest) (profile.Profile, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

func (f *fakeProfileSelector) Select(_ context.Context, candidates []profile.Profile, active string) (string, error) {
	f.calls++
	f.profiles = append([]profile.Profile(nil), candidates...)
	f.names = make([]string, len(candidates))
	for index, candidate := range candidates {
		f.names[index] = candidate.Name
	}
	f.active = active
	return f.selected, f.err
}

type switchTerminal struct {
	prompts bool
	color   bool
}

func requireAutomaticHelp(t *testing.T, err error, want string) {
	t.Helper()
	var automaticHelp AutomaticHelp
	if !errors.As(err, &automaticHelp) {
		t.Fatalf("error = %v, want AutomaticHelp", err)
	}
	if automaticHelp.Text != want {
		t.Errorf("automatic help = %q, want %q", automaticHelp.Text, want)
	}
}

func (switchTerminal) InputIsTerminal() bool   { return false }
func (switchTerminal) DisplayIsTerminal() bool { return false }
func (t switchTerminal) PromptsEnabled() bool  { return t.prompts }
func (t switchTerminal) ColorEnabled() bool    { return t.color }

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
		{name: "add", arguments: []string{"add", "--help"}, want: []string{"Add a Vault profile", "--address", "--username", "--color", "Examples:"}},
		{name: "list", arguments: []string{"list", "--help"}, want: []string{"List Vault profiles", "Usage:", "Examples:"}},
		{name: "show", arguments: []string{"show", "--help"}, want: []string{"Show a Vault profile", "Usage:", "Examples:"}},
		{name: "update", arguments: []string{"update", "--help"}, want: []string{"Update a Vault profile", "--namespace", "--color", "Examples:"}},
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

func TestProfileHandlerReturnsAutomaticHelpForMissingSubcommand(t *testing.T) {
	store := &fakeProfileStore{}
	mutator := &fakeProfileMutator{}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Profiles: store, Mutations: mutator, Output: &output})

	err := handler(context.Background(), nil)

	requireAutomaticHelp(t, err, profileHelpText)
	if output.Len() != 0 || store.loads != 0 || len(mutator.added)+len(mutator.updated)+len(mutator.removed) != 0 {
		t.Errorf("missing subcommand used services: output=%q loads=%d mutator=%#v", output.String(), store.loads, mutator)
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

func TestProfileMutationSuccessOutputUsesSharedStatusPresentation(t *testing.T) {
	tests := []struct {
		name string
		want string
		run  func(*bytes.Buffer) error
	}{
		{
			name: "add",
			want: "Added profile \"team-a\".\n",
			run: func(output *bytes.Buffer) error {
				handler := NewProfileHandler(ProfileDependencies{
					Mutations: &fakeProfileMutator{}, Output: output, Terminal: switchTerminal{color: true},
				})
				return handler(context.Background(), []string{
					"add", "team-a", "--address", "https://vault.example.com", "--username", "alice",
				})
			},
		},
		{
			name: "update",
			want: "Updated profile \"team-a\".\n",
			run: func(output *bytes.Buffer) error {
				handler := NewProfileHandler(ProfileDependencies{
					Mutations: &fakeProfileMutator{}, Output: output, Terminal: switchTerminal{color: true},
				})
				return handler(context.Background(), []string{"update", "team-a", "--namespace", "platform"})
			},
		},
		{
			name: "remove",
			want: "Removed profile \"team-a\".\n",
			run: func(output *bytes.Buffer) error {
				handler := NewProfileHandler(ProfileDependencies{
					Mutations: &fakeProfileMutator{}, Output: output, Terminal: switchTerminal{color: true},
				})
				return handler(context.Background(), []string{"remove", "team-a"})
			},
		},
		{
			name: "switch",
			want: "Switched to profile \"team-a\".\n",
			run: func(output *bytes.Buffer) error {
				store := &fakeProfileStore{configuration: config.Configuration{
					Profiles: []profile.Profile{managementTestProfile("team-a")},
				}}
				handler := NewSwitchHandler(SwitchDependencies{
					Profiles: store, Output: output, Terminal: switchTerminal{color: true},
				})
				return handler(context.Background(), []string{"team-a"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := tt.run(&output); err != nil {
				t.Fatalf("mutation error = %v", err)
			}
			if !strings.Contains(output.String(), "\x1b[") {
				t.Fatalf("styled output contains no ANSI: %q", output.String())
			}
			if got := ansi.Strip(output.String()); got != tt.want {
				t.Fatalf("unstyled output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProfileCancellationUsesOneDiagnosticAndNoSuccessOutput(t *testing.T) {
	tests := []struct {
		name  string
		cause error
		run   func(*bytes.Buffer, error) error
	}{
		{
			name: "add form", cause: huh.ErrUserAborted,
			run: func(output *bytes.Buffer, cause error) error {
				handler := NewProfileHandler(ProfileDependencies{
					Mutations: &fakeProfileMutator{}, Output: output, Terminal: switchTerminal{prompts: true},
					Form: &fakeProfileForm{err: cause},
				})
				return handler(context.Background(), []string{"add"})
			},
		},
		{
			name: "update form", cause: huh.ErrUserAborted,
			run: func(output *bytes.Buffer, cause error) error {
				store := &fakeProfileStore{configuration: config.Configuration{
					Profiles: []profile.Profile{managementTestProfile("team-a")},
				}}
				handler := NewProfileHandler(ProfileDependencies{
					Profiles: store, Mutations: &fakeProfileMutator{}, Output: output,
					Terminal: switchTerminal{prompts: true}, Form: &fakeProfileForm{err: cause},
				})
				return handler(context.Background(), []string{"update", "team-a"})
			},
		},
		{
			name: "remove confirmation", cause: huh.ErrUserAborted,
			run: func(output *bytes.Buffer, cause error) error {
				store := &fakeProfileStore{configuration: config.Configuration{
					Profiles: []profile.Profile{managementTestProfile("team-a")}, ActiveProfile: "team-a",
				}}
				handler := NewProfileHandler(ProfileDependencies{
					Profiles: store, Mutations: &fakeProfileMutator{}, Output: output,
					Terminal: switchTerminal{prompts: true}, Selector: &fakeProfileSelector{selected: "team-a"},
					RemovalConfirmer: &fakeProfileRemovalConfirmer{err: cause},
				})
				return handler(context.Background(), []string{"remove"})
			},
		},
		{
			name: "switch selector", cause: ErrSharedSelectorCanceled,
			run: func(output *bytes.Buffer, cause error) error {
				store := &fakeProfileStore{configuration: config.Configuration{
					Profiles: []profile.Profile{managementTestProfile("team-a")}, ActiveProfile: "team-a",
				}}
				handler := NewSwitchHandler(SwitchDependencies{
					Profiles: store, Output: output, Terminal: switchTerminal{prompts: true},
					Selector: &fakeProfileSelector{err: cause},
				})
				return handler(context.Background(), nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := tt.run(&output, tt.cause)
			if err == nil || err.Error() != "operation canceled" {
				t.Fatalf("cancellation error = %v, want operation canceled", err)
			}
			if !errors.Is(err, tt.cause) {
				t.Fatalf("cancellation error = %v, want cause %v", err, tt.cause)
			}
			if output.Len() != 0 {
				t.Fatalf("cancellation output = %q, want no success output", output.String())
			}
		})
	}
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
			name: "accepts optional fields and insecure HTTP opt-in",
			arguments: []string{
				"add", "team-b", "--address=http://127.0.0.1:8200", "--username=bob",
				"--auth-path=company-oidc", "--namespace=engineering", "--allow-insecure",
			},
			want: profile.Profile{
				Name: "team-b", Address: "http://127.0.0.1:8200", Username: "bob",
				AuthPath: "company-oidc", Namespace: "engineering", AllowInsecure: true,
			},
		},
		{
			name:      "accepts a profile color",
			arguments: []string{"add", "team-c", "--address", "https://vault.example.com", "--username", "carol", "--color", "#a1B2c3"},
			want: profile.Profile{
				Name: "team-c", Address: "https://vault.example.com", Username: "carol", AuthPath: "oidc", Color: "#a1B2c3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutator := &fakeProfileMutator{}
			form := &fakeProfileForm{}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{Mutations: mutator, Output: &output, Form: form})

			if err := handler(context.Background(), tt.arguments); err != nil {
				t.Fatalf("profile add error = %v", err)
			}
			if !reflect.DeepEqual(mutator.added, []profile.Profile{tt.want}) {
				t.Fatalf("added profiles = %#v, want %#v", mutator.added, []profile.Profile{tt.want})
			}
			if got, want := output.String(), "Added profile \""+tt.want.Name+"\".\n"; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
			if form.calls != 0 {
				t.Errorf("form calls = %d, want 0 for complete explicit add", form.calls)
			}
		})
	}
}

func TestProfileHandlerAddCollectsMissingFieldsInteractively(t *testing.T) {
	completed := profile.Profile{
		Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc",
	}
	tests := []struct {
		name      string
		arguments []string
		wantStart profile.Profile
	}{
		{
			name:      "preserves supplied values",
			arguments: []string{"add", "team-a", "--address", "https://vault.example.com"},
			wantStart: profile.Profile{Name: "team-a", Address: "https://vault.example.com", AuthPath: "oidc"},
		},
		{
			name:      "collects a missing name",
			arguments: []string{"add", "--address", "https://vault.example.com", "--username", "alice"},
			wantStart: profile.Profile{Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc"},
		},
		{
			name:      "collects all required values",
			arguments: []string{"add"},
			wantStart: profile.Profile{AuthPath: "oidc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutator := &fakeProfileMutator{}
			form := &fakeProfileForm{result: completed}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{
				Mutations: mutator, Output: &output, Terminal: switchTerminal{prompts: true}, Form: form,
			})

			if err := handler(context.Background(), tt.arguments); err != nil {
				t.Fatalf("profile add error = %v", err)
			}
			if !reflect.DeepEqual(form.request.Profile, tt.wantStart) {
				t.Errorf("form initial profile = %#v, want %#v", form.request.Profile, tt.wantStart)
			}
			if !form.request.NameEditable {
				t.Error("add form name is read-only, want editable")
			}
			if !reflect.DeepEqual(mutator.added, []profile.Profile{completed}) {
				t.Errorf("added profiles = %#v, want completed profile", mutator.added)
			}
			if got, want := output.String(), "Added profile \"team-a\".\n"; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestProfileHandlerAddRejectsMissingInputOutsideTerminal(t *testing.T) {
	for _, tt := range []struct {
		name      string
		arguments []string
	}{
		{name: "all required values", arguments: []string{"add"}},
		{name: "name", arguments: []string{"add", "--address", "https://vault.example.com", "--username", "alice"}},
		{name: "address", arguments: []string{"add", "team-a", "--username", "alice"}},
		{name: "username", arguments: []string{"add", "team-a", "--address", "https://vault.example.com"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mutator := &fakeProfileMutator{}
			form := &fakeProfileForm{result: managementTestProfile("team-a")}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{
				Mutations: mutator, Output: &output, Terminal: switchTerminal{}, Form: form,
			})

			err := handler(context.Background(), tt.arguments)

			requireAutomaticHelp(t, err, profileAddHelpText)
			if form.calls != 0 {
				t.Errorf("form calls = %d, want 0", form.calls)
			}
			if len(mutator.added) != 0 {
				t.Errorf("added profiles = %#v, want none", mutator.added)
			}
			if output.Len() != 0 {
				t.Errorf("output = %q, want empty", output.String())
			}
		})
	}
}

func TestProfileHandlerAddCancelAndInterruptDoNotMutate(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "cancel", err: errors.New("user aborted with hvs.synthetic-add-form-token")},
		{name: "interrupt", err: context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mutator := &fakeProfileMutator{}
			form := &fakeProfileForm{err: tt.err}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{
				Mutations: mutator, Output: &output, Terminal: switchTerminal{prompts: true}, Form: form,
			})

			err := handler(context.Background(), []string{"add"})
			if err == nil {
				t.Fatal("profile add error = nil, want form failure")
			}
			if strings.Contains(err.Error(), "synthetic-add-form-token") {
				t.Fatalf("profile add error exposed token: %q", err)
			}
			if len(mutator.added) != 0 {
				t.Errorf("added profiles = %#v, want none", mutator.added)
			}
			if output.Len() != 0 {
				t.Errorf("output = %q, want empty", output.String())
			}
		})
	}
}

func TestProfileHandlerAddRedactsMutationFailureAfterForm(t *testing.T) {
	const token = "hvs.synthetic-add-mutation-token"
	mutator := &fakeProfileMutator{addErr: errors.New("authenticate with VAULT_TOKEN=" + token)}
	form := &fakeProfileForm{result: managementTestProfile("team-a")}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{
		Mutations: mutator, Output: &output, Terminal: switchTerminal{prompts: true}, Form: form,
	})

	err := handler(context.Background(), []string{"add"})
	if err == nil {
		t.Fatal("profile add error = nil, want mutation failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("profile add error exposed token: %q", err)
	}
	if output.Len() != 0 {
		t.Errorf("output = %q, want empty", output.String())
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
	want := "#  ACTIVE  NAME    ADDRESS                    NAMESPACE  ALLOW HTTP  COLOR\n" +
		"1          Alpha   https://vault.example.org  -          no          -\n" +
		"2  *       team-a  https://vault.example.com  platform   no          -\n" +
		"3          team-b  https://vault.example.net  -          no          -\n"
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
	if got, want := output.String(), "#  ACTIVE  NAME  ADDRESS  NAMESPACE  ALLOW HTTP  COLOR\n"; got != want {
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
	selector := &fakeProfileSelector{selected: "team-b"}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Profiles: store, Output: &output, Selector: selector})

	if err := handler(context.Background(), []string{"show", "team-a"}); err != nil {
		t.Fatalf("profile show error = %v", err)
	}
	want := "Name:       team-a\nAddress:    https://vault.example.com\nUsername:   alice\nAuth path:  company-oidc\nNamespace:  engineering\nAllow HTTP: no\nColor:      -\nActive:     yes\n"
	if got := output.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	for _, forbidden := range []string{"token", "keyring", "credential"} {
		if strings.Contains(strings.ToLower(output.String()), forbidden) {
			t.Errorf("output exposed %q: %q", forbidden, output.String())
		}
	}
	if selector.calls != 0 {
		t.Errorf("selector calls = %d, want 0 for explicit show", selector.calls)
	}
}

func TestProfileHandlerShowSelectsInteractively(t *testing.T) {
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles: []profile.Profile{
			managementTestProfile("team-b"),
			{
				Name: "team-a", Address: "https://vault.example.com", Username: "alice",
				AuthPath: "company-oidc", Namespace: "engineering",
			},
		},
		ActiveProfile: "team-b",
	}}
	selector := &fakeProfileSelector{selected: "team-a"}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Output: &output, Terminal: switchTerminal{prompts: true}, Selector: selector,
	})

	if err := handler(context.Background(), []string{"show"}); err != nil {
		t.Fatalf("profile show error = %v", err)
	}
	if !reflect.DeepEqual(selector.names, []string{"team-a", "team-b"}) {
		t.Errorf("selector names = %#v, want sorted names", selector.names)
	}
	if selector.active != "team-b" {
		t.Errorf("selector active profile = %q, want team-b", selector.active)
	}
	want := "Name:       team-a\nAddress:    https://vault.example.com\nUsername:   alice\nAuth path:  company-oidc\nNamespace:  engineering\nAllow HTTP: no\nColor:      -\nActive:     no\n"
	if got := output.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestProfileHandlerShowRejectsMissingNameOutsideTerminal(t *testing.T) {
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles: []profile.Profile{managementTestProfile("team-a")},
	}}
	selector := &fakeProfileSelector{selected: "team-a"}
	handler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Output: &bytes.Buffer{}, Terminal: switchTerminal{}, Selector: selector,
	})

	err := handler(context.Background(), []string{"show"})
	requireAutomaticHelp(t, err, profileShowHelpText)
	if selector.calls != 0 || store.loads != 0 {
		t.Errorf("non-terminal show used services: selector calls=%d profile loads=%d", selector.calls, store.loads)
	}
}

func TestProfileHandlerShowExplainsHowToAddFirstProfile(t *testing.T) {
	store := &fakeProfileStore{}
	selector := &fakeProfileSelector{}
	handler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Output: &bytes.Buffer{}, Terminal: switchTerminal{prompts: true}, Selector: selector,
	})

	err := handler(context.Background(), []string{"show"})
	if err == nil {
		t.Fatal("profile show error = nil, want empty-profile guidance")
	}
	for _, want := range []string{"no profiles configured", "vlt profile add"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("profile show error = %q, want text %q", err, want)
		}
	}
	if selector.calls != 0 {
		t.Errorf("selector calls = %d, want 0", selector.calls)
	}
}

func TestProfileHandlerShowCancelAndInterruptEmitNothing(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "cancel", err: errors.New("user aborted with hvs.synthetic-show-token")},
		{name: "interrupt", err: context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProfileStore{configuration: config.Configuration{
				Profiles:      []profile.Profile{managementTestProfile("team-a")},
				ActiveProfile: "team-a",
			}}
			selector := &fakeProfileSelector{err: tt.err}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{
				Profiles: store, Output: &output, Terminal: switchTerminal{prompts: true}, Selector: selector,
			})

			err := handler(context.Background(), []string{"show"})
			if err == nil {
				t.Fatal("profile show error = nil, want selection failure")
			}
			if strings.Contains(err.Error(), "synthetic-show-token") {
				t.Fatalf("profile show error exposed token: %q", err)
			}
			if output.Len() != 0 {
				t.Errorf("output = %q, want empty", output.String())
			}
			if len(store.selectors) != 0 || store.configuration.ActiveProfile != "team-a" {
				t.Errorf("selection failure changed state: selectors=%#v active=%q", store.selectors, store.configuration.ActiveProfile)
			}
		})
	}
}

func TestProfileHandlerUpdatePassesOnlySuppliedFields(t *testing.T) {
	mutator := &fakeProfileMutator{}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Mutations: mutator, Output: &output})

	arguments := []string{"update", "team-a", "--address", "https://new.example.com", "--namespace=", "--allow-insecure=false", "--color=#A1B2C3"}
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
	if got.changes.AllowInsecure == nil || *got.changes.AllowInsecure {
		t.Errorf("allow insecure change = %#v, want explicit false", got.changes.AllowInsecure)
	}
	if got.changes.Color == nil || *got.changes.Color != "#A1B2C3" {
		t.Errorf("color change = %#v, want supplied color", got.changes.Color)
	}
	if got.changes.Username != nil || got.changes.AuthPath != nil {
		t.Errorf("omitted changes = %#v, want nil", got.changes)
	}
	if got, want := output.String(), "Updated profile \"team-a\".\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestProfileHandlerUpdateClearsColorExplicitly(t *testing.T) {
	mutator := &fakeProfileMutator{}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Mutations: mutator, Output: &output, Form: &fakeProfileForm{}})
	if err := handler(context.Background(), []string{"update", "team-a", "--color="}); err != nil {
		t.Fatalf("profile update error = %v", err)
	}
	if len(mutator.updated) != 1 || mutator.updated[0].changes.Color == nil || *mutator.updated[0].changes.Color != "" {
		t.Fatalf("color updates = %#v, want explicit empty color", mutator.updated)
	}
}

func TestProfileHandlerRejectsInvalidColorBeforeMutation(t *testing.T) {
	for _, arguments := range [][]string{
		{"add", "team-a", "--address", "https://vault.example.com", "--username", "alice", "--color=red"},
		{"update", "team-a", "--color=#12345G"},
	} {
		mutator := &fakeProfileMutator{}
		var output bytes.Buffer
		handler := NewProfileHandler(ProfileDependencies{Mutations: mutator, Output: &output})
		if err := handler(context.Background(), arguments); err == nil || !strings.Contains(err.Error(), "color") {
			t.Fatalf("handler(%q) error = %v, want color validation", arguments, err)
		}
		if len(mutator.added) != 0 || len(mutator.updated) != 0 || output.Len() != 0 {
			t.Fatalf("invalid color reached mutation or output: mutator=%#v output=%q", mutator, output.String())
		}
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
		{name: "unknown subcommand", arguments: []string{"rename"}, want: "unknown profile command"},
		{name: "add missing name", arguments: []string{"add"}, want: "profile add [NAME]"},
		{name: "add missing address", arguments: []string{"add", "team-a", "--username", "alice"}, want: "--address"},
		{name: "add missing username", arguments: []string{"add", "team-a", "--address", "https://vault.example.com"}, want: "--username"},
		{name: "add extra argument", arguments: []string{"add", "team-a", "extra", "--address", "x", "--username", "y"}, want: "profile add [NAME]"},
		{name: "list extra argument", arguments: []string{"list", "extra"}, want: "profile list"},
		{name: "show extra argument", arguments: []string{"show", "team-a", "extra"}, want: "profile show [NAME]"},
		{name: "update unknown flag", arguments: []string{"update", "team-a", "--token", "secret"}, want: "token"},
		{name: "remove extra argument", arguments: []string{"remove", "team-a", "extra"}, want: "profile remove [NAME]"},
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

func TestSwitchHandlerSelectsInteractivelyByStableName(t *testing.T) {
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles:      []profile.Profile{managementTestProfile("team-b"), managementTestProfile("team-a")},
		ActiveProfile: "team-b",
	}}
	selector := &fakeProfileSelector{selected: "team-a"}
	var output bytes.Buffer
	handler := NewSwitchHandler(SwitchDependencies{
		Profiles: store, Output: &output, Terminal: switchTerminal{prompts: true}, Selector: selector,
	})

	if err := handler(context.Background(), nil); err != nil {
		t.Fatalf("switch error = %v", err)
	}
	if !reflect.DeepEqual(selector.names, []string{"team-a", "team-b"}) {
		t.Errorf("selector names = %#v, want sorted names", selector.names)
	}
	if selector.active != "team-b" {
		t.Errorf("selector active profile = %q, want team-b", selector.active)
	}
	if !reflect.DeepEqual(store.selectors, []string{"team-a"}) {
		t.Errorf("persisted selectors = %#v, want stable name team-a", store.selectors)
	}
	if got, want := output.String(), "Switched to profile \"team-a\".\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestSwitchHandlerRejectsMissingSelectionOutsideTerminal(t *testing.T) {
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles: []profile.Profile{managementTestProfile("team-a")},
	}}
	selector := &fakeProfileSelector{selected: "team-a"}
	handler := NewSwitchHandler(SwitchDependencies{
		Profiles: store, Output: &bytes.Buffer{}, Terminal: switchTerminal{}, Selector: selector,
	})

	err := handler(context.Background(), nil)
	requireAutomaticHelp(t, err, switchHelpText)
	if selector.calls != 0 {
		t.Errorf("selector calls = %d, want 0", selector.calls)
	}
	if store.loads != 0 {
		t.Errorf("profile loads = %d, want 0", store.loads)
	}
	if len(store.selectors) != 0 {
		t.Errorf("persisted selectors = %#v, want none", store.selectors)
	}
}

func TestSwitchHandlerExplainsHowToAddFirstProfile(t *testing.T) {
	store := &fakeProfileStore{}
	selector := &fakeProfileSelector{}
	handler := NewSwitchHandler(SwitchDependencies{
		Profiles: store, Output: &bytes.Buffer{}, Terminal: switchTerminal{prompts: true}, Selector: selector,
	})

	err := handler(context.Background(), nil)
	if err == nil {
		t.Fatal("switch error = nil, want empty-profile guidance")
	}
	for _, want := range []string{"no profiles configured", "vlt profile add"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("switch error = %q, want text %q", err, want)
		}
	}
	if selector.calls != 0 {
		t.Errorf("selector calls = %d, want 0", selector.calls)
	}
}

func TestSwitchHandlerCancelLeavesActiveProfileUnchanged(t *testing.T) {
	const token = "hvs.synthetic-selector-token"
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles:      []profile.Profile{managementTestProfile("team-a"), managementTestProfile("team-b")},
		ActiveProfile: "team-a",
	}}
	selector := &fakeProfileSelector{err: errors.New("user aborted with " + token)}
	var output bytes.Buffer
	handler := NewSwitchHandler(SwitchDependencies{
		Profiles: store, Output: &output, Terminal: switchTerminal{prompts: true}, Selector: selector,
	})

	err := handler(context.Background(), nil)
	if err == nil {
		t.Fatal("switch error = nil, want cancellation")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("switch error exposed token: %q", err)
	}
	if len(store.selectors) != 0 || store.configuration.ActiveProfile != "team-a" {
		t.Errorf("cancel changed active profile: selectors=%#v active=%q", store.selectors, store.configuration.ActiveProfile)
	}
	if output.Len() != 0 {
		t.Errorf("output = %q, want no success message", output.String())
	}
}

func TestSwitchHandlerPrintsNoSuccessBeforePersistence(t *testing.T) {
	store := &fakeProfileStore{
		configuration: config.Configuration{Profiles: []profile.Profile{managementTestProfile("team-a")}},
		selectErr:     errors.New("save failed"),
	}
	selector := &fakeProfileSelector{selected: "team-a"}
	var output bytes.Buffer
	handler := NewSwitchHandler(SwitchDependencies{
		Profiles: store, Output: &output, Terminal: switchTerminal{prompts: true}, Selector: selector,
	})

	if err := handler(context.Background(), nil); err == nil {
		t.Fatal("switch error = nil, want persistence failure")
	}
	if output.Len() != 0 {
		t.Errorf("output = %q, want no success message", output.String())
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
