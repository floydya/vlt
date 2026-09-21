package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"vlt/internal/config"
	"vlt/internal/favorite"
	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

type favoriteComponentHarness struct {
	*componentHarness
	favoritePath string
	favorites    *favorite.Store
}

type favoriteComponentAdapters struct {
	terminal           Terminal
	executionSelector  FavoriteSelector
	managementSelector FavoriteManagementSelector
	form               FavoriteForm
	favoriteConfirmer  FavoriteRemovalConfirmer
	profileConfirmer   ProfileRemovalConfirmer
}

func newFavoriteComponentHarness(t *testing.T) *favoriteComponentHarness {
	t.Helper()
	base := newComponentHarness(t, time.Date(2026, time.September, 19, 8, 0, 0, 0, time.UTC))
	favoritePath := filepath.Join(filepath.Dir(base.configPath), "favorites.json")
	return &favoriteComponentHarness{
		componentHarness: base,
		favoritePath:     favoritePath,
		favorites:        favorite.NewStore(favoritePath),
	}
}

func (h *favoriteComponentHarness) wire(adapters favoriteComponentAdapters) {
	favoriteMutations := favorite.NewMutationService(h.favorites, h.profiles)
	cascade := favorite.NewCascadeService(h.favorites, h.mutations)
	h.dispatcher = NewDispatcher(Dependencies{
		Output: &h.stdout,
		Profile: NewProfileHandler(ProfileDependencies{
			Profiles: h.profiles, Mutations: h.mutations, Output: &h.stdout, Terminal: adapters.terminal,
			RemovalConfirmer: adapters.profileConfirmer, Cascade: cascade,
		}),
		Favorite: NewFavoriteHandler(FavoriteDependencies{
			Profiles: h.profiles, Favorites: h.favorites, Mutations: favoriteMutations,
			Output: &h.stdout, Terminal: adapters.terminal, Selector: adapters.executionSelector,
			Vault: h.vault, ManagementSelector: adapters.managementSelector,
			Form: adapters.form, RemovalConfirmer: adapters.favoriteConfirmer,
		}),
		Vault: h.vault,
	})
}

func (h *favoriteComponentHarness) seedProfiles(t *testing.T, configuration config.Configuration, tokens map[string]string) {
	t.Helper()
	if err := h.profiles.Save(context.Background(), configuration); err != nil {
		t.Fatalf("seed profiles: %v", err)
	}
	for name, token := range tokens {
		if err := h.credentials.Set(context.Background(), name, token); err != nil {
			t.Fatalf("seed credential %q: %v", name, err)
		}
	}
}

