package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vlt/internal/favorite"
)

func TestSharedFavoriteSelectorUsesDeterministicSearchableRowsAndOpaqueSelection(t *testing.T) {
	favorites := []favorite.Favorite{
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/z", Note: "reporting"},
		{Profile: "team-a", Operation: favorite.OperationKVGet, Path: "secret/a", Note: "daily\tnote\nsecond line"},
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/a", Note: "database credentials"},
	}
	ordered := favorite.NewService(favorites).List()
	shared := &recordingSharedSelector{selectedID: "favorite-000002"}
	selector := NewSharedFavoriteSelector(shared)

	selected, err := selector.Select(context.Background(), favorites)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selected != ordered[1] {
		t.Fatalf("Select() = %#v, want opaque selection %#v", selected, ordered[1])
	}
	if shared.title != "Select a favorite" || shared.preselectedID != "" {
		t.Errorf("shared selector request = title %q, preselected %q", shared.title, shared.preselectedID)
	}
	if len(shared.items) != len(ordered) {
		t.Fatalf("shared item count = %d, want %d", len(shared.items), len(ordered))
	}
	for index, item := range shared.items {
		wantID := "favorite-00000" + string(rune('1'+index))
		if item.ID != wantID {
			t.Errorf("item %d ID = %q, want %q", index, item.ID, wantID)
		}
		for _, text := range []string{ordered[index].Path, ordered[index].Profile, ordered[index].Operation} {
			if !strings.Contains(item.Label, text) || !strings.Contains(item.SearchText, text) {
				t.Errorf("item %d = %#v, want searchable %q", index, item, text)
			}
		}
	}
	if strings.Contains(shared.items[0].Label, "\t") || strings.Contains(shared.items[0].Label, "\n") {
		t.Errorf("favorite label retains control characters: %q", shared.items[0].Label)
	}
	for _, tt := range []struct {
		query string
		want  favorite.Favorite
	}{
		{query: "DBCRD", want: ordered[1]},
		{query: "KVTMA", want: ordered[0]},
		{query: "RPRT", want: ordered[2]},
	} {
		matches := filterSharedSelectorItems(tt.query, shared.items)
		if len(matches) != 1 {
			t.Errorf("filter %q = %#v, want one match", tt.query, matches)
			continue
		}
		selected, selectErr := NewSharedFavoriteSelector(&recordingSharedSelector{selectedID: matches[0].ID}).Select(context.Background(), favorites)
		if selectErr != nil || selected != tt.want {
			t.Errorf("filter %q selected %#v, %v; want %#v", tt.query, selected, selectErr, tt.want)
		}
	}
}

func TestSharedFavoriteSelectorShowsCountFirstRowsAndSearchesCounts(t *testing.T) {
	favorites := []favorite.Favorite{
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/z", RunCount: 127},
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/a", RunCount: 8},
		{Profile: "team-a", Operation: favorite.OperationKVGet, Path: "secret/a", RunCount: 8},
	}
	shared := &recordingSharedSelector{selectedID: "favorite-000002"}
	selected, err := NewSharedFavoriteSelector(shared).Select(context.Background(), favorites)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	wantRows := []string{
		"1  127  read  team-b  secret/z  -",
		"2  8  kv-get  team-a  secret/a  -",
		"3  8  read  team-b  secret/a  -",
	}
	if len(shared.items) != len(wantRows) {
		t.Fatalf("selector rows = %#v, want %d rows", shared.items, len(wantRows))
	}
	for index, want := range wantRows {
		if shared.items[index].Label != want || shared.items[index].SearchText != want {
			t.Errorf("row %d = %#v, want label and search text %q", index, shared.items[index], want)
		}
	}
	if selected != favorites[2] {
		t.Fatalf("Select() = %#v, want second count-first favorite %#v", selected, favorites[2])
	}
	if matches := filterSharedSelectorItems("127", shared.items); len(matches) != 1 || matches[0].ID != "favorite-000001" {
		t.Fatalf("count search = %#v, want first favorite", matches)
	}
}

func TestSharedFavoriteSelectorShowsDashForEmptyNote(t *testing.T) {
	shared := &recordingSharedSelector{selectedID: "favorite-000001"}
	selector := NewSharedFavoriteSelector(shared)
	candidate := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}

	selected, err := selector.Select(context.Background(), []favorite.Favorite{candidate})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selected != candidate {
		t.Fatalf("Select() = %#v, want unchanged favorite %#v", selected, candidate)
	}
	if !strings.Contains(shared.items[0].Label, "-") {
		t.Errorf("empty-note label = %q, want dash", shared.items[0].Label)
	}
}

func TestSharedFavoriteSelectorMapsCancellationAndContext(t *testing.T) {
	candidate := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}
	tests := []struct {
		name   string
		ctx    context.Context
		shared *recordingSharedSelector
		want   error
	}{
		{name: "cancel", ctx: context.Background(), shared: &recordingSharedSelector{err: ErrSharedSelectorCanceled}, want: ErrFavoriteSelectionCanceled},
		{name: "interrupt", ctx: context.Background(), shared: &recordingSharedSelector{err: context.Canceled}, want: context.Canceled},
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	tests = append(tests, struct {
		name   string
		ctx    context.Context
		shared *recordingSharedSelector
		want   error
	}{name: "context", ctx: canceledContext, shared: &recordingSharedSelector{}, want: context.Canceled})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selected, err := NewSharedFavoriteSelector(tt.shared).Select(tt.ctx, []favorite.Favorite{candidate})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Select() error = %v, want %v", err, tt.want)
			}
			if selected != (favorite.Favorite{}) {
				t.Errorf("Select() = %#v, want no favorite", selected)
			}
		})
	}
}

func TestSharedFavoriteSelectorRejectsEmptyAndUnknownSelections(t *testing.T) {
	shared := &recordingSharedSelector{selectedID: "favorite-999999"}
	selector := NewSharedFavoriteSelector(shared)

	_, err := selector.Select(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "favorite add") {
		t.Fatalf("empty Select() error = %v, want add guidance", err)
	}
	if shared.calls != 0 {
		t.Fatalf("empty Select() shared calls = %d, want 0", shared.calls)
	}

	_, err = selector.Select(context.Background(), []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
	}})
	if err == nil || !strings.Contains(err.Error(), "unknown selection") {
		t.Fatalf("unknown Select() error = %v, want rejection", err)
	}
}

func TestSharedFavoriteSelectorRequiresSharedComponent(t *testing.T) {
	selector := NewSharedFavoriteSelector(nil)
	_, err := selector.Select(context.Background(), []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
	}})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("Select() error = %v, want configuration error", err)
	}
}
