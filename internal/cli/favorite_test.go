package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"

	"vlt/internal/config"
	"vlt/internal/favorite"
	"vlt/internal/profile"
	"vlt/internal/statelock"
)

type fakeFavoriteStore struct {
	configuration favorite.Configuration
	err           error
	loadCalls     int
}

type fakeFavoriteUseRecorder struct {
	selected []favorite.Favorite
	err      error
	onRecord func()
}

func (r *fakeFavoriteUseRecorder) RecordUse(_ context.Context, selected favorite.Favorite) error {
	r.selected = append(r.selected, selected)
	if r.onRecord != nil {
		r.onRecord()
	}
	return r.err
}

type fakeFavoriteUseLock struct {
	held  bool
	calls int
}

func (l *fakeFavoriteUseLock) WithLock(ctx context.Context, operation func(context.Context) error) error {
	l.calls++
	l.held = true
	defer func() { l.held = false }()
	return operation(ctx)
}

func (s *fakeFavoriteStore) Load(context.Context) (favorite.Configuration, error) {
	s.loadCalls++
	if s.err != nil {
		return favorite.Configuration{}, s.err
	}
	result := s.configuration
	result.Favorites = append([]favorite.Favorite(nil), result.Favorites...)
	return result, nil
}

type recordingFavoriteMutator struct {
	added     []favorite.Favorite
	updated   []recordedFavoriteUpdate
	removed   []string
	addErr    error
	updateErr error
	removeErr error
}

type recordedFavoriteUpdate struct {
	selector string
	changes  favorite.FavoriteChanges
}

func TestFavoriteMutationSuccessOutputUsesSharedStatusPresentation(t *testing.T) {
	tests := []struct {
		name string
		want string
		run  func(*bytes.Buffer) error
	}{
		{
			name: "add",
			want: "Added favorite \"secret/a\" for profile \"team-a\".\n",
			run: func(output *bytes.Buffer) error {
				handler := NewFavoriteHandler(FavoriteDependencies{
					Mutations: &recordingFavoriteMutator{}, Output: output, Terminal: switchTerminal{color: true},
				})
				return handler(context.Background(), []string{
					"add", "secret/a", "--profile", "team-a", "--operation", "read",
				})
			},
		},
		{
			name: "update",
			want: "Updated favorite 2.\n",
			run: func(output *bytes.Buffer) error {
				handler := NewFavoriteHandler(FavoriteDependencies{
					Mutations: &recordingFavoriteMutator{}, Output: output, Terminal: switchTerminal{color: true},
				})
				return handler(context.Background(), []string{"update", "2", "--note", "daily"})
			},
		},
		{
			name: "remove",
			want: "Removed favorite 3.\n",
			run: func(output *bytes.Buffer) error {
				handler := NewFavoriteHandler(FavoriteDependencies{
					Mutations: &recordingFavoriteMutator{}, Output: output, Terminal: switchTerminal{color: true},
				})
				return handler(context.Background(), []string{"remove", "3"})
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

func TestFavoriteCancellationUsesOneDiagnosticAndNoWork(t *testing.T) {
	selected := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}
	tests := []struct {
		name  string
		cause error
		run   func(*bytes.Buffer, error) (int, error)
	}{
		{
			name: "execute selector", cause: ErrFavoriteSelectionCanceled,
			run: func(output *bytes.Buffer, cause error) (int, error) {
				vaultCalls := 0
				handler := NewFavoriteHandler(FavoriteDependencies{
					Favorites: &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}},
					Output:    output, Terminal: switchTerminal{prompts: true},
					Selector: &fakeFavoriteExecutionSelector{err: cause},
					Vault:    func(context.Context, []string) error { vaultCalls++; return nil },
				})
				return vaultCalls, handler(context.Background(), nil)
			},
		},
		{
			name: "add form", cause: huh.ErrUserAborted,
			run: func(output *bytes.Buffer, cause error) (int, error) {
				mutations := &recordingFavoriteMutator{}
				handler := NewFavoriteHandler(FavoriteDependencies{
					Profiles:  &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{{Name: "team-a"}}}},
					Mutations: mutations, Output: output, Terminal: switchTerminal{prompts: true},
					Form: &fakeFavoriteForm{err: cause},
				})
				err := handler(context.Background(), []string{"add"})
				return len(mutations.added), err
			},
		},
		{
			name: "update selector", cause: ErrFavoriteSelectionCanceled,
			run: func(output *bytes.Buffer, cause error) (int, error) {
				mutations := &recordingFavoriteMutator{}
				handler := NewFavoriteHandler(FavoriteDependencies{
					Favorites: &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}},
					Mutations: mutations, Output: output, Terminal: switchTerminal{prompts: true},
					ManagementSelector: &fakeFavoriteManagementSelector{err: cause},
				})
				err := handler(context.Background(), []string{"update"})
				return len(mutations.updated), err
			},
		},
		{
			name: "remove confirmation", cause: huh.ErrUserAborted,
			run: func(output *bytes.Buffer, cause error) (int, error) {
				mutations := &recordingFavoriteMutator{}
				handler := NewFavoriteHandler(FavoriteDependencies{
					Favorites: &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}},
					Mutations: mutations, Output: output, Terminal: switchTerminal{prompts: true},
					ManagementSelector: &fakeFavoriteManagementSelector{selected: selected},
					RemovalConfirmer:   &fakeFavoriteRemovalConfirmer{err: cause},
				})
				err := handler(context.Background(), []string{"remove"})
				return len(mutations.removed), err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			calls, err := tt.run(&output, tt.cause)
			if err == nil || err.Error() != "operation canceled" {
				t.Fatalf("cancellation error = %v, want operation canceled", err)
			}
			if !errors.Is(err, tt.cause) {
				t.Fatalf("cancellation error = %v, want cause %v", err, tt.cause)
			}
			if calls != 0 || output.Len() != 0 {
				t.Fatalf("cancellation work calls = %d, output = %q; want none", calls, output.String())
			}
		})
	}
}

