package favorite

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRandomIDHandlesReaderFailure(t *testing.T) {
	if _, err := randomID(bytes.NewReader([]byte{1, 2})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("randomID error = %v, want short read", err)
	}
}

func TestResolveFavoriteIDRejectsUnknownAndMalformedValues(t *testing.T) {
	candidate := Favorite{ID: "f_0123456789abcdef", Profile: "team-a", Operation: OperationRead, Path: "secret/a"}
	service := NewService([]Favorite{candidate})
	for _, selector := range []string{"f_invalid", "f_abcdef0123456789"} {
		if selected, err := service.Resolve(selector); err == nil || selected != (Favorite{}) {
			t.Fatalf("Resolve(%q) = %#v, %v", selector, selected, err)
		}
	}
}

func TestStoreLoadsV1WithStableIDsWithoutWriting(t *testing.T) {
	path := testFavoritePath(t)
	before := []byte(`{"version":1,"favorites":[{"profile":"team-a","operation":"read","path":"secret/a","note":"first"},{"profile":"team-a","operation":"kv-get","path":"secret/b","note":"second"}]}`)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	first, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Favorites) != 2 || first.Favorites[0].ID == "" || first.Favorites[1].ID == "" || first.Favorites[0].ID == first.Favorites[1].ID {
		t.Fatalf("loaded IDs = %#v", first.Favorites)
	}
	for index := range first.Favorites {
		if first.Favorites[index].ID != second.Favorites[index].ID {
			t.Fatalf("ID changed between reads: %#v, %#v", first, second)
		}
	}
	afterRead, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, afterRead) {
		t.Fatal("read-only load changed favorites file")
	}
	if err := store.Save(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	afterWrite, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(afterWrite, []byte(`"version": 2`)) {
		t.Fatalf("saved document did not migrate: %s", afterWrite)
	}
	reloaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for index := range first.Favorites {
		if first.Favorites[index].ID != reloaded.Favorites[index].ID {
			t.Fatalf("ID changed during migration: %#v, %#v", first, reloaded)
		}
	}
}

func TestStoreRejectsBadVersionTwoIDs(t *testing.T) {
	tests := []struct {
		name string
		ids  string
	}{
		{name: "missing", ids: `"","f_0123456789abcdef"`},
		{name: "invalid", ids: `"f_bad","f_0123456789abcdef"`},
		{name: "duplicate", ids: `"f_0123456789abcdef","f_0123456789abcdef"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := testFavoritePath(t)
			ids := strings.Split(tt.ids, ",")
			contents := `{"version":2,"favorites":[{"id":` + ids[0] + `,"profile":"team-a","operation":"read","path":"secret/a"},{"id":` + ids[1] + `,"profile":"team-a","operation":"read","path":"secret/b"}]}`
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewStore(path).Load(context.Background()); err == nil || !strings.Contains(err.Error(), "ID") {
				t.Fatalf("Load() error = %v, want ID rejection", err)
			}
		})
	}
}

func TestMutationServiceAddAssignsIDAndUpdateKeepsIt(t *testing.T) {
	store := &mutationFavoriteStore{}
	service := NewMutationService(store, mutationProfiles("team-a"))
	added, err := service.AddWithResult(context.Background(), testFavorite("team-a", OperationRead, "secret/a", "first"))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.configuration.Favorites) != 1 {
		t.Fatalf("favorites = %#v", store.configuration.Favorites)
	}
	id := store.configuration.Favorites[0].ID
	if !validID(id) {
		t.Fatalf("generated ID = %q", id)
	}
	if added.ID != id {
		t.Fatalf("returned ID = %q, want %q", added.ID, id)
	}
	newPath := "secret/b"
	updated, err := service.UpdateWithResult(context.Background(), id, FavoriteChanges{Path: &newPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.configuration.Favorites[0]; got.ID != id || got.Path != newPath || updated != got {
		t.Fatalf("updated favorite = %#v, want stable ID %q", got, id)
	}
	resolved, err := NewService(store.configuration.Favorites).Resolve(id)
	if err != nil || resolved.Path != newPath {
		t.Fatalf("Resolve(%q) = %#v, %v", id, resolved, err)
	}
}

func TestMutationServiceRejectsChangedGuidedSelection(t *testing.T) {
	before := Favorite{ID: "f_0123456789abcdef", Profile: "team-a", Operation: OperationRead, Path: "secret/a", Note: "reviewed", RunCount: 1}
	current := before
	current.Path = "secret/b"
	store := &mutationFavoriteStore{configuration: Configuration{Favorites: []Favorite{current}}}
	service := NewMutationService(store, mutationProfiles("team-a"))
	newNote := "edited"
	if _, err := service.UpdateWithResult(context.Background(), before.ID, FavoriteChanges{Note: &newNote}, &before); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("Update() error = %v, want changed selection", err)
	}
	if _, err := service.RemoveWithResult(context.Background(), before.ID, &before); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("Remove() error = %v, want changed selection", err)
	}
	if store.saveCalls != 0 || store.configuration.Favorites[0] != current {
		t.Fatalf("stale guided action changed favorite: %#v", store.configuration)
	}
	before = current
	before.RunCount = 0
	if _, err := service.UpdateWithResult(context.Background(), current.ID, FavoriteChanges{Note: &newNote}, &before); err != nil {
		t.Fatalf("count-only change blocked update: %v", err)
	}
}
