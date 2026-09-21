package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vlt/internal/cli"
	"vlt/internal/config"
	"vlt/internal/favorite"
	"vlt/internal/profile"
)

type automaticHelpServices struct {
	loads      int
	selections int
	mutations  int
	vaultCalls int
}

func (s *automaticHelpServices) Load(context.Context) (config.Configuration, error) {
	s.loads++
	return config.Configuration{}, nil
}

func (s *automaticHelpServices) SetActiveProfile(context.Context, string) error {
	s.selections++
	return nil
}

func (s *automaticHelpServices) Add(context.Context, profile.Profile) error {
	s.mutations++
	return nil
}

func (s *automaticHelpServices) Update(context.Context, string, profile.ProfileChanges) error {
	s.mutations++
	return nil
}

func (s *automaticHelpServices) Remove(context.Context, string) error {
	s.mutations++
	return nil
}

func (s *automaticHelpServices) handler(output io.Writer) *cli.Dispatcher {
	return cli.NewDispatcher(cli.Dependencies{
		Output: output,
		Profile: cli.NewProfileHandler(cli.ProfileDependencies{
			Profiles: s, Mutations: s, Output: output,
		}),
		Switch:     cli.NewSwitchHandler(cli.SwitchDependencies{Profiles: s, Output: output}),
		Favorite:   cli.NewFavoriteHandler(cli.FavoriteDependencies{Output: output}),
		Completion: cli.NewCompletionHandler(cli.CompletionDependencies{Profiles: s, Output: output}),
		Vault: func(context.Context, []string) error {
			s.vaultCalls++
			return nil
		},
	})
}

func (s *automaticHelpServices) calls() int {
	return s.loads + s.selections + s.mutations + s.vaultCalls
}

func TestNewDispatcherWiresProfileManagement(t *testing.T) {
	var stdout bytes.Buffer
	dispatcher := newDispatcherAt(t.TempDir(), strings.NewReader(""), &stdout, io.Discard)

	if err := dispatcher.Dispatch(context.Background(), []string{"profile", "list"}); err != nil {
		t.Fatalf("profile list error = %v", err)
	}
	if got, want := stdout.String(), "#  ACTIVE  NAME  ADDRESS  NAMESPACE  ALLOW HTTP  COLOR\n"; got != want {
		t.Errorf("profile list output = %q, want %q", got, want)
	}
	stdout.Reset()

	err := dispatcher.Dispatch(context.Background(), []string{"switch"})
	var automaticHelp cli.AutomaticHelp
	if !errors.As(err, &automaticHelp) {
		t.Fatalf("switch error = %v, want AutomaticHelp", err)
	}
	if automaticHelp.Text == "" || !strings.Contains(automaticHelp.Text, "Usage:\n  vlt switch") {
		t.Errorf("switch automatic help = %q, want switch help", automaticHelp.Text)
	}
	if stdout.Len() != 0 {
		t.Errorf("switch output = %q, want empty", stdout.String())
	}
}

func TestNewDispatcherWiresFavoriteManagement(t *testing.T) {
	configDirectory := t.TempDir()
	profiles := config.NewStore(filepath.Join(configDirectory, "vlt", "profiles.json"))
	if err := profiles.Save(context.Background(), config.Configuration{Profiles: []profile.Profile{{
		Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc",
	}}}); err != nil {
		t.Fatalf("save profiles: %v", err)
	}
	var stdout bytes.Buffer
	dispatcher := newDispatcherAt(configDirectory, strings.NewReader(""), &stdout, io.Discard)

	if err := dispatcher.Dispatch(context.Background(), []string{
		"favorite", "add", "secret/data/app", "--profile", "team-a", "--operation", "kv-get", "--note", "daily",
	}); err != nil {
		t.Fatalf("favorite add error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Added favorite") {
		t.Fatalf("favorite add output = %q, want success", stdout.String())
	}

	store := favorite.NewStore(filepath.Join(configDirectory, "vlt", "favorites.json"))
	configuration, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load favorites: %v", err)
	}
	want := []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationKVGet, Path: "secret/data/app", Note: "daily",
	}}
	if !reflect.DeepEqual(configuration.Favorites, want) {
		t.Fatalf("stored favorites = %#v, want %#v", configuration.Favorites, want)
	}

	stdout.Reset()
	if err := dispatcher.Dispatch(context.Background(), []string{"favorite", "list"}); err != nil {
		t.Fatalf("favorite list error = %v", err)
	}
	for _, text := range []string{"OPERATION", "PROFILE", "PATH", "NOTE", "kv-get", "team-a", "secret/data/app", "daily"} {
		if !strings.Contains(stdout.String(), text) {
			t.Errorf("favorite list output = %q, want %q", stdout.String(), text)
		}
	}
}