func TestFavoriteExecutionOutputContainsOnlyDelegatedVaultBytes(t *testing.T) {
	selected := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}
	var output bytes.Buffer
	handler := NewFavoriteHandler(FavoriteDependencies{
		Favorites: &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}},
		Output:    &output, Terminal: switchTerminal{prompts: true},
		Selector: &fakeFavoriteExecutionSelector{selected: selected},
		Vault: func(context.Context, []string) error {
			_, err := output.WriteString("delegated Vault output\n")
			return err
		},
	})

	if err := handler(context.Background(), nil); err != nil {
		t.Fatalf("favorite execution error = %v", err)
	}
	if got, want := output.String(), "delegated Vault output\n"; got != want {
		t.Fatalf("execution output = %q, want %q", got, want)
	}
}

type fakeFavoriteExecutionSelector struct {
	candidates []favorite.Favorite
	selected   favorite.Favorite
	err        error
	calls      int
}

func (s *fakeFavoriteExecutionSelector) Select(_ context.Context, candidates []favorite.Favorite) (favorite.Favorite, error) {
	s.calls++
	s.candidates = append([]favorite.Favorite(nil), candidates...)
	return s.selected, s.err
}

func (m *recordingFavoriteMutator) Add(_ context.Context, candidate favorite.Favorite) error {
	m.added = append(m.added, candidate)
	return m.addErr
}

func (m *recordingFavoriteMutator) Update(_ context.Context, selector string, changes favorite.FavoriteChanges) error {
	m.updated = append(m.updated, recordedFavoriteUpdate{selector: selector, changes: changes})
	return m.updateErr
}

func (m *recordingFavoriteMutator) Remove(_ context.Context, selector string) error {
	m.removed = append(m.removed, selector)
	return m.removeErr
}