func TestFakeBackedFavoriteExplicitAndGuidedCRUD(t *testing.T) {
	harness := newFavoriteComponentHarness(t)
	harness.seedProfiles(t, config.Configuration{Profiles: []profile.Profile{
		{Name: "team-a", Address: "https://team-a.example", Username: "alice", AuthPath: "oidc"},
		{Name: "team-b", Address: "https://team-b.example", Username: "bob", AuthPath: "oidc"},
	}}, nil)
	managementSelector := &fakeFavoriteManagementSelector{}
	form := &fakeFavoriteForm{}
	confirmer := &fakeFavoriteRemovalConfirmer{}
	harness.wire(favoriteComponentAdapters{
		terminal: switchTerminal{prompts: true}, managementSelector: managementSelector,
		form: form, favoriteConfirmer: confirmer,
	})

	explicit := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/z", Note: "explicit"}
	if err := harness.dispatcher.Dispatch(context.Background(), []string{
		"favorite", "add", explicit.Path, "--profile", explicit.Profile, "--operation", explicit.Operation, "--note", explicit.Note,
	}); err != nil {
		t.Fatalf("explicit favorite add error = %v", err)
	}

	guided := favorite.Favorite{Profile: "team-b", Operation: favorite.OperationKVGet, Path: "secret/a", Note: "guided"}
	form.result = guided
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"favorite", "add"}); err != nil {
		t.Fatalf("guided favorite add error = %v", err)
	}
	harness.stdout.Reset()
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"favorite", "list"}); err != nil {
		t.Fatalf("favorite list error = %v", err)
	}
	for _, want := range []string{"secret/a", "secret/z", "guided", "explicit"} {
		if !strings.Contains(harness.stdout.String(), want) {
			t.Errorf("favorite list output = %q, want %q", harness.stdout.String(), want)
		}
	}
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"favorite", "update", "2", "--note=updated"}); err != nil {
		t.Fatalf("explicit favorite update error = %v", err)
	}
	afterExplicitUpdate, err := harness.favorites.Load(context.Background())
	if err != nil {
		t.Fatalf("load favorites after explicit update: %v", err)
	}
	ordered := favorite.NewService(afterExplicitUpdate.Favorites).List()
	if len(ordered) != 2 || ordered[1].Note != "updated" {
		t.Fatalf("favorites after explicit update = %#v, want updated second note", ordered)
	}

	managementSelector.selected = guided
	guided.Path = "secret/b"
	form.result = guided
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"favorite", "update"}); err != nil {
		t.Fatalf("guided favorite update error = %v", err)
	}
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"favorite", "remove", "2"}); err != nil {
		t.Fatalf("explicit favorite remove error = %v", err)
	}

	managementSelector.selected = guided
	confirmer.confirmed = true
	if err := harness.dispatcher.Dispatch(context.Background(), []string{"favorite", "remove"}); err != nil {
		t.Fatalf("guided favorite remove error = %v", err)
	}
	configuration, err := harness.favorites.Load(context.Background())
	if err != nil {
		t.Fatalf("load favorites: %v", err)
	}
	if len(configuration.Favorites) != 0 {
		t.Fatalf("favorites after CRUD flow = %#v, want empty", configuration.Favorites)
	}
	if form.calls != 2 || managementSelector.calls != 2 || confirmer.calls != 1 {
		t.Errorf("guided calls: form=%d selector=%d confirmer=%d, want 2/2/1", form.calls, managementSelector.calls, confirmer.calls)
	}
	if len(harness.vaultRunner.commands) != 0 {
		t.Fatalf("favorite management invoked Vault: %#v", harness.vaultRunner.commandArguments())
	}
}

func TestFakeBackedFavoriteExecutionMapsReadOperationsWithoutChangingActiveProfile(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		want      []string
	}{
		{name: "read", operation: favorite.OperationRead, want: []string{"read", "secret/data/app"}},
		{name: "kv get", operation: favorite.OperationKVGet, want: []string{"kv", "get", "secret/data/app"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const activeToken = "hvs.synthetic-active-token"
			const favoriteToken = "hvs.synthetic-favorite-token"
			const secretValue = "synthetic-stored-secret-value"
			harness := newFavoriteComponentHarness(t)
			harness.seedProfiles(t, config.Configuration{
				Profiles: []profile.Profile{
					{Name: "active", Address: "https://active.example", Username: "alice", AuthPath: "oidc"},
					{Name: "favorite", Address: "https://favorite.example", Username: "bob", AuthPath: "oidc"},
				},
				ActiveProfile: "active",
			}, map[string]string{"active": activeToken, "favorite": favoriteToken})
			selected := favorite.Favorite{
				Profile: "favorite", Operation: tt.operation, Path: "secret/data/app", Note: "daily",
			}
			if err := harness.favorites.Save(context.Background(), favorite.Configuration{Favorites: []favorite.Favorite{selected}}); err != nil {
				t.Fatalf("seed favorite: %v", err)
			}
			harness.vaultRunner.run = func(command vaultexec.Command) (vaultexec.Result, error) {
				if command.Arguments[0] == "token" {
					return vaultexec.Result{Stdout: []byte(`{"data":{"ttl":3600,"renewable":false}}`)}, nil
				}
				_, err := strings.NewReader(secretValue).WriteTo(command.Stdout)
				return vaultexec.Result{}, err
			}
			harness.wire(favoriteComponentAdapters{
				terminal:          switchTerminal{prompts: true},
				executionSelector: &fakeFavoriteExecutionSelector{selected: selected},
			})

			if err := harness.dispatcher.Dispatch(context.Background(), []string{"favorite"}); err != nil {
				t.Fatalf("execute favorite error = %v", err)
			}
			if got := harness.stdout.String(); got != secretValue {
				t.Fatalf("delegated output = %q, want unchanged Vault output", got)
			}
			commands := harness.vaultRunner.delegatedCommands()
			if len(commands) != 1 {
				t.Fatalf("delegated commands = %#v, want one", harness.vaultRunner.commandArguments())
			}
			assertComponentCommand(t, commands[0], tt.want, "https://favorite.example", favoriteToken, "")
			profiles, err := harness.profiles.Load(context.Background())
			if err != nil {
				t.Fatalf("load profiles: %v", err)
			}
			if profiles.ActiveProfile != "active" {
				t.Errorf("active profile = %q, want unchanged", profiles.ActiveProfile)
			}

			harness.stdout.Reset()
			if err := harness.dispatcher.Dispatch(context.Background(), []string{"favorite", "list"}); err != nil {
				t.Fatalf("favorite list error = %v", err)
			}
			contents, err := os.ReadFile(harness.favoritePath)
			if err != nil {
				t.Fatalf("read favorite config: %v", err)
			}
			for _, surface := range []string{string(contents), harness.stdout.String()} {
				for _, forbidden := range []string{activeToken, favoriteToken, secretValue} {
					if strings.Contains(surface, forbidden) {
						t.Errorf("favorite metadata surface exposed canary %q: %q", forbidden, surface)
					}
				}
			}
		})
	}
}

