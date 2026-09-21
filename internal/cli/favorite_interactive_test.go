package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vlt/internal/config"
	"vlt/internal/favorite"
	"vlt/internal/profile"
)

type fakeFavoriteManagementSelector struct {
	candidates []favorite.Favorite
	selected   favorite.Favorite
	err        error
	calls      int
}

type queuedSharedSelector struct {
	titles         []string
	items          [][]SharedSelectorItem
	preselectedIDs []string
	selections     []string
	errors         []error
}

func (s *queuedSharedSelector) Select(
	_ context.Context,
	title string,
	_ string,
	items []SharedSelectorItem,
	preselectedID string,
) (string, error) {
	s.titles = append(s.titles, title)
	s.items = append(s.items, append([]SharedSelectorItem(nil), items...))
	s.preselectedIDs = append(s.preselectedIDs, preselectedID)
	index := len(s.titles) - 1
	if index < len(s.errors) && s.errors[index] != nil {
		return "", s.errors[index]
	}
	if index < len(s.selections) {
		return s.selections[index], nil
	}
	return "", nil
}

func (s *fakeFavoriteManagementSelector) Select(_ context.Context, candidates []favorite.Favorite) (favorite.Favorite, error) {
	s.calls++
	s.candidates = append([]favorite.Favorite(nil), candidates...)
	return s.selected, s.err
}

type fakeFavoriteForm struct {
	request FavoriteFormRequest
	result  favorite.Favorite
	err     error
	calls   int
}

func (f *fakeFavoriteForm) Run(_ context.Context, request FavoriteFormRequest) (favorite.Favorite, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

type fakeFavoriteRemovalConfirmer struct {
	request   FavoriteRemovalConfirmation
	confirmed bool
	err       error
	calls     int
}

func (c *fakeFavoriteRemovalConfirmer) Confirm(_ context.Context, request FavoriteRemovalConfirmation) (bool, error) {
	c.calls++
	c.request = request
	return c.confirmed, c.err
}

func interactiveFavoriteDependencies(
	profiles *fakeProfileStore,
	favorites *fakeFavoriteStore,
	mutations *recordingFavoriteMutator,
	selector *fakeFavoriteManagementSelector,
	form *fakeFavoriteForm,
	confirmer *fakeFavoriteRemovalConfirmer,
	output *bytes.Buffer,
) FavoriteDependencies {
	return FavoriteDependencies{
		Profiles: profiles, Favorites: favorites, Mutations: mutations, Output: output,
		Terminal: switchTerminal{prompts: true}, ManagementSelector: selector,
		Form: form, RemovalConfirmer: confirmer,
	}
}

func TestFavoriteInteractiveAddUsesSortedProfilesAndReadDefault(t *testing.T) {
	profiles := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{
		{Name: "team-b"}, {Name: "team-a"},
	}}}
	favorites := &fakeFavoriteStore{}
	mutations := &recordingFavoriteMutator{}
	form := &fakeFavoriteForm{result: favorite.Favorite{
		Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/app", Note: "daily",
	}}
	var output bytes.Buffer
	handler := NewFavoriteHandler(interactiveFavoriteDependencies(
		profiles, favorites, mutations, &fakeFavoriteManagementSelector{}, form, &fakeFavoriteRemovalConfirmer{}, &output,
	))

	if err := handler(context.Background(), []string{"add"}); err != nil {
		t.Fatalf("favorite add error = %v", err)
	}
	if form.calls != 1 {
		t.Fatalf("form calls = %d, want 1", form.calls)
	}
	if got := form.request.Favorite.Operation; got != favorite.OperationRead {
		t.Fatalf("default operation = %q, want read", got)
	}
	if got := []string{form.request.Profiles[0].Name, form.request.Profiles[1].Name}; !reflect.DeepEqual(got, []string{"team-a", "team-b"}) {
		t.Fatalf("form profiles = %#v, want sorted names", got)
	}
	if !reflect.DeepEqual(mutations.added, []favorite.Favorite{form.result}) {
		t.Fatalf("Add() values = %#v, want %#v", mutations.added, []favorite.Favorite{form.result})
	}
	if !strings.Contains(output.String(), "Added favorite") {
		t.Fatalf("output = %q, want success", output.String())
	}
}

