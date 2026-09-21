package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSharedSelectorEmptyQueryPreservesSourceOrder(t *testing.T) {
	items := []SharedSelectorItem{
		{ID: "third", Label: "zeta", SearchText: "zeta"},
		{ID: "first", Label: "alpha", SearchText: "alpha"},
		{ID: "second", Label: "beta", SearchText: "beta"},
	}

	got := filterSharedSelectorItems("", items)
	if len(got) != len(items) {
		t.Fatalf("filtered item count = %d, want %d", len(got), len(items))
	}
	for index := range items {
		if got[index].ID != items[index].ID {
			t.Errorf("filtered item %d ID = %q, want source ID %q", index, got[index].ID, items[index].ID)
		}
	}
}

func TestSharedSelectorFiltersCaseInsensitiveFuzzyAcrossCompleteSearchText(t *testing.T) {
	items := []SharedSelectorItem{
		{ID: "database", Label: "team-a", SearchText: "alpha platform database team-a kv-get"},
		{ID: "reports", Label: "team-b", SearchText: "omega reporting credentials team-b read"},
		{ID: "api", Label: "team-c", SearchText: "gamma production service team-c read"},
	}

	got := filterSharedSelectorItems("PLTDB", items)
	if len(got) != 1 || got[0].ID != "database" {
		t.Fatalf("PLTDB matches = %#v, want database from path text", got)
	}
	got = filterSharedSelectorItems("OMGCRD", items)
	if len(got) != 1 || got[0].ID != "reports" {
		t.Fatalf("OMGCRD matches = %#v, want reports from note text", got)
	}
}

func TestSharedSelectorFuzzyTiesKeepDeterministicSourceOrder(t *testing.T) {
	items := []SharedSelectorItem{
		{ID: "first", Label: "first", SearchText: "same searchable text"},
		{ID: "second", Label: "second", SearchText: "same searchable text"},
	}

	got := filterSharedSelectorItems("SMTXT", items)
	if len(got) != 2 {
		t.Fatalf("filtered item count = %d, want 2", len(got))
	}
	if got[0].ID != "first" || got[1].ID != "second" {
		t.Fatalf("filtered IDs = %q, %q, want source order", got[0].ID, got[1].ID)
	}
}

func TestSharedSelectorTypingFiltersImmediatelyAndReturnsStableIdentity(t *testing.T) {
	items := []SharedSelectorItem{
		{ID: "opaque-a", Label: "same display", SearchText: "team-a database"},
		{ID: "opaque-b", Label: "same display", SearchText: "team-b reports"},
	}
	model, err := newSharedSelectorModel("Select a resource", "", items, "", newPresentation(fixedTerminal{}))
	if err != nil {
		t.Fatalf("new selector model error = %v", err)
	}

	model = updateSharedSelectorModel(t, model, tea.KeyPressMsg(tea.Key{Text: "TR", Code: 't'}))
	if model.query != "TR" {
		t.Fatalf("query = %q, want TR", model.query)
	}
	if len(model.visible) != 1 || model.visible[0].ID != "opaque-b" {
		t.Fatalf("visible items = %#v, want opaque-b", model.visible)
	}

	updated, command := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(sharedSelectorModel)
	if command == nil {
		t.Fatal("enter command is nil, want quit command")
	}
	if model.selectedID != "opaque-b" {
		t.Errorf("selected ID = %q, want opaque-b", model.selectedID)
	}
}

func TestSharedSelectorNavigationStartsAtPreselectedIdentity(t *testing.T) {
	items := []SharedSelectorItem{
		{ID: "one", Label: "one", SearchText: "one"},
		{ID: "two", Label: "two", SearchText: "two"},
		{ID: "three", Label: "three", SearchText: "three"},
	}
	model, err := newSharedSelectorModel("Select", "", items, "two", newPresentation(fixedTerminal{}))
	if err != nil {
		t.Fatalf("new selector model error = %v", err)
	}
	if model.cursor != 1 {
		t.Fatalf("initial cursor = %d, want preselected index 1", model.cursor)
	}

	model = updateSharedSelectorModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if got := updated.(sharedSelectorModel).selectedID; got != "three" {
		t.Errorf("selected ID = %q, want three", got)
	}
}