func TestNewDispatcherProfileRemoveProtectsLinkedFavorites(t *testing.T) {
	configDirectory := t.TempDir()
	profiles := config.NewStore(filepath.Join(configDirectory, "vlt", "profiles.json"))
	profileConfiguration := config.Configuration{Profiles: []profile.Profile{{
		Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc",
	}}}
	if err := profiles.Save(context.Background(), profileConfiguration); err != nil {
		t.Fatalf("save profiles: %v", err)
	}
	favorites := favorite.NewStore(filepath.Join(configDirectory, "vlt", "favorites.json"))
	favoriteConfiguration := favorite.Configuration{Favorites: []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/app",
	}}}
	if err := favorites.Save(context.Background(), favoriteConfiguration); err != nil {
		t.Fatalf("save favorites: %v", err)
	}
	dispatcher := newDispatcherAt(configDirectory, strings.NewReader(""), io.Discard, io.Discard)

	err := dispatcher.Dispatch(context.Background(), []string{"profile", "remove", "team-a"})
	if err == nil || !strings.Contains(err.Error(), "--remove-favorites") {
		t.Fatalf("profile remove error = %v, want cascade guidance", err)
	}
	gotProfiles, err := profiles.Load(context.Background())
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	if !reflect.DeepEqual(gotProfiles, profileConfiguration) {
		t.Fatalf("profiles after refused removal = %#v, want %#v", gotProfiles, profileConfiguration)
	}
	gotFavorites, err := favorites.Load(context.Background())
	if err != nil {
		t.Fatalf("load favorites: %v", err)
	}
	if !reflect.DeepEqual(gotFavorites, favoriteConfiguration) {
		t.Fatalf("favorites after refused removal = %#v, want %#v", gotFavorites, favoriteConfiguration)
	}
}

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

func TestDispatchWritesOneANSIFreeDiagnosticPrefix(t *testing.T) {
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output: &bytes.Buffer{},
		Vault: func(context.Context, []string) error {
			return errors.New("vlt: \x1b[31mrequest failed\x1b[0m")
		},
	})
	var stderr bytes.Buffer

	exitCode := dispatch(context.Background(), dispatcher, []string{"status"}, &stderr)

	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}
	if got, want := stderr.String(), "vlt: request failed\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestNewDispatcherRedirectedOutputIsPlainAndDeterministic(t *testing.T) {
	configDirectory := t.TempDir()
	var first bytes.Buffer
	firstDispatcher := newDispatcherAt(configDirectory, strings.NewReader(""), &first, io.Discard)
	if err := firstDispatcher.Dispatch(context.Background(), []string{"profile", "list"}); err != nil {
		t.Fatalf("first profile list error = %v", err)
	}

	var second bytes.Buffer
	secondDispatcher := newDispatcherAt(configDirectory, strings.NewReader(""), &second, io.Discard)
	if err := secondDispatcher.Dispatch(context.Background(), []string{"profile", "list"}); err != nil {
		t.Fatalf("second profile list error = %v", err)
	}

	if strings.Contains(first.String(), "\x1b[") {
		t.Fatalf("redirected output contains ANSI: %q", first.String())
	}
	if got, want := second.String(), first.String(); got != want {
		t.Fatalf("second output = %q, want deterministic %q", got, want)
	}
}

func TestRunRootHelpDoesNotRequireConfigDirectory(t *testing.T) {
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "AppData"} {
		t.Setenv(name, "")
	}
	var expected bytes.Buffer
	explicitDispatcher := cli.NewDispatcher(cli.Dependencies{Output: &expected})
	if err := explicitDispatcher.Dispatch(context.Background(), []string{"--help"}); err != nil {
		t.Fatalf("prepare root help: %v", err)
	}

	for _, tt := range []struct {
		name       string
		arguments  []string
		wantStatus int
		automatic  bool
	}{
		{name: "automatic", wantStatus: 1, automatic: true},
		{name: "explicit", arguments: []string{"--help"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			status := run(context.Background(), tt.arguments, strings.NewReader(""), &stdout, &stderr)

			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
			if tt.automatic {
				if got := stderr.String(); got != expected.String() {
					t.Errorf("stderr = %q, want %q", got, expected.String())
				}
				if stdout.Len() != 0 {
					t.Errorf("stdout = %q, want empty", stdout.String())
				}
			} else {
				if got := stdout.String(); got != expected.String() {
					t.Errorf("stdout = %q, want %q", got, expected.String())
				}
				if stderr.Len() != 0 {
					t.Errorf("stderr = %q, want empty", stderr.String())
				}
			}
		})
	}
}