func TestFavoriteHandlerReturnsCanonicalHelp(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      string
	}{
		{name: "favorite help", arguments: []string{"--help"}, want: favoriteHelpText},
		{name: "add help", arguments: []string{"add", "--help"}, want: favoriteAddHelpText},
		{name: "list help", arguments: []string{"list", "-h"}, want: favoriteListHelpText},
		{name: "update help", arguments: []string{"update", "--help"}, want: favoriteUpdateHelpText},
		{name: "remove help", arguments: []string{"remove", "--help"}, want: favoriteRemoveHelpText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewFavoriteHandler(FavoriteDependencies{Output: &output})
			if err := handler(context.Background(), tt.arguments); err != nil {
				t.Fatalf("handler error = %v", err)
			}
			if got := output.String(); got != tt.want {
				t.Fatalf("help output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFavoriteHandlerReturnsAutomaticHelpForMissingCommand(t *testing.T) {
	handler := NewFavoriteHandler(FavoriteDependencies{})
	err := handler(context.Background(), nil)

	var automaticHelp AutomaticHelp
	if !errors.As(err, &automaticHelp) {
		t.Fatalf("handler error = %v, want AutomaticHelp", err)
	}
	if automaticHelp.Text != favoriteHelpText {
		t.Fatalf("automatic help = %q, want %q", automaticHelp.Text, favoriteHelpText)
	}
}

func TestFavoriteAddPassesExactValuesAndPrintsAfterSuccess(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      favorite.Favorite
	}{
		{
			name:      "read without note",
			arguments: []string{"add", "secret/data/app", "--profile", "team-a", "--operation", "read"},
			want: favorite.Favorite{
				Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/data/app",
			},
		},
		{
			name: "kv get with explicit note",
			arguments: []string{
				"add", "Secret/Data/App", "--profile=Team-A", "--operation=kv-get", "--note=daily credentials",
			},
			want: favorite.Favorite{
				Profile: "Team-A", Operation: favorite.OperationKVGet, Path: "Secret/Data/App", Note: "daily credentials",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			mutations := &recordingFavoriteMutator{}
			handler := NewFavoriteHandler(FavoriteDependencies{Mutations: mutations, Output: &output})

			if err := handler(context.Background(), tt.arguments); err != nil {
				t.Fatalf("favorite add error = %v", err)
			}
			if !reflect.DeepEqual(mutations.added, []favorite.Favorite{tt.want}) {
				t.Fatalf("Add() values = %#v, want %#v", mutations.added, []favorite.Favorite{tt.want})
			}
			for _, text := range []string{"Added favorite", tt.want.Path, tt.want.Profile} {
				if !strings.Contains(output.String(), text) {
					t.Errorf("success output = %q, want %q", output.String(), text)
				}
			}
		})
	}
}

func TestFavoriteAddPreservesValuesButSanitizesSuccessOutput(t *testing.T) {
	mutations := &recordingFavoriteMutator{}
	var output bytes.Buffer
	handler := NewFavoriteHandler(FavoriteDependencies{Mutations: mutations, Output: &output})
	path := "secret/a\nspoofed row\x1b[31m"

	if err := handler(context.Background(), []string{
		"add", path, "--profile", "team-a", "--operation", "read",
	}); err != nil {
		t.Fatalf("favorite add error = %v", err)
	}
	if len(mutations.added) != 1 || mutations.added[0].Path != path {
		t.Fatalf("Add() path = %#v, want exact raw path %q", mutations.added, path)
	}
	if strings.Contains(output.String(), "\x1b") || strings.Count(output.String(), "\n") != 1 {
		t.Fatalf("success output contains terminal control structure: %q", output.String())
	}
	if !strings.Contains(output.String(), `secret/a\nspoofed row\x1b[31m`) {
		t.Fatalf("success output = %q, want safely escaped path", output.String())
	}
}

func TestFavoriteAddMissingRequiredInputReturnsAutomaticHelp(t *testing.T) {
	tests := [][]string{
		{"add"},
		{"add", "secret/a", "--operation", "read"},
		{"add", "secret/a", "--profile", "team-a"},
		{"add", "--profile", "team-a", "--operation", "read"},
	}

	for _, arguments := range tests {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			mutations := &recordingFavoriteMutator{}
			handler := NewFavoriteHandler(FavoriteDependencies{Mutations: mutations, Output: &bytes.Buffer{}})

			err := handler(context.Background(), arguments)
			var automaticHelp AutomaticHelp
			if !errors.As(err, &automaticHelp) || automaticHelp.Text != favoriteAddHelpText {
				t.Fatalf("favorite add error = %v, want canonical AutomaticHelp", err)
			}
			if len(mutations.added) != 0 {
				t.Fatalf("Add() calls = %#v, want none", mutations.added)
			}
		})
	}
}