func TestSharedSelectorNoMatchesAndBackspaceAreSafe(t *testing.T) {
	items := []SharedSelectorItem{{ID: "one", Label: "one", SearchText: "one"}}
	model, err := newSharedSelectorModel("Select", "", items, "", newPresentation(fixedTerminal{}))
	if err != nil {
		t.Fatalf("new selector model error = %v", err)
	}

	model = updateSharedSelectorModel(t, model, tea.KeyPressMsg(tea.Key{Text: "z", Code: 'z'}))
	if len(model.visible) != 0 {
		t.Fatalf("visible item count = %d, want no matches", len(model.visible))
	}
	updated, command := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(sharedSelectorModel)
	if command != nil || model.selectedID != "" {
		t.Fatalf("enter on no matches selected %q with command %v", model.selectedID, command)
	}
	model = updateSharedSelectorModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	if model.query != "" || len(model.visible) != 1 || model.visible[0].ID != "one" {
		t.Fatalf("backspace state = query %q, visible %#v", model.query, model.visible)
	}
}

func TestSharedSelectorHandlesPasteCancellationAndInterrupt(t *testing.T) {
	items := []SharedSelectorItem{{ID: "one", Label: "one", SearchText: "production database"}}
	model, err := newSharedSelectorModel("Select", "", items, "", newPresentation(fixedTerminal{}))
	if err != nil {
		t.Fatalf("new selector model error = %v", err)
	}
	model = updateSharedSelectorModel(t, model, tea.PasteMsg{Content: "PDB"})
	if model.query != "PDB" || len(model.visible) != 1 {
		t.Fatalf("paste state = query %q, visible %#v", model.query, model.visible)
	}

	for _, message := range []tea.Msg{
		tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}),
		tea.InterruptMsg{},
	} {
		candidate, err := newSharedSelectorModel("Select", "", items, "", newPresentation(fixedTerminal{}))
		if err != nil {
			t.Fatalf("new selector model error = %v", err)
		}
		updated, command := candidate.Update(message)
		candidate = updated.(sharedSelectorModel)
		if command == nil || !candidate.canceled {
			t.Errorf("message %T = command %v, canceled %t; want quit and canceled", message, command, candidate.canceled)
		}
	}
}