func TestFavoriteInteractiveAddPassesActiveProfileToForm(t *testing.T) {
	profiles := &fakeProfileStore{configuration: config.Configuration{
		ActiveProfile: "team-b",
		Profiles:      []profile.Profile{{Name: "team-a"}, {Name: "team-b"}},
	}}
	form := &fakeFavoriteForm{result: favorite.Favorite{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/app"}}
	handler := NewFavoriteHandler(interactiveFavoriteDependencies(
		profiles, &fakeFavoriteStore{}, &recordingFavoriteMutator{}, &fakeFavoriteManagementSelector{}, form,
		&fakeFavoriteRemovalConfirmer{}, &bytes.Buffer{},
	))
	if err := handler(context.Background(), []string{"add"}); err != nil {
		t.Fatal(err)
	}
	if form.request.ActiveProfile != "team-b" {
		t.Fatalf("form active profile = %q", form.request.ActiveProfile)
	}
}

func TestFavoriteInteractiveAddRequiresProfilesBeforeOpeningForm(t *testing.T) {
	profiles := &fakeProfileStore{}
	mutations := &recordingFavoriteMutator{}
	form := &fakeFavoriteForm{}
	handler := NewFavoriteHandler(interactiveFavoriteDependencies(
		profiles, &fakeFavoriteStore{}, mutations, &fakeFavoriteManagementSelector{}, form,
		&fakeFavoriteRemovalConfirmer{}, &bytes.Buffer{},
	))

	err := handler(context.Background(), []string{"add"})
	if err == nil || !strings.Contains(err.Error(), "vlt profile add") {
		t.Fatalf("favorite add error = %v, want profile guidance", err)
	}
	if form.calls != 0 || len(mutations.added) != 0 {
		t.Fatalf("empty profiles used form or mutation: form=%d add=%#v", form.calls, mutations.added)
	}
}

func TestFavoriteInteractiveUpdateSelectsStableFavoriteAndChangesOnlyEditedFields(t *testing.T) {
	alpha := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a", Note: "old"}
	zulu := favorite.Favorite{Profile: "team-b", Operation: favorite.OperationKVGet, Path: "secret/z", Note: "z"}
	profiles := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{{Name: "team-b"}, {Name: "team-a"}}}}
	favorites := &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{zulu, alpha}}}
	selector := &fakeFavoriteManagementSelector{selected: alpha}
	updated := alpha
	updated.Note = "new"
	form := &fakeFavoriteForm{result: updated}
	mutations := &recordingFavoriteMutator{}
	var output bytes.Buffer
	handler := NewFavoriteHandler(interactiveFavoriteDependencies(
		profiles, favorites, mutations, selector, form, &fakeFavoriteRemovalConfirmer{}, &output,
	))

	if err := handler(context.Background(), []string{"update"}); err != nil {
		t.Fatalf("favorite update error = %v", err)
	}
	if !reflect.DeepEqual(selector.candidates, []favorite.Favorite{alpha, zulu}) {
		t.Fatalf("selector candidates = %#v, want sorted favorites", selector.candidates)
	}
	if form.request.Favorite != alpha {
		t.Fatalf("form favorite = %#v, want %#v", form.request.Favorite, alpha)
	}
	if len(mutations.updated) != 1 || mutations.updated[0].selector != "1" {
		t.Fatalf("Update() calls = %#v, want stable selector 1", mutations.updated)
	}
	changes := mutations.updated[0].changes
	if changes.Note == nil || *changes.Note != "new" || changes.Profile != nil || changes.Operation != nil || changes.Path != nil {
		t.Fatalf("Update() changes = %#v, want note only", changes)
	}
}