func TestFavoriteAddRejectsInvalidSyntaxWithContext(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
	}{
		{name: "unknown flag", arguments: []string{"add", "secret/a", "--profile", "team-a", "--operation", "read", "--unknown"}},
		{name: "extra argument", arguments: []string{"add", "secret/a", "extra", "--profile", "team-a", "--operation", "read"}},
		{name: "flag before path", arguments: []string{"add", "--profile", "team-a", "secret/a", "--operation", "read"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutations := &recordingFavoriteMutator{}
			handler := NewFavoriteHandler(FavoriteDependencies{Mutations: mutations, Output: &bytes.Buffer{}})
			err := handler(context.Background(), tt.arguments)
			if err == nil {
				t.Fatal("favorite add error = nil, want usage error")
			}
			for _, text := range []string{"invalid favorite add option", "Usage: " + favoriteAddUsage, "vlt favorite add --help"} {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("favorite add error = %q, want %q", err, text)
				}
			}
			if len(mutations.added) != 0 {
				t.Fatalf("Add() calls = %#v, want none", mutations.added)
			}
		})
	}
}

func TestFavoriteListDisplaysStableNumberedColumns(t *testing.T) {
	store := &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/z", Note: "", RunCount: 9},
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/a", Note: "reporting", RunCount: 7},
		{Profile: "team-a", Operation: favorite.OperationKVGet, Path: "secret/a", Note: "daily", RunCount: 7},
	}}}
	var output bytes.Buffer
	handler := NewFavoriteHandler(FavoriteDependencies{Favorites: store, Output: &output, Terminal: fixedTerminal{}})

	if err := handler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("favorite list error = %v", err)
	}
	want := "" +
		"#  RUNS  OPERATION  PROFILE  PATH      NOTE\n" +
		"1  9     read       team-b   secret/z  -\n" +
		"2  7     kv-get     team-a   secret/a  daily\n" +
		"3  7     read       team-b   secret/a  reporting\n"
	if got := output.String(); got != want {
		t.Fatalf("favorite list output = %q, want %q", got, want)
	}
}

func TestFavoriteListSanitizesControlCharactersWithinRows(t *testing.T) {
	store := &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a\nspoof", Note: "note\tvalue\x1b[31m",
	}}}}
	var output bytes.Buffer
	handler := NewFavoriteHandler(FavoriteDependencies{Favorites: store, Output: &output, Terminal: fixedTerminal{}})

	if err := handler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("favorite list error = %v", err)
	}
	if strings.Contains(output.String(), "\x1b") || strings.Count(output.String(), "\n") != 2 {
		t.Fatalf("favorite list contains terminal control structure: %q", output.String())
	}
	for _, text := range []string{"secret/a spoof", "note value [31m"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("favorite list output = %q, want sanitized text %q", output.String(), text)
		}
	}
}

func TestFavoriteListRejectsArgumentsWithoutLoading(t *testing.T) {
	store := &fakeFavoriteStore{}
	handler := NewFavoriteHandler(FavoriteDependencies{Favorites: store, Output: &bytes.Buffer{}})
	err := handler(context.Background(), []string{"list", "extra"})

	if err == nil || !strings.Contains(err.Error(), "Usage: "+favoriteListUsage) {
		t.Fatalf("favorite list error = %v, want usage error", err)
	}
	if store.loadCalls != 0 {
		t.Fatalf("Load() calls = %d, want 0", store.loadCalls)
	}
}