func TestDispatchWritesAutomaticManagementHelpToStderr(t *testing.T) {
	tests := []struct {
		name      string
		automatic []string
		explicit  []string
	}{
		{name: "root", explicit: []string{"--help"}},
		{name: "profile", automatic: []string{"profile"}, explicit: []string{"profile", "--help"}},
		{name: "add all", automatic: []string{"profile", "add"}, explicit: []string{"profile", "add", "--help"}},
		{name: "add name", automatic: []string{"profile", "add", "--address", "https://vault.example.com", "--username", "alice"}, explicit: []string{"profile", "add", "--help"}},
		{name: "add address", automatic: []string{"profile", "add", "team-a", "--username", "alice"}, explicit: []string{"profile", "add", "--help"}},
		{name: "add username", automatic: []string{"profile", "add", "team-a", "--address", "https://vault.example.com"}, explicit: []string{"profile", "add", "--help"}},
		{name: "show", automatic: []string{"profile", "show"}, explicit: []string{"profile", "show", "--help"}},
		{name: "update selection", automatic: []string{"profile", "update"}, explicit: []string{"profile", "update", "--help"}},
		{name: "update changes", automatic: []string{"profile", "update", "team-a"}, explicit: []string{"profile", "update", "--help"}},
		{name: "update name", automatic: []string{"profile", "update", "--address", "https://vault.example.com"}, explicit: []string{"profile", "update", "--help"}},
		{name: "remove", automatic: []string{"profile", "remove"}, explicit: []string{"profile", "remove", "--help"}},
		{name: "switch", automatic: []string{"switch"}, explicit: []string{"switch", "--help"}},
		{name: "favorite", automatic: []string{"favorite"}, explicit: []string{"favorite", "--help"}},
		{name: "favorite add", automatic: []string{"favorite", "add"}, explicit: []string{"favorite", "add", "--help"}},
		{name: "favorite update", automatic: []string{"favorite", "update"}, explicit: []string{"favorite", "update", "--help"}},
		{name: "favorite remove", automatic: []string{"favorite", "remove"}, explicit: []string{"favorite", "remove", "--help"}},
		{name: "completion", automatic: []string{"completion"}, explicit: []string{"completion", "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			services := &automaticHelpServices{}
			var automaticStdout bytes.Buffer
			var automaticStderr bytes.Buffer

			automaticStatus := dispatch(
				context.Background(), services.handler(&automaticStdout), tt.automatic, &automaticStderr,
			)

			var explicitStdout bytes.Buffer
			var explicitStderr bytes.Buffer
			explicitStatus := dispatch(
				context.Background(), services.handler(&explicitStdout), tt.explicit, &explicitStderr,
			)

			if automaticStatus == 0 {
				t.Error("automatic status = 0, want failure")
			}
			if explicitStatus != 0 {
				t.Errorf("explicit status = %d, want 0", explicitStatus)
			}
			if automaticStdout.Len() != 0 {
				t.Errorf("automatic stdout = %q, want empty", automaticStdout.String())
			}
			if explicitStderr.Len() != 0 {
				t.Errorf("explicit stderr = %q, want empty", explicitStderr.String())
			}
			if got, want := automaticStderr.String(), explicitStdout.String(); got != want {
				t.Errorf("automatic stderr = %q, want exact explicit help %q", got, want)
			}
			for _, forbidden := range []string{"vlt: ", " is required", "required outside", "Run '"} {
				if strings.Contains(automaticStderr.String(), forbidden) {
					t.Errorf("automatic stderr = %q, want no %q", automaticStderr.String(), forbidden)
				}
			}
			if services.calls() != 0 {
				t.Errorf("service calls = %d, want 0", services.calls())
			}
		})
	}
}

func TestDispatchWritesAutomaticRootHelpToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	called := false
	handler := func(context.Context, []string) error {
		called = true
		return nil
	}
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output:     &stdout,
		Profile:    handler,
		Switch:     handler,
		Completion: handler,
		Vault:      handler,
	})

	exitCode := dispatch(context.Background(), dispatcher, nil, &stderr)

	if exitCode == 0 {
		t.Fatal("exit code = 0, want failure")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if called {
		t.Fatal("dispatch called a handler for automatic help")
	}

	var explicitHelp bytes.Buffer
	explicitDispatcher := cli.NewDispatcher(cli.Dependencies{Output: &explicitHelp})
	if err := explicitDispatcher.Dispatch(context.Background(), []string{"--help"}); err != nil {
		t.Fatalf("explicit help error = %v", err)
	}
	if got, want := stderr.String(), explicitHelp.String(); got != want {
		t.Errorf("stderr = %q, want exact help %q", got, want)
	}
}