func TestSharedSelectorRejectsInvalidItemsAndCanceledContext(t *testing.T) {
	selector := NewSharedSelector(nil, nil, fixedTerminal{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name  string
		ctx   context.Context
		items []SharedSelectorItem
		want  error
	}{
		{name: "empty", ctx: context.Background(), want: ErrSharedSelectorEmpty},
		{name: "empty identity", ctx: context.Background(), items: []SharedSelectorItem{{Label: "one"}}, want: ErrSharedSelectorInvalidItems},
		{name: "duplicate identity", ctx: context.Background(), items: []SharedSelectorItem{{ID: "same", Label: "one"}, {ID: "same", Label: "two"}}, want: ErrSharedSelectorInvalidItems},
		{name: "canceled context", ctx: ctx, items: []SharedSelectorItem{{ID: "one", Label: "one"}}, want: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := selector.Select(tt.ctx, "Select", "", tt.items, "")
			if !errors.Is(err, tt.want) {
				t.Errorf("Select() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSharedSelectorRendersCompactInlinePlainView(t *testing.T) {
	items := make([]SharedSelectorItem, 10)
	for index := range items {
		items[index] = SharedSelectorItem{
			ID:         string(rune('a' + index)),
			Label:      "resource-" + string(rune('a'+index)),
			SearchText: "resource-" + string(rune('a'+index)),
		}
	}
	model, err := newSharedSelectorModel("Select a resource", "", items, "", newPresentation(fixedTerminal{}))
	if err != nil {
		t.Fatalf("new selector model error = %v", err)
	}

	view := model.View()
	if view.AltScreen {
		t.Fatal("selector view enables alternate screen")
	}
	if strings.Contains(view.Content, "\x1b[") {
		t.Fatalf("plain selector view contains ANSI: %q", view.Content)
	}
	if !strings.Contains(view.Content, "Select a resource\nSearch: \n> resource-a") {
		t.Fatalf("selector view = %q, want title, immediate search, and selected row", view.Content)
	}
	if strings.Contains(view.Content, "resource-j") {
		t.Fatalf("selector view is not compact: %q", view.Content)
	}
	if lineCount := strings.Count(view.Content, "\n") + 1; lineCount > sharedSelectorMaxVisible+4 {
		t.Errorf("selector line count = %d, want at most %d", lineCount, sharedSelectorMaxVisible+4)
	}
}

func TestSharedSelectorShowsProfileAndFavoriteColumnHeaders(t *testing.T) {
	for _, tt := range []struct {
		title  string
		header string
	}{
		{title: "Select a profile", header: "#  ACTIVE  NAME"},
		{title: "Select a favorite", header: "#  RUNS  OPERATION"},
	} {
		t.Run(tt.title, func(t *testing.T) {
			model, err := newSharedSelectorModel(tt.title, tt.header, []SharedSelectorItem{{ID: "one", Label: "1  one"}}, "", newPresentation(fixedTerminal{}))
			if err != nil {
				t.Fatal(err)
			}
			view := model.View().Content
			if !strings.Contains(view, "Search: \n  "+tt.header+"\n> 1  one") {
				t.Errorf("selector view = %q, want header between search and rows", view)
			}
		})
	}
}

func TestSharedSelectorColorsEachProfileRowAndKeepsSelectionVisible(t *testing.T) {
	items := []SharedSelectorItem{
		{ID: "first", Label: "team-a", Color: "#112233"},
		{ID: "second", Label: "team-b", Color: "#445566"},
		{ID: "third", Label: "team-c"},
	}
	for _, tt := range []struct {
		name     string
		terminal Terminal
		colored  bool
	}{
		{name: "color", terminal: fixedTerminal{color: true}, colored: true},
		{name: "plain", terminal: fixedTerminal{color: false}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			model, err := newSharedSelectorModel("Select", "", items, "second", newPresentation(tt.terminal))
			if err != nil {
				t.Fatalf("new selector model error = %v", err)
			}
			lines := strings.Split(model.View().Content, "\n")
			if len(lines) < 5 {
				t.Fatalf("selector lines = %#v, want three rows", lines)
			}
			if tt.colored {
				if !strings.Contains(lines[2], "38;2;17;34;51m") || !strings.Contains(lines[3], "38;2;68;85;102m") {
					t.Errorf("profile rows lack their saved colors: %#v", lines[2:4])
				}
				if strings.Contains(lines[4], "\x1b[") {
					t.Errorf("uncolored row changed style: %q", lines[4])
				}
			} else if strings.Contains(model.View().Content, "\x1b[") {
				t.Errorf("plain selector contains ANSI: %q", model.View().Content)
			}
			if got := ansi.Strip(lines[3]); got != "> team-b" {
				t.Errorf("selected row = %q, want visible marker", got)
			}
		})
	}
}

func TestSharedSelectorAdapterRequiresConfiguredStreams(t *testing.T) {
	items := []SharedSelectorItem{{ID: "one", Label: "one"}}
	tests := []struct {
		name     string
		selector SharedSelector
		want     string
	}{
		{name: "input", selector: NewSharedSelector(nil, &bytes.Buffer{}, fixedTerminal{}), want: "input is not configured"},
		{name: "output", selector: NewSharedSelector(strings.NewReader(""), nil, fixedTerminal{}), want: "output is not configured"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.selector.Select(context.Background(), "Select", "", items, "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Select() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func updateSharedSelectorModel(t *testing.T, model sharedSelectorModel, message tea.Msg) sharedSelectorModel {
	t.Helper()
	updated, _ := model.Update(message)
	return updated.(sharedSelectorModel)
}
