package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"vlt/internal/favorite"
)

func TestFavoriteListShowsStableIDs(t *testing.T) {
	selected := favorite.Favorite{
		ID: "f_0123456789abcdef", Profile: "team-a", Operation: favorite.OperationRead,
		Path: "secret/a", RunCount: 4,
	}
	got := favoriteListOutput([]favorite.Favorite{selected}, nil, fixedTerminal{})
	if !strings.Contains(got, "ID") || !strings.Contains(got, selected.ID) {
		t.Fatalf("favorite list = %q, want ID column and %q", got, selected.ID)
	}
}

func TestGuidedFavoriteRemoveUsesStableID(t *testing.T) {
	selected := favorite.Favorite{
		ID: "f_0123456789abcdef", Profile: "team-a", Operation: favorite.OperationRead,
		Path: "secret/a", Note: "reviewed", RunCount: 2,
	}
	store := &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}}
	mutations := &recordingFavoriteMutator{}
	var output bytes.Buffer
	handler := NewFavoriteHandler(FavoriteDependencies{
		Favorites: store, Mutations: mutations, Output: &output,
		Terminal:           switchTerminal{prompts: true},
		ManagementSelector: &fakeFavoriteManagementSelector{selected: selected},
		RemovalConfirmer:   &fakeFavoriteRemovalConfirmer{confirmed: true},
	})
	if err := handler(context.Background(), []string{"remove"}); err != nil {
		t.Fatal(err)
	}
	if len(mutations.removed) != 1 || mutations.removed[0] != selected.ID {
		t.Fatalf("removed selectors = %#v, want %q", mutations.removed, selected.ID)
	}
	if !strings.Contains(output.String(), selected.ID) || !strings.Contains(output.String(), selected.Path) {
		t.Fatalf("status = %q, want stable ID and path", output.String())
	}
}