func TestFavoriteUpdatePassesOnlySuppliedValuesIncludingEmptyNote(t *testing.T) {
	mutations := &recordingFavoriteMutator{}
	var output bytes.Buffer
	handler := NewFavoriteHandler(FavoriteDependencies{Mutations: mutations, Output: &output})
	arguments := []string{
		"update", "2", "--profile=Team-A", "--operation", "kv-get", "--path", "Secret/A", "--note=",
	}

	if err := handler(context.Background(), arguments); err != nil {
		t.Fatalf("favorite update error = %v", err)
	}
	if len(mutations.updated) != 1 {
		t.Fatalf("Update() calls = %#v, want one", mutations.updated)
	}
	got := mutations.updated[0]
	if got.selector != "2" {
		t.Fatalf("Update() selector = %q, want 2", got.selector)
	}
	if got.changes.Profile == nil || *got.changes.Profile != "Team-A" {
		t.Fatalf("profile change = %#v, want Team-A", got.changes.Profile)
	}
	if got.changes.Operation == nil || *got.changes.Operation != favorite.OperationKVGet {
		t.Fatalf("operation change = %#v, want kv-get", got.changes.Operation)
	}
	if got.changes.Path == nil || *got.changes.Path != "Secret/A" {
		t.Fatalf("path change = %#v, want Secret/A", got.changes.Path)
	}
	if got.changes.Note == nil || *got.changes.Note != "" {
		t.Fatalf("note change = %#v, want explicit empty", got.changes.Note)
	}
	if !strings.Contains(output.String(), "Updated favorite 2") {
		t.Fatalf("success output = %q, want selector", output.String())
	}
}

func TestFavoriteUpdatePreservesOmittedValues(t *testing.T) {
	mutations := &recordingFavoriteMutator{}
	handler := NewFavoriteHandler(FavoriteDependencies{Mutations: mutations, Output: &bytes.Buffer{}})

	if err := handler(context.Background(), []string{"update", "1", "--path=secret/new"}); err != nil {
		t.Fatalf("favorite update error = %v", err)
	}
	changes := mutations.updated[0].changes
	if changes.Path == nil || *changes.Path != "secret/new" {
		t.Fatalf("path change = %#v, want secret/new", changes.Path)
	}
	if changes.Profile != nil || changes.Operation != nil || changes.Note != nil {
		t.Fatalf("omitted changes were set: %#v", changes)
	}
}

func TestFavoriteUpdateMissingSelectorOrChangesReturnsAutomaticHelp(t *testing.T) {
	tests := [][]string{
		{"update"},
		{"update", "1"},
		{"update", "--note=changed"},
	}
	for _, arguments := range tests {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			mutations := &recordingFavoriteMutator{}
			handler := NewFavoriteHandler(FavoriteDependencies{Mutations: mutations, Output: &bytes.Buffer{}})

			err := handler(context.Background(), arguments)
			var automaticHelp AutomaticHelp
			if !errors.As(err, &automaticHelp) || automaticHelp.Text != favoriteUpdateHelpText {
				t.Fatalf("favorite update error = %v, want canonical AutomaticHelp", err)
			}
			if len(mutations.updated) != 0 {
				t.Fatalf("Update() calls = %#v, want none", mutations.updated)
			}
		})
	}
}

func TestFavoriteRemovePassesSelectorAndPrintsAfterSuccess(t *testing.T) {
	mutations := &recordingFavoriteMutator{}
	var output bytes.Buffer
	handler := NewFavoriteHandler(FavoriteDependencies{Mutations: mutations, Output: &output})

	if err := handler(context.Background(), []string{"remove", "3"}); err != nil {
		t.Fatalf("favorite remove error = %v", err)
	}
	if !reflect.DeepEqual(mutations.removed, []string{"3"}) {
		t.Fatalf("Remove() selectors = %#v, want 3", mutations.removed)
	}
	if !strings.Contains(output.String(), "Removed favorite 3") {
		t.Fatalf("success output = %q, want selector", output.String())
	}
}