func TestFavoriteInteractiveUpdateByNumberSkipsSelector(t *testing.T) {
	current := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a", Note: "old"}
	profiles := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{{Name: "team-a"}}}}
	favorites := &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{current}}}
	selector := &fakeFavoriteManagementSelector{}
	updated := current
	updated.Path = "secret/new"
	form := &fakeFavoriteForm{result: updated}
	mutations := &recordingFavoriteMutator{}
	handler := NewFavoriteHandler(interactiveFavoriteDependencies(
		profiles, favorites, mutations, selector, form, &fakeFavoriteRemovalConfirmer{}, &bytes.Buffer{},
	))

	if err := handler(context.Background(), []string{"update", "1"}); err != nil {
		t.Fatalf("favorite update error = %v", err)
	}
	if selector.calls != 0 {
		t.Fatalf("selector calls = %d, want 0", selector.calls)
	}
	if len(mutations.updated) != 1 || mutations.updated[0].changes.Path == nil || *mutations.updated[0].changes.Path != "secret/new" {
		t.Fatalf("Update() calls = %#v, want path change", mutations.updated)
	}
}

func TestFavoriteInteractiveRemoveConfirmsBeforeMutation(t *testing.T) {
	selected := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a", Note: "note"}
	tests := []struct {
		name      string
		confirmed bool
		wantCalls int
	}{
		{name: "decline"},
		{name: "confirm", confirmed: true, wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			favorites := &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}}
			selector := &fakeFavoriteManagementSelector{selected: selected}
			confirmer := &fakeFavoriteRemovalConfirmer{confirmed: tt.confirmed}
			mutations := &recordingFavoriteMutator{}
			var output bytes.Buffer
			handler := NewFavoriteHandler(interactiveFavoriteDependencies(
				&fakeProfileStore{}, favorites, mutations, selector, &fakeFavoriteForm{}, confirmer, &output,
			))

			if err := handler(context.Background(), []string{"remove"}); err != nil {
				t.Fatalf("favorite remove error = %v", err)
			}
			if confirmer.request.Favorite != selected {
				t.Fatalf("confirmation favorite = %#v, want %#v", confirmer.request.Favorite, selected)
			}
			if len(mutations.removed) != tt.wantCalls {
				t.Fatalf("Remove() calls = %#v, want %d", mutations.removed, tt.wantCalls)
			}
			if !tt.confirmed && output.Len() != 0 {
				t.Fatalf("declined removal output = %q, want empty", output.String())
			}
		})
	}
}

func TestFavoriteInteractiveEmptyFavoritesGiveAddGuidance(t *testing.T) {
	commands := [][]string{{"update"}, {"remove"}}
	for _, command := range commands {
		t.Run(strings.Join(command, "_"), func(t *testing.T) {
			mutations := &recordingFavoriteMutator{}
			handler := NewFavoriteHandler(interactiveFavoriteDependencies(
				&fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{{Name: "team-a"}}}},
				&fakeFavoriteStore{}, mutations, &fakeFavoriteManagementSelector{}, &fakeFavoriteForm{},
				&fakeFavoriteRemovalConfirmer{}, &bytes.Buffer{},
			))
			err := handler(context.Background(), command)
			if err == nil || !strings.Contains(err.Error(), "vlt favorite add") {
				t.Fatalf("handler error = %v, want add guidance", err)
			}
			if len(mutations.updated)+len(mutations.removed) != 0 {
				t.Fatalf("empty favorites mutated state: %#v", mutations)
			}
		})
	}
}

func TestFavoriteInteractiveCancellationMakesNoMutation(t *testing.T) {
	selected := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}
	favorites := &fakeFavoriteStore{configuration: favorite.Configuration{Favorites: []favorite.Favorite{selected}}}
	mutations := &recordingFavoriteMutator{}
	selector := &fakeFavoriteManagementSelector{err: context.Canceled}
	handler := NewFavoriteHandler(interactiveFavoriteDependencies(
		&fakeProfileStore{}, favorites, mutations, selector, &fakeFavoriteForm{},
		&fakeFavoriteRemovalConfirmer{}, &bytes.Buffer{},
	))

	err := handler(context.Background(), []string{"remove"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("favorite remove error = %v, want context.Canceled", err)
	}
	if len(mutations.removed) != 0 {
		t.Fatalf("Remove() calls = %#v, want none", mutations.removed)
	}
}

