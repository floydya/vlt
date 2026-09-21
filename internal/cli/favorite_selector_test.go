package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vlt/internal/config"
	"vlt/internal/favorite"
	"vlt/internal/profile"
)

func TestSharedFavoriteSelectorUsesDeterministicSearchableRowsAndOpaqueSelection(t *testing.T) {
	favorites := []favorite.Favorite{
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/z", Note: "reporting"},
		{Profile: "team-a", Operation: favorite.OperationKVGet, Path: "secret/a", Note: "daily\tnote\nsecond line"},
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/a", Note: "database credentials"},
	}
	ordered := favorite.NewService(favorites).List()
	shared := &recordingSharedSelector{selectedID: "favorite-000002"}
	selector := NewSharedFavoriteSelector(shared, nil)

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
			if !strings.Contains(item.Label+item.Detail, text) || !strings.Contains(item.SearchText, text) {
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
		{query: "kv-get team-a", want: ordered[0]},
		{query: "reporting", want: ordered[2]},
	} {
		matches := filterSharedSelectorItems(tt.query, shared.items)
		if len(matches) != 1 {
			t.Errorf("filter %q = %#v, want one match", tt.query, matches)
			continue
		}
		selected, selectErr := NewSharedFavoriteSelector(&recordingSharedSelector{selectedID: matches[0].ID}, nil).Select(context.Background(), favorites)
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
	selected, err := NewSharedFavoriteSelector(shared, nil).Select(context.Background(), favorites)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	wantRows := []string{
		"1  secret/z  (127 runs)",
		"2  secret/a  (8 runs)",
		"3  secret/a  (8 runs)",
	}
	if len(shared.items) != len(wantRows) {
		t.Fatalf("selector rows = %#v, want %d rows", shared.items, len(wantRows))
	}
	for index, want := range wantRows {
		if shared.items[index].Label != want || !strings.Contains(shared.items[index].Detail, "Profile: "+favorite.NewService(favorites).List()[index].Profile) {
			t.Errorf("row %d = %#v, want compact label %q and selected detail", index, shared.items[index], want)
		}
	}
	if selected != favorites[2] {
		t.Fatalf("Select() = %#v, want second count-first favorite %#v", selected, favorites[2])
	}
	if matches := filterSharedSelectorItems("127", shared.items); len(matches) != 1 || matches[0].ID != "favorite-000001" {
		t.Fatalf("count search = %#v, want first favorite", matches)
	}
}

func TestSharedFavoriteSelectorColorsEachRowFromItsOwnProfile(t *testing.T) {
	profiles := &fakeProfileStore{configuration: config.Configuration{
		Profiles:      []profile.Profile{{Name: "profile-a", Color: "#FF8800"}, {Name: "profile-b", Color: "#008844"}, {Name: "zz"}},
		ActiveProfile: "profile-b",
	}}
	favorites := []favorite.Favorite{
		{Profile: "profile-a", Operation: favorite.OperationRead, Path: "secret/profile-a"},
		{Profile: "profile-b", Operation: favorite.OperationRead, Path: "secret/profile-b"},
		{Profile: "zz", Operation: favorite.OperationRead, Path: "secret/zz"},
	}
	shared := &recordingSharedSelector{selectedID: "favorite-000001"}
	selected, err := NewSharedFavoriteSelector(shared, profiles).Select(context.Background(), favorites)
	if err != nil || selected != favorites[0] {
		t.Fatalf("selected = %#v, %v, want profile-a favorite", selected, err)
	}
	if len(shared.items) != 3 || shared.items[0].Color != "#FF8800" || shared.items[1].Color != "#008844" || shared.items[2].Color != "" {
		t.Fatalf("favorite row colors = %#v, want profile-a, profile-b, and uncolored rows", shared.items)
	}
	for _, tt := range []struct {
		name  string
		color bool
	}{
		{name: "color", color: true},
		{name: "plain"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			model, err := newSharedSelectorModel(shared.title, shared.header, shared.items, shared.items[0].ID, newPresentation(fixedTerminal{color: tt.color}))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(model.View().Content, "\n")
			if tt.color {
				if strings.Contains(lines[2], "38;2;255;136;0m") || !strings.Contains(lines[3], "38;2;255;136;0m") || !strings.Contains(lines[4], "38;2;0;136;68m") || strings.Contains(lines[5], "\x1b[") {
					t.Errorf("favorite selector colors = %#v, want only matching row colors", lines)
				}
				if count := strings.Count(lines[3], "38;2;255;136;0m"); count != 1 {
					t.Errorf("first favorite color count = %d, want one color on its full row: %q", count, lines[3])
				}
			} else if strings.Contains(model.View().Content, "\x1b[") {
				t.Errorf("plain selector contains ANSI: %q", model.View().Content)
			}
			if !strings.Contains(lines[3], "> ") {
				t.Errorf("selected row lost its marker: %q", lines[3])
			}
		})
	}
}

func TestSharedFavoriteSelectorShowsDashForEmptyNote(t *testing.T) {
	shared := &recordingSharedSelector{selectedID: "favorite-000001"}
	selector := NewSharedFavoriteSelector(shared, nil)
	candidate := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}

	selected, err := selector.Select(context.Background(), []favorite.Favorite{candidate})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selected != candidate {
		t.Fatalf("Select() = %#v, want unchanged favorite %#v", selected, candidate)
	}
	if !strings.Contains(shared.items[0].Detail, "Note: -") {
		t.Errorf("empty-note detail = %q, want dash", shared.items[0].Detail)
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
			selected, err := NewSharedFavoriteSelector(tt.shared, nil).Select(tt.ctx, []favorite.Favorite{candidate})
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
	selector := NewSharedFavoriteSelector(shared, nil)

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
	selector := NewSharedFavoriteSelector(nil, nil)
	_, err := selector.Select(context.Background(), []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
	}})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("Select() error = %v, want configuration error", err)
	}
}