func TestFavoriteRemoveMissingOrExtraSelectorDoesNotMutate(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		automatic bool
	}{
		{name: "missing", arguments: []string{"remove"}, automatic: true},
		{name: "extra", arguments: []string{"remove", "1", "extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutations := &recordingFavoriteMutator{}
			handler := NewFavoriteHandler(FavoriteDependencies{Mutations: mutations, Output: &bytes.Buffer{}})
			err := handler(context.Background(), tt.arguments)
			if tt.automatic {
				var automaticHelp AutomaticHelp
				if !errors.As(err, &automaticHelp) || automaticHelp.Text != favoriteRemoveHelpText {
					t.Fatalf("favorite remove error = %v, want AutomaticHelp", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "Usage: "+favoriteRemoveUsage) {
				t.Fatalf("favorite remove error = %v, want usage error", err)
			}
			if len(mutations.removed) != 0 {
				t.Fatalf("Remove() calls = %#v, want none", mutations.removed)
			}
		})
	}
}

func TestFavoriteMutationFailureIsRedactedAndPrintsNoSuccess(t *testing.T) {
	const token = "hvs.synthetic-favorite-handler-token"
	mutations := &recordingFavoriteMutator{addErr: errors.New("persist failed with VAULT_TOKEN=" + token)}
	var output bytes.Buffer
	handler := NewFavoriteHandler(FavoriteDependencies{Mutations: mutations, Output: &output})

	err := handler(context.Background(), []string{"add", "secret/a", "--profile", "team-a", "--operation", "read"})
	if err == nil {
		t.Fatal("favorite add error = nil, want failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("favorite add error exposed token: %q", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("favorite add error = %q, want redaction marker", err)
	}
	if output.Len() != 0 {
		t.Fatalf("output after failed persistence = %q, want empty", output.String())
	}
}

func TestFavoriteHandlerRejectsUnknownCommandWithSuggestion(t *testing.T) {
	handler := NewFavoriteHandler(FavoriteDependencies{})
	err := handler(context.Background(), []string{"lst"})

	if err == nil {
		t.Fatal("handler error = nil, want unknown command error")
	}
	for _, text := range []string{`unknown favorite command "lst"`, `Did you mean "list"?`, "Usage: " + favoriteUsage} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("handler error = %q, want %q", err, text)
		}
	}
}

func TestFavoriteExecutionDelegatesSelectedOperationExactlyOnce(t *testing.T) {
	tests := []struct {
		name     string
		selected favorite.Favorite
		wantArgs []string
	}{
		{
			name: "read",
			selected: favorite.Favorite{
				Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/app",
			},
			wantArgs: []string{"--profile", "team-a", "read", "secret/app"},
		},
		{
			name: "kv get",
			selected: favorite.Favorite{
				Profile: "team-b", Operation: favorite.OperationKVGet, Path: "secret/data/app",
			},
			wantArgs: []string{"--profile", "team-b", "kv", "get", "secret/data/app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := favorite.Favorite{Profile: "team-z", Operation: favorite.OperationRead, Path: "secret/z"}
			store := &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{other, tt.selected}}}
			selector := &fakeFavoriteExecutionSelector{selected: tt.selected}
			var vaultCalls [][]string
			vault := func(_ context.Context, arguments []string) error {
				vaultCalls = append(vaultCalls, append([]string(nil), arguments...))
				return nil
			}
			profiles := &fakeProfileStore{configuration: config.Configuration{ActiveProfile: "team-z"}}
			handler := NewFavoriteHandler(FavoriteDependencies{
				Profiles: profiles, Favorites: store, Terminal: switchTerminal{prompts: true},
				Selector: selector, Vault: vault,
			})

			if err := handler(context.Background(), nil); err != nil {
				t.Fatalf("favorite execution error = %v", err)
			}
			if !reflect.DeepEqual(selector.candidates, favorite.NewService([]favorite.Favorite{other, tt.selected}).List()) {
				t.Fatalf("selector candidates = %#v, want sorted favorites", selector.candidates)
			}
			if !reflect.DeepEqual(vaultCalls, [][]string{tt.wantArgs}) {
				t.Fatalf("Vault calls = %#v, want %#v", vaultCalls, [][]string{tt.wantArgs})
			}
			if profiles.loads != 0 || len(profiles.selectors) != 0 {
				t.Fatalf("favorite execution changed or loaded active profile: %#v", profiles)
			}
		})
	}
}