func TestFavoriteIncompleteCommandsDoNotPromptOutsideTerminal(t *testing.T) {
	tests := []struct {
		arguments []string
		want      string
	}{
		{arguments: []string{"add"}, want: favoriteAddHelpText},
		{arguments: []string{"update"}, want: favoriteUpdateHelpText},
		{arguments: []string{"update", "1"}, want: favoriteUpdateHelpText},
		{arguments: []string{"remove"}, want: favoriteRemoveHelpText},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.arguments, "_"), func(t *testing.T) {
			selector := &fakeFavoriteManagementSelector{}
			form := &fakeFavoriteForm{}
			confirmer := &fakeFavoriteRemovalConfirmer{}
			handler := NewFavoriteHandler(FavoriteDependencies{
				Terminal: switchTerminal{}, ManagementSelector: selector, Form: form, RemovalConfirmer: confirmer,
			})
			err := handler(context.Background(), tt.arguments)
			requireAutomaticHelp(t, err, tt.want)
			if selector.calls != 0 || form.calls != 0 || confirmer.calls != 0 {
				t.Fatalf("non-TTY command prompted: selector=%d form=%d confirmer=%d", selector.calls, form.calls, confirmer.calls)
			}
		})
	}
}

func TestFavoriteFormUsesSharedProfileAndOperationSelectorsWithDefaults(t *testing.T) {
	shared := &queuedSharedSelector{selections: []string{"team-a", favorite.OperationRead}}
	var output bytes.Buffer
	form := huhFavoriteForm{
		input: &promptLineReader{lines: [][]byte{
			[]byte("secret/app\n"), []byte("daily\n"),
		}},
		output: &output, accessible: true, selector: shared,
	}
	profiles := []profile.Profile{
		{Name: "team-b", Address: "https://team-b.example"},
		{Name: "team-a", Address: "https://team-a.example", Namespace: "engineering", Color: "#112233"},
	}

	got, err := form.Run(context.Background(), FavoriteFormRequest{Profiles: profiles})
	if err != nil {
		t.Fatalf("favorite form error = %v; output = %q", err, output.String())
	}
	want := favorite.Favorite{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/app", Note: "daily",
	}
	if got != want {
		t.Fatalf("favorite form result = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(shared.titles, []string{"Select a profile", "Select an operation"}) {
		t.Errorf("selector titles = %#v", shared.titles)
	}
	if !reflect.DeepEqual(shared.preselectedIDs, []string{"team-a", favorite.OperationRead}) {
		t.Errorf("preselected IDs = %#v, want defaults", shared.preselectedIDs)
	}
	if len(shared.items) != 2 || shared.items[0][0].ID != "team-a" || shared.items[0][1].ID != "team-b" {
		t.Fatalf("profile choices = %#v, want sorted stable names", shared.items)
	}
	if shared.items[0][0].Color != "#112233" || !strings.Contains(shared.items[0][0].Detail, "#112233") || shared.items[0][1].Color != "" || !strings.HasSuffix(shared.items[0][1].Detail, "Color: -") {
		t.Errorf("favorite form profile rows = %#v, want saved color and plain fallback", shared.items[0])
	}
	if len(shared.items[1]) != 2 || shared.items[1][0].ID != favorite.OperationRead || shared.items[1][1].ID != favorite.OperationKVGet {
		t.Fatalf("operation choices = %#v, want read then kv-get", shared.items[1])
	}
	for _, text := range []string{"Path", "Note"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("favorite form output = %q, want %q", output.String(), text)
		}
	}
}