func TestFakeBackedFavoriteSelectionStopsBeforeVault(t *testing.T) {
	tests := []struct {
		name      string
		selector  FavoriteSelector
		wantError string
	}{
		{
			name:      "shared selector cancellation",
			selector:  NewSharedFavoriteSelector(&recordingSharedSelector{err: ErrSharedSelectorCanceled}),
			wantError: "operation canceled",
		},
		{
			name:      "unknown stable identity",
			selector:  NewSharedFavoriteSelector(&recordingSharedSelector{selectedID: "favorite-999999"}),
			wantError: "unknown selection",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newFavoriteComponentHarness(t)
			originalProfiles := config.Configuration{
				Profiles:      []profile.Profile{{Name: "team-a", Address: "https://team-a.example", Username: "alice", AuthPath: "oidc"}},
				ActiveProfile: "team-a",
			}
			harness.seedProfiles(t, originalProfiles, map[string]string{"team-a": "hvs.synthetic-selector-token"})
			originalFavorites := favorite.Configuration{Favorites: []favorite.Favorite{{
				Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/app", Note: "daily",
			}}}
			if err := harness.favorites.Save(context.Background(), originalFavorites); err != nil {
				t.Fatalf("seed favorite: %v", err)
			}
			var loadErr error
			originalFavorites, loadErr = harness.favorites.Load(context.Background())
			if loadErr != nil {
				t.Fatalf("load seeded favorite: %v", loadErr)
			}
			harness.wire(favoriteComponentAdapters{
				terminal: switchTerminal{prompts: true}, executionSelector: tt.selector,
			})

			err := harness.dispatcher.Dispatch(context.Background(), []string{"favorite"})
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("execute favorite error = %v, want %q", err, tt.wantError)
			}
			gotProfiles, loadErr := harness.profiles.Load(context.Background())
			if loadErr != nil {
				t.Fatalf("load profiles: %v", loadErr)
			}
			gotFavorites, loadErr := harness.favorites.Load(context.Background())
			if loadErr != nil {
				t.Fatalf("load favorites: %v", loadErr)
			}
			if !reflect.DeepEqual(gotProfiles, originalProfiles) || !reflect.DeepEqual(gotFavorites, originalFavorites) {
				t.Errorf("selection failure changed state: profiles=%#v favorites=%#v", gotProfiles, gotFavorites)
			}
			if len(harness.vaultRunner.commands) != 0 {
				t.Fatalf("selection failure invoked Vault: %#v", harness.vaultRunner.commandArguments())
			}
		})
	}
}