func TestFavoriteExecutionRecordsUseAfterSuccessfulVaultRun(t *testing.T) {
	selected := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}
	lock := &fakeFavoriteUseLock{}
	recorder := &fakeFavoriteUseRecorder{onRecord: func() {
		if !lock.held {
			t.Fatal("RecordUse() ran without the mutation lock")
		}
	}}
	vaultCalls := 0
	handler := NewFavoriteHandler(FavoriteDependencies{
		Favorites: &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}},
		Terminal:  switchTerminal{prompts: true}, Selector: &fakeFavoriteExecutionSelector{selected: selected},
		Lock: lock, Recorder: recorder,
		Vault: func(context.Context, []string) error {
			vaultCalls++
			if lock.held {
				t.Fatal("Vault ran while holding the mutation lock")
			}
			return nil
		},
	})
	if err := handler(context.Background(), nil); err != nil {
		t.Fatalf("favorite execution error = %v", err)
	}
	if vaultCalls != 1 || lock.calls != 1 || !reflect.DeepEqual(recorder.selected, []favorite.Favorite{selected}) {
		t.Fatalf("calls: Vault=%d lock=%d recorded=%#v", vaultCalls, lock.calls, recorder.selected)
	}
}

func TestFavoriteExecutionSkipsUseAfterVaultFailure(t *testing.T) {
	selected := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}
	wantErr := errors.New("delegated failure")
	lock := &fakeFavoriteUseLock{}
	recorder := &fakeFavoriteUseRecorder{}
	handler := NewFavoriteHandler(FavoriteDependencies{
		Favorites: &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}},
		Terminal:  switchTerminal{prompts: true}, Selector: &fakeFavoriteExecutionSelector{selected: selected},
		Lock: lock, Recorder: recorder, Vault: func(context.Context, []string) error { return wantErr },
	})
	if err := handler(context.Background(), nil); err != wantErr {
		t.Fatalf("favorite execution error = %v, want delegated error", err)
	}
	if lock.calls != 0 || len(recorder.selected) != 0 {
		t.Fatalf("failed Vault run recorded use: lock=%d recorded=%#v", lock.calls, recorder.selected)
	}
}

func TestFavoriteExecutionIgnoresCountSaveFailureAndPreservesStreams(t *testing.T) {
	selected := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}
	var stdout, stderr bytes.Buffer
	recorder := &fakeFavoriteUseRecorder{err: errors.New("synthetic save failure")}
	handler := NewFavoriteHandler(FavoriteDependencies{
		Favorites: &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}},
		Terminal:  switchTerminal{prompts: true}, Selector: &fakeFavoriteExecutionSelector{selected: selected},
		Lock: &fakeFavoriteUseLock{}, Recorder: recorder, Output: &stdout,
		Vault: func(context.Context, []string) error {
			_, _ = stdout.WriteString("Vault stdout\n")
			_, _ = stderr.WriteString("Vault stderr\n")
			return nil
		},
	})
	if err := handler(context.Background(), nil); err != nil {
		t.Fatalf("favorite execution error = %v, want Vault success", err)
	}
	if stdout.String() != "Vault stdout\n" || stderr.String() != "Vault stderr\n" || len(recorder.selected) != 1 {
		t.Fatalf("streams or use changed: stdout=%q stderr=%q recorded=%#v", stdout.String(), stderr.String(), recorder.selected)
	}
}