func TestFavoriteFormPreselectsAndPreservesPopulatedChoices(t *testing.T) {
	current := favorite.Favorite{
		Profile: "team-b", Operation: favorite.OperationKVGet, Path: "secret/old", Note: "old note",
	}
	shared := &queuedSharedSelector{selections: []string{"team-b", favorite.OperationKVGet}}
	var output bytes.Buffer
	form := huhFavoriteForm{
		input: &promptLineReader{lines: [][]byte{
			[]byte("secret/new\n"), []byte("old note\n"),
		}},
		output: &output, accessible: true, selector: shared,
	}

	got, err := form.Run(context.Background(), FavoriteFormRequest{
		Favorite: current, Profiles: []profile.Profile{{Name: "team-a"}, {Name: "team-b"}},
	})
	if err != nil {
		t.Fatalf("favorite form error = %v; output = %q", err, output.String())
	}
	want := current
	want.Path = "secret/new"
	if got != want {
		t.Fatalf("favorite form result = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(shared.preselectedIDs, []string{"team-b", favorite.OperationKVGet}) {
		t.Errorf("preselected IDs = %#v, want populated choices", shared.preselectedIDs)
	}
}

func TestFavoriteFormCancellationStopsBeforeFreeTextInput(t *testing.T) {
	current := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/old"}
	for _, tt := range []struct {
		name   string
		errors []error
	}{
		{name: "profile", errors: []error{ErrSharedSelectorCanceled}},
		{name: "operation", errors: []error{nil, context.Canceled}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			shared := &queuedSharedSelector{
				selections: []string{"team-a", favorite.OperationRead}, errors: tt.errors,
			}
			form := huhFavoriteForm{
				input: &promptLineReader{}, output: &bytes.Buffer{}, accessible: true, selector: shared,
			}
			got, err := form.Run(context.Background(), FavoriteFormRequest{
				Favorite: current, Profiles: []profile.Profile{{Name: "team-a"}},
			})
			if err == nil {
				t.Fatal("favorite form error = nil, want cancellation")
			}
			if got != (favorite.Favorite{}) {
				t.Errorf("favorite form result = %#v, want no partial result", got)
			}
		})
	}
}

func TestFavoriteManagementUsesSharedSelectorRowsAndStableIdentity(t *testing.T) {
	candidates := []favorite.Favorite{
		{Profile: "team-b", Operation: favorite.OperationKVGet, Path: "secret/b", Note: "daily", RunCount: 12},
		{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a", RunCount: 2},
	}
	shared := &recordingSharedSelector{selectedID: "favorite-000002"}
	profiles := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{
		{Name: "team-a", Color: "#112233"}, {Name: "team-b", Color: "#445566"},
	}}}
	selector := NewSharedFavoriteManagementSelector(shared, profiles)

	selected, err := selector.Select(context.Background(), candidates)
	if err != nil {
		t.Fatalf("favorite selector error = %v", err)
	}
	ordered := favorite.NewService(candidates).List()
	if selected != ordered[1] {
		t.Fatalf("selected favorite = %#v, want stable ordered favorite %#v", selected, ordered[1])
	}
	if len(shared.items) != 2 || shared.items[0].Label != "1  secret/b  (12 runs)" || shared.items[1].Label != "2  secret/a  (2 runs)" {
		t.Fatalf("management selector rows = %#v, want count-first rows", shared.items)
	}
	if shared.items[0].Color != "#445566" || shared.items[1].Color != "#112233" {
		t.Errorf("management selector colors = %#v, want each favorite profile color", shared.items)
	}
	for _, text := range []string{"secret/a", "secret/b", "team-a", "team-b", "kv-get", "daily"} {
		found := false
		for _, item := range shared.items {
			found = found || strings.Contains(item.SearchText, text)
		}
		if !found {
			t.Errorf("favorite selector items = %#v, want searchable %q", shared.items, text)
		}
	}
}

func TestFavoriteManagementSharedCancellationReturnsNoFavorite(t *testing.T) {
	selector := NewSharedFavoriteManagementSelector(&recordingSharedSelector{err: ErrSharedSelectorCanceled}, nil)

	selected, err := selector.Select(context.Background(), []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
	}})
	if !errors.Is(err, ErrFavoriteSelectionCanceled) {
		t.Fatalf("favorite selector error = %v, want favorite cancellation", err)
	}
	if selected != (favorite.Favorite{}) {
		t.Errorf("selected favorite = %#v, want empty", selected)
	}
}

func TestHuhFavoriteRemovalConfirmerNamesTarget(t *testing.T) {
	selected := favorite.Favorite{Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a"}
	var output bytes.Buffer
	confirmer := huhFavoriteRemovalConfirmer{
		input: strings.NewReader("y\n"), output: &output, accessible: true,
	}

	confirmed, err := confirmer.Confirm(context.Background(), FavoriteRemovalConfirmation{Favorite: selected})
	if err != nil {
		t.Fatalf("favorite confirmation error = %v", err)
	}
	if !confirmed {
		t.Fatal("confirmed = false, want true")
	}
	for _, text := range []string{"read", "secret/a", "team-a"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("favorite confirmation output = %q, want %q", output.String(), text)
		}
	}
}