func TestFakeBackedFavoriteCascadeApprovalAndRollback(t *testing.T) {
	tests := []struct {
		name        string
		deleteError error
		wantError   bool
	}{
		{name: "approved"},
		{name: "credential rollback", deleteError: errors.New("delete failed with hvs.synthetic-cascade-token"), wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const teamAToken = "hvs.synthetic-cascade-token"
			const teamBToken = "hvs.synthetic-unrelated-token"
			harness := newFavoriteComponentHarness(t)
			originalProfiles := config.Configuration{
				Profiles: []profile.Profile{
					{Name: "team-a", Address: "https://team-a.example", Username: "alice", AuthPath: "oidc"},
					{Name: "team-b", Address: "https://team-b.example", Username: "bob", AuthPath: "oidc"},
				},
				ActiveProfile: "team-a",
			}
			harness.seedProfiles(t, originalProfiles, map[string]string{"team-a": teamAToken, "team-b": teamBToken})
			originalFavorites := favorite.Configuration{Favorites: []favorite.Favorite{
				{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"},
				{Profile: "team-b", Operation: favorite.OperationKVGet, Path: "secret/b"},
			}}
			if err := harness.favorites.Save(context.Background(), originalFavorites); err != nil {
				t.Fatalf("seed favorites: %v", err)
			}
			var loadErr error
			originalFavorites, loadErr = harness.favorites.Load(context.Background())
			if loadErr != nil {
				t.Fatalf("load seeded favorites: %v", loadErr)
			}
			harness.credentials.deleteErr = tt.deleteError
			profileConfirmer := &fakeProfileRemovalConfirmer{result: true}
			harness.wire(favoriteComponentAdapters{
				terminal: switchTerminal{prompts: true}, profileConfirmer: profileConfirmer,
			})

			err := harness.dispatcher.Dispatch(context.Background(), []string{"profile", "remove", "team-a"})
			if (err != nil) != tt.wantError {
				t.Fatalf("profile remove error = %v, wantError=%t", err, tt.wantError)
			}
			if profileConfirmer.request.LinkedFavorites != 1 {
				t.Errorf("linked favorite count = %d, want 1", profileConfirmer.request.LinkedFavorites)
			}
			gotProfiles, loadErr := harness.profiles.Load(context.Background())
			if loadErr != nil {
				t.Fatalf("load profiles: %v", loadErr)
			}
			gotFavorites, loadErr := harness.favorites.Load(context.Background())
			if loadErr != nil {
				t.Fatalf("load favorites: %v", loadErr)
			}
			if tt.wantError {
				if !reflect.DeepEqual(gotProfiles, originalProfiles) || !reflect.DeepEqual(gotFavorites, originalFavorites) {
					t.Fatalf("failed cascade did not roll back: profiles=%#v favorites=%#v", gotProfiles, gotFavorites)
				}
				if err != nil && strings.Contains(err.Error(), teamAToken) {
					t.Errorf("cascade error exposed credential: %q", err)
				}
				if got := harness.credentials.values["team-a"]; got != teamAToken {
					t.Errorf("rolled back credential = %q, want original", got)
				}
				return
			}

			wantProfiles := config.Configuration{Profiles: originalProfiles.Profiles[1:]}
			wantFavorites := favorite.Configuration{Favorites: originalFavorites.Favorites[1:]}
			if !reflect.DeepEqual(gotProfiles, wantProfiles) || !reflect.DeepEqual(gotFavorites, wantFavorites) {
				t.Errorf("approved cascade state: profiles=%#v favorites=%#v", gotProfiles, gotFavorites)
			}
			if _, found := harness.credentials.values["team-a"]; found {
				t.Error("approved cascade retained removed credential")
			}
			if got := harness.credentials.values["team-b"]; got != teamBToken {
				t.Errorf("unrelated credential = %q, want unchanged", got)
			}
		})
	}
}