func TestConcurrentFavoriteExecutionsRecordEverySuccessfulRun(t *testing.T) {
	selected := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}
	directory := filepath.Join(t.TempDir(), "vlt")
	store := favorite.NewStore(filepath.Join(directory, "favorites.json"))
	if err := store.Save(context.Background(), favorite.Configuration{Favorites: []favorite.Favorite{selected}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	lock := statelock.New(directory)
	recorder := favorite.NewMutationService(store, nil)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	results := make(chan error, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := lock.WithLock(ctx, func(ctx context.Context) error {
		defer close(release)
		for range 2 {
			handler := NewFavoriteHandler(FavoriteDependencies{
				Favorites: store, Terminal: switchTerminal{prompts: true},
				Selector: &fakeFavoriteExecutionSelector{selected: selected},
				Lock:     lock, Recorder: recorder,
				Vault: func(ctx context.Context, _ []string) error {
					entered <- struct{}{}
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				},
			})
			go func() { results <- handler(ctx, nil) }()
		}
		for range 2 {
			select {
			case <-entered:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Vault runs did not finish while the mutation lock was held: %v", err)
	}
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("favorite execution error = %v", err)
			}
		case <-ctx.Done():
			t.Fatalf("favorite execution timed out: %v", ctx.Err())
		}
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Favorites) != 1 || got.Favorites[0].RunCount != 2 {
		t.Fatalf("favorites after concurrent runs = %#v, want two runs", got.Favorites)
	}
}

func TestFavoriteExecutionCancellationDoesNotDelegate(t *testing.T) {
	store := &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
	}}}}
	selector := &fakeFavoriteExecutionSelector{err: ErrFavoriteSelectionCanceled}
	vaultCalls := 0
	lock := &fakeFavoriteUseLock{}
	recorder := &fakeFavoriteUseRecorder{}
	handler := NewFavoriteHandler(FavoriteDependencies{
		Favorites: store, Terminal: switchTerminal{prompts: true}, Selector: selector,
		Lock: lock, Recorder: recorder,
		Vault: func(context.Context, []string) error { vaultCalls++; return nil },
	})

	err := handler(context.Background(), nil)
	if !errors.Is(err, ErrFavoriteSelectionCanceled) {
		t.Fatalf("favorite execution error = %v, want ErrFavoriteSelectionCanceled", err)
	}
	if vaultCalls != 0 {
		t.Fatalf("Vault calls = %d, want 0", vaultCalls)
	}
	if lock.calls != 0 || len(recorder.selected) != 0 {
		t.Fatalf("canceled selection recorded use: lock=%d recorded=%#v", lock.calls, recorder.selected)
	}
}

func TestFavoriteExecutionEmptyStateGivesAddGuidanceBeforeSelection(t *testing.T) {
	selector := &fakeFavoriteExecutionSelector{}
	vaultCalls := 0
	handler := NewFavoriteHandler(FavoriteDependencies{
		Favorites: &fakeFavoriteStore{}, Terminal: switchTerminal{prompts: true}, Selector: selector,
		Vault: func(context.Context, []string) error { vaultCalls++; return nil },
	})

	err := handler(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "vlt favorite add") {
		t.Fatalf("favorite execution error = %v, want add guidance", err)
	}
	if selector.calls != 0 || vaultCalls != 0 {
		t.Fatalf("empty execution used selector or Vault: selector=%d Vault=%d", selector.calls, vaultCalls)
	}
}

func TestFavoriteExecutionOutsideTerminalReturnsAutomaticHelp(t *testing.T) {
	store := &fakeFavoriteStore{}
	selector := &fakeFavoriteExecutionSelector{}
	vaultCalls := 0
	handler := NewFavoriteHandler(FavoriteDependencies{
		Favorites: store, Terminal: switchTerminal{}, Selector: selector,
		Vault: func(context.Context, []string) error { vaultCalls++; return nil },
	})

	err := handler(context.Background(), nil)
	requireAutomaticHelp(t, err, favoriteHelpText)
	if store.loadCalls != 0 || selector.calls != 0 || vaultCalls != 0 {
		t.Fatalf("non-TTY execution used dependencies: loads=%d selector=%d Vault=%d", store.loadCalls, selector.calls, vaultCalls)
	}
}

func TestFavoriteExecutionPreservesDelegatedError(t *testing.T) {
	selected := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}
	wantErr := errors.New("delegated failure")
	handler := NewFavoriteHandler(FavoriteDependencies{
		Favorites: &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}},
		Terminal:  switchTerminal{prompts: true},
		Selector:  &fakeFavoriteExecutionSelector{selected: selected},
		Vault:     func(context.Context, []string) error { return wantErr },
	})

	err := handler(context.Background(), nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("favorite execution error = %v, want exact delegated error", err)
	}
}