func TestDispatchWritesExplicitRootHelpToStdout(t *testing.T) {
	var want string
	for _, argument := range []string{"-h", "--help"} {
		t.Run(argument, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			dispatcher := cli.NewDispatcher(cli.Dependencies{Output: &stdout})

			exitCode := dispatch(context.Background(), dispatcher, []string{argument}, &stderr)

			if exitCode != 0 {
				t.Errorf("exit code = %d, want 0", exitCode)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
			if stdout.Len() == 0 {
				t.Fatal("stdout is empty, want root help")
			}
			if want == "" {
				want = stdout.String()
			} else if got := stdout.String(); got != want {
				t.Errorf("stdout = %q, want exact help %q", got, want)
			}
		})
	}
}

func TestDispatchWritesProfileHelpToStdout(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output:  &stdout,
		Profile: cli.NewProfileHandler(cli.ProfileDependencies{Output: &stdout}),
		Switch:  unavailable("profile switching"),
		Vault:   unavailable("Vault delegation"),
	})

	exitCode := dispatch(context.Background(), dispatcher, []string{"profile", "--help"}, &stderr)

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout.String(), "Manage Vault profiles") {
		t.Errorf("stdout = %q, want profile help", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestDispatchWritesProfileGuidanceToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output:  &stdout,
		Profile: cli.NewProfileHandler(cli.ProfileDependencies{Output: &stdout}),
		Switch:  unavailable("profile switching"),
		Vault:   unavailable("Vault delegation"),
	})

	exitCode := dispatch(context.Background(), dispatcher, []string{"profile", "udpate"}, &stderr)

	if exitCode == 0 {
		t.Fatal("exit code = 0, want failure")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{"Did you mean \"update\"?", "Usage: vlt profile"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want text %q", stderr.String(), want)
		}
	}
}

func TestNewDispatcherWiresCompletionScripts(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var stdout bytes.Buffer
			dispatcher := newDispatcherAt(t.TempDir(), strings.NewReader(""), &stdout, io.Discard)

			if err := dispatcher.Dispatch(context.Background(), []string{"completion", shell}); err != nil {
				t.Fatalf("completion %s error = %v", shell, err)
			}
			if stdout.Len() == 0 || !strings.Contains(stdout.String(), "vlt") {
				t.Errorf("completion %s output = %q, want script", shell, stdout.String())
			}
		})
	}
}

func TestDispatchWritesCompletionFailureToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output:     &stdout,
		Profile:    unavailable("profile management"),
		Switch:     unavailable("profile switching"),
		Completion: cli.NewCompletionHandler(cli.CompletionDependencies{Output: &stdout}),
		Vault:      unavailable("Vault delegation"),
	})

	exitCode := dispatch(context.Background(), dispatcher, []string{"completion", "powershell"}, &stderr)

	if exitCode == 0 {
		t.Fatal("exit code = 0, want failure")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{"unsupported shell", "Usage: vlt completion"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want text %q", stderr.String(), want)
		}
	}
}

func TestNewDispatcherWiresCompletionCandidates(t *testing.T) {
	configDirectory := t.TempDir()
	store := config.NewStore(filepath.Join(configDirectory, "vlt", "profiles.json"))
	if err := store.Save(context.Background(), config.Configuration{Profiles: []profile.Profile{
		{Name: "team-b", Address: "https://vault.example.net", Username: "bob", AuthPath: "oidc"},
		{Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc"},
	}}); err != nil {
		t.Fatalf("save profiles: %v", err)
	}
	var stdout bytes.Buffer
	dispatcher := newDispatcherAt(configDirectory, strings.NewReader(""), &stdout, io.Discard)

	if err := dispatcher.Dispatch(context.Background(), []string{"completion", "__profiles"}); err != nil {
		t.Fatalf("completion candidates error = %v", err)
	}
	if got, want := stdout.String(), "team-a\nteam-b\n"; got != want {
		t.Errorf("completion candidates = %q, want %q", got, want)
	}
}

type mainExitError int

func (e mainExitError) Error() string { return "delegated failure" }
func (e mainExitError) ExitCode() int { return int(e) }
