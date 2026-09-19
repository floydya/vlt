package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vlt/internal/favorite"
)

type fakeFavoriteStore struct {
	configuration favorite.Configuration
	err           error
	loadCalls     int
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
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/z", Note: ""},
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/a", Note: "reporting"},
		{Profile: "team-a", Operation: favorite.OperationKVGet, Path: "secret/a", Note: "daily"},
	}}}
	var output bytes.Buffer
	handler := NewFavoriteHandler(FavoriteDependencies{Favorites: store, Output: &output, Terminal: fixedTerminal{}})

	if err := handler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("favorite list error = %v", err)
	}
	want := "" +
		"#  OPERATION  PROFILE  PATH      NOTE\n" +
		"1  kv-get     team-a   secret/a  daily\n" +
		"2  read       team-b   secret/a  reporting\n" +
		"3  read       team-b   secret/z  -\n"
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
