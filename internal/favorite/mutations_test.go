package favorite

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"vlt/internal/profile"
)

func TestMutationServiceRecordUseReloadsAndIncrementsCurrentFavorite(t *testing.T) {
	selected := testFavorite("team-a", OperationRead, "secret/a", "old note")
	current := selected
	current.Note = "new note"
	current.RunCount = 4
	other := testFavorite("team-a", OperationKVGet, "secret/b", "other")
	other.RunCount = 2
	store := &mutationFavoriteStore{configuration: Configuration{Favorites: []Favorite{other, current}}}
	service := NewMutationService(store, mutationProfiles("team-a"))

	if err := service.RecordUse(context.Background(), selected); err != nil {
		t.Fatalf("RecordUse() error = %v", err)
	}
	current.RunCount = 5
	if want := []Favorite{other, current}; !reflect.DeepEqual(store.configuration.Favorites, want) {
		t.Fatalf("favorites after RecordUse() = %#v, want %#v", store.configuration.Favorites, want)
	}
	if store.loadCalls != 1 || store.saveCalls != 1 {
		t.Fatalf("store calls: loads=%d saves=%d, want one each", store.loadCalls, store.saveCalls)
	}
}

func TestMutationServiceRecordUseSkipsMissingChangedOrMaxCountFavorite(t *testing.T) {
	selected := testFavorite("team-a", OperationRead, "secret/a", "")
	changed := selected
	changed.Path = "secret/b"
	maxed := selected
	maxed.RunCount = math.MaxInt64
	for _, tt := range []struct {
		name      string
		favorites []Favorite
	}{
		{name: "removed"},
		{name: "changed command", favorites: []Favorite{changed}},
		{name: "maximum count", favorites: []Favorite{maxed}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &mutationFavoriteStore{configuration: Configuration{Favorites: tt.favorites}}
			service := NewMutationService(store, mutationProfiles("team-a"))
			if err := service.RecordUse(context.Background(), selected); err != nil {
				t.Fatalf("RecordUse() error = %v", err)
			}
			if store.saveCalls != 0 || !reflect.DeepEqual(store.configuration.Favorites, tt.favorites) {
				t.Fatalf("RecordUse() changed favorites: %#v, saves=%d", store.configuration.Favorites, store.saveCalls)
			}
		})
	}
}

func TestMutationServiceRecordUseRestoresStateAfterSaveFailure(t *testing.T) {
	selected := testFavorite("team-a", OperationRead, "secret/a", "")
	original := Configuration{Favorites: []Favorite{selected}}
	store := &mutationFavoriteStore{configuration: original, saveErrAt: 1, saveAfterErr: true}
	service := NewMutationService(store, mutationProfiles("team-a"))
	if err := service.RecordUse(context.Background(), selected); err == nil {
		t.Fatal("RecordUse() error = nil, want save failure")
	}
	if !reflect.DeepEqual(store.configuration, original) {
		t.Fatalf("favorites after failed RecordUse() = %#v, want %#v", store.configuration, original)
	}
}

type mutationFavoriteStore struct {
	configuration Configuration
	loadErr       error
	loadCalls     int
	saveCalls     int
	saveErrAt     int
	saveAfterErr  bool
}

func (s *mutationFavoriteStore) Load(context.Context) (Configuration, error) {
	s.loadCalls++
	if s.loadErr != nil {
		return Configuration{}, s.loadErr
	}
	return cloneFavoriteConfiguration(s.configuration), nil
}

func (s *mutationFavoriteStore) Save(_ context.Context, configuration Configuration) error {
	s.saveCalls++
	if s.saveErrAt == s.saveCalls && !s.saveAfterErr {
		return errors.New("injected favorite persistence failure")
	}
	s.configuration = cloneFavoriteConfiguration(configuration)
	if s.saveErrAt == s.saveCalls {
		return errors.New("injected favorite persistence failure after replacement")
	}
	return nil
}

type mutationProfileStore struct {
	configuration profile.Configuration
	loadErr       error
	loadCalls     int
}

func (s *mutationProfileStore) Load(context.Context) (profile.Configuration, error) {
	s.loadCalls++
	if s.loadErr != nil {
		return profile.Configuration{}, s.loadErr
	}
	result := s.configuration
	result.Profiles = append([]profile.Profile(nil), result.Profiles...)
	return result, nil
}

func mutationProfiles(names ...string) *mutationProfileStore {
	profiles := make([]profile.Profile, len(names))
	for index, name := range names {
		profiles[index] = profile.Profile{Name: name}
	}
	return &mutationProfileStore{configuration: profile.Configuration{Profiles: profiles}}
}

func TestMutationServiceAddPersistsFavorite(t *testing.T) {
	original := Configuration{Favorites: []Favorite{
		testFavorite("team-a", OperationRead, "secret/a", "first"),
	}}
	favorites := &mutationFavoriteStore{configuration: original}
	profiles := mutationProfiles("team-a", "team-b")
	service := NewMutationService(favorites, profiles)
	added := testFavorite("team-b", OperationKVGet, "secret/b", "second")

	if err := service.Add(context.Background(), added); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	want := Configuration{Favorites: []Favorite{original.Favorites[0], added}}
	if !reflect.DeepEqual(favorites.configuration, want) {
		t.Fatalf("configuration after Add() = %#v, want %#v", favorites.configuration, want)
	}
}

func TestMutationServiceAddRejectsInvalidDuplicateOrMissingProfileWithoutChangingState(t *testing.T) {
	original := Configuration{Favorites: []Favorite{
		testFavorite("team-a", OperationRead, "secret/a", "original"),
	}}
	tests := []struct {
		name      string
		candidate Favorite
		profiles  *mutationProfileStore
		wantErr   string
	}{
		{
			name:      "invalid favorite",
			candidate: testFavorite("team-a", "write", "secret/b", ""),
			profiles:  mutationProfiles("team-a"),
			wantErr:   "invalid favorite",
		},
		{
			name:      "duplicate tuple with different note",
			candidate: testFavorite("team-a", OperationRead, "secret/a", "replacement"),
			profiles:  mutationProfiles("team-a"),
			wantErr:   "already exists",
		},
		{
			name:      "missing profile",
			candidate: testFavorite("team-b", OperationRead, "secret/b", ""),
			profiles:  mutationProfiles("team-a"),
			wantErr:   "profile does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			favorites := &mutationFavoriteStore{configuration: original}
			service := NewMutationService(favorites, tt.profiles)

			err := service.Add(context.Background(), tt.candidate)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Add() error = %v, want %q", err, tt.wantErr)
			}
			if !reflect.DeepEqual(favorites.configuration, original) {
				t.Fatalf("configuration after failed Add() = %#v, want %#v", favorites.configuration, original)
			}
			if favorites.saveCalls != 0 {
				t.Fatalf("Save() calls = %d, want 0", favorites.saveCalls)
			}
		})
	}
}

func TestMutationServiceAddPropagatesLoadFailuresWithoutSaving(t *testing.T) {
	tests := []struct {
		name      string
		favorites *mutationFavoriteStore
		profiles  *mutationProfileStore
		wantErr   string
	}{
		{
			name:      "profile load",
			favorites: &mutationFavoriteStore{},
			profiles:  &mutationProfileStore{loadErr: errors.New("profiles unavailable")},
			wantErr:   "load profiles",
		},
		{
			name:      "favorite load",
			favorites: &mutationFavoriteStore{loadErr: errors.New("favorites unavailable")},
			profiles:  mutationProfiles("team-a"),
			wantErr:   "load favorites",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewMutationService(tt.favorites, tt.profiles)
			err := service.Add(context.Background(), testFavorite("team-a", OperationRead, "secret/a", ""))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Add() error = %v, want %q", err, tt.wantErr)
			}
			if tt.favorites.saveCalls != 0 {
				t.Fatalf("Save() calls = %d, want 0", tt.favorites.saveCalls)
			}
		})
	}
}

func TestMutationServiceAddRestoresStateAfterPersistenceFailure(t *testing.T) {
	for _, saveAfterErr := range []bool{false, true} {
		name := "before replacement"
		if saveAfterErr {
			name = "after replacement"
		}
		t.Run(name, func(t *testing.T) {
			original := Configuration{Favorites: []Favorite{
				testFavorite("team-a", OperationRead, "secret/a", "original"),
			}}
			favorites := &mutationFavoriteStore{
				configuration: original,
				saveErrAt:     1,
				saveAfterErr:  saveAfterErr,
			}
			service := NewMutationService(favorites, mutationProfiles("team-a", "team-b"))

			err := service.Add(context.Background(), testFavorite("team-b", OperationRead, "secret/b", ""))
			if err == nil || !strings.Contains(err.Error(), "persist favorite") {
				t.Fatalf("Add() error = %v, want persistence error", err)
			}
			if !reflect.DeepEqual(favorites.configuration, original) {
				t.Fatalf("configuration after failed Add() = %#v, want %#v", favorites.configuration, original)
			}
		})
	}
}

func TestMutationServiceUpdateChangesOnlySuppliedFieldsAndClearsNote(t *testing.T) {
	originalFavorite := testFavorite("team-a", OperationRead, "secret/a", "original note")
	favorites := &mutationFavoriteStore{configuration: Configuration{Favorites: []Favorite{originalFavorite}}}
	service := NewMutationService(favorites, mutationProfiles("team-a", "team-b"))
	profileName := "team-b"
	path := "secret/b"
	note := ""

	err := service.Update(context.Background(), "1", FavoriteChanges{
		Profile: &profileName,
		Path:    &path,
		Note:    &note,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	want := originalFavorite
	want.Profile = profileName
	want.Path = path
	want.Note = ""
	if !reflect.DeepEqual(favorites.configuration.Favorites, []Favorite{want}) {
		t.Fatalf("favorites after Update() = %#v, want %#v", favorites.configuration.Favorites, []Favorite{want})
	}
}

func TestMutationServiceUpdateResetsRunCountOnlyWhenCommandChanges(t *testing.T) {
	original := testFavorite("team-a", OperationRead, "secret/a", "original")
	original.RunCount = 8
	note := "updated"
	profileName := "team-b"
	operation := OperationKVGet
	path := "secret/b"
	tests := []struct {
		name      string
		changes   FavoriteChanges
		want      Favorite
		wantSaves int
	}{
		{name: "note only", changes: FavoriteChanges{Note: &note}, want: Favorite{Profile: original.Profile, Operation: original.Operation, Path: original.Path, Note: note, RunCount: 8}, wantSaves: 1},
		{name: "unchanged command", changes: FavoriteChanges{Profile: &original.Profile, Operation: &original.Operation, Path: &original.Path}, want: original},
		{name: "changed profile", changes: FavoriteChanges{Profile: &profileName}, want: Favorite{Profile: profileName, Operation: original.Operation, Path: original.Path, Note: original.Note}, wantSaves: 1},
		{name: "changed operation", changes: FavoriteChanges{Operation: &operation}, want: Favorite{Profile: original.Profile, Operation: operation, Path: original.Path, Note: original.Note}, wantSaves: 1},
		{name: "changed path", changes: FavoriteChanges{Path: &path}, want: Favorite{Profile: original.Profile, Operation: original.Operation, Path: path, Note: original.Note}, wantSaves: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			favorites := &mutationFavoriteStore{configuration: Configuration{Favorites: []Favorite{original}}}
			service := NewMutationService(favorites, mutationProfiles("team-a", "team-b"))
			if err := service.Update(context.Background(), "1", tt.changes); err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			if got := favorites.configuration.Favorites[0]; got != tt.want {
				t.Fatalf("favorite after Update() = %#v, want %#v", got, tt.want)
			}
			if favorites.saveCalls != tt.wantSaves {
				t.Fatalf("Save() calls = %d, want %d", favorites.saveCalls, tt.wantSaves)
			}
		})
	}
}

func TestMutationServiceUpdateResolvesCurrentSortedOrder(t *testing.T) {
	zulu := testFavorite("team-a", OperationRead, "secret/z", "z")
	alpha := testFavorite("team-a", OperationRead, "secret/a", "a")
	favorites := &mutationFavoriteStore{configuration: Configuration{Favorites: []Favorite{zulu, alpha}}}
	service := NewMutationService(favorites, mutationProfiles("team-a"))
	note := "updated"

	if err := service.Update(context.Background(), "1", FavoriteChanges{Note: &note}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	want := []Favorite{zulu, alpha}
	want[1].Note = note
	if !reflect.DeepEqual(favorites.configuration.Favorites, want) {
		t.Fatalf("favorites after Update() = %#v, want %#v", favorites.configuration.Favorites, want)
	}
}

func TestMutationServiceUpdateRejectsInvalidMissingProfileOrDuplicateWithoutChangingState(t *testing.T) {
	original := Configuration{Favorites: []Favorite{
		testFavorite("team-a", OperationRead, "secret/a", "first"),
		testFavorite("team-b", OperationKVGet, "secret/b", "second"),
	}}
	missingProfile := "missing"
	duplicateProfile := "team-a"
	duplicateOperation := OperationRead
	duplicatePath := "secret/a"
	blankPath := "  "
	tests := []struct {
		name     string
		selector string
		changes  FavoriteChanges
		wantErr  string
	}{
		{name: "invalid selector", selector: "0", changes: FavoriteChanges{Note: stringPointer("changed")}, wantErr: "selector"},
		{name: "unknown selector", selector: "3", changes: FavoriteChanges{Note: stringPointer("changed")}, wantErr: "range"},
		{name: "missing profile", selector: "1", changes: FavoriteChanges{Profile: &missingProfile}, wantErr: "profile does not exist"},
		{name: "blank path", selector: "1", changes: FavoriteChanges{Path: &blankPath}, wantErr: "invalid favorite"},
		{
			name:     "duplicate result",
			selector: "2",
			changes: FavoriteChanges{
				Profile:   &duplicateProfile,
				Operation: &duplicateOperation,
				Path:      &duplicatePath,
			},
			wantErr: "already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			favorites := &mutationFavoriteStore{configuration: original}
			service := NewMutationService(favorites, mutationProfiles("team-a", "team-b"))

			err := service.Update(context.Background(), tt.selector, tt.changes)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Update() error = %v, want %q", err, tt.wantErr)
			}
			if !reflect.DeepEqual(favorites.configuration, original) || favorites.saveCalls != 0 {
				t.Fatalf("state changed after failed Update(): configuration = %#v, saves = %d", favorites.configuration, favorites.saveCalls)
			}
		})
	}
}

func TestMutationServiceUpdateWithNoChangesDoesNotSave(t *testing.T) {
	original := Configuration{Favorites: []Favorite{testFavorite("team-a", OperationRead, "secret/a", "note")}}
	favorites := &mutationFavoriteStore{configuration: original}
	service := NewMutationService(favorites, mutationProfiles("team-a"))

	if err := service.Update(context.Background(), "1", FavoriteChanges{}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !reflect.DeepEqual(favorites.configuration, original) || favorites.saveCalls != 0 {
		t.Fatalf("no-op Update() changed state: configuration = %#v, saves = %d", favorites.configuration, favorites.saveCalls)
	}
}

func TestMutationServiceUpdateRejectsOrphanedFavorite(t *testing.T) {
	original := Configuration{Favorites: []Favorite{testFavorite("missing", OperationRead, "secret/a", "note")}}
	favorites := &mutationFavoriteStore{configuration: original}
	service := NewMutationService(favorites, mutationProfiles("team-a"))

	err := service.Update(context.Background(), "1", FavoriteChanges{})
	if err == nil || !strings.Contains(err.Error(), "profile does not exist") {
		t.Fatalf("Update() error = %v, want missing profile rejection", err)
	}
	if favorites.saveCalls != 0 {
		t.Fatalf("Save() calls = %d, want 0", favorites.saveCalls)
	}
}

func TestMutationServiceUpdateRestoresStateAfterPersistenceFailure(t *testing.T) {
	original := Configuration{Favorites: []Favorite{testFavorite("team-a", OperationRead, "secret/a", "original")}}
	favorites := &mutationFavoriteStore{configuration: original, saveErrAt: 1, saveAfterErr: true}
	service := NewMutationService(favorites, mutationProfiles("team-a"))
	note := "updated"

	err := service.Update(context.Background(), "1", FavoriteChanges{Note: &note})
	if err == nil || !strings.Contains(err.Error(), "persist favorite update") {
		t.Fatalf("Update() error = %v, want persistence error", err)
	}
	if !reflect.DeepEqual(favorites.configuration, original) {
		t.Fatalf("configuration after failed Update() = %#v, want %#v", favorites.configuration, original)
	}
}

func TestMutationServiceRemoveUsesSortedNumberAndPreservesUnrelatedFavorites(t *testing.T) {
	zulu := testFavorite("team-a", OperationRead, "secret/z", "z")
	alpha := testFavorite("team-b", OperationRead, "secret/a", "a")
	favorites := &mutationFavoriteStore{configuration: Configuration{Favorites: []Favorite{zulu, alpha}}}
	service := NewMutationService(favorites, mutationProfiles("team-a", "team-b"))

	if err := service.Remove(context.Background(), "1"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !reflect.DeepEqual(favorites.configuration.Favorites, []Favorite{zulu}) {
		t.Fatalf("favorites after Remove() = %#v, want %#v", favorites.configuration.Favorites, []Favorite{zulu})
	}
}

func TestMutationServiceRemoveRejectsInvalidSelectorOrOrphanWithoutChangingState(t *testing.T) {
	tests := []struct {
		name     string
		favorite Favorite
		selector string
		profiles *mutationProfileStore
		wantErr  string
	}{
		{
			name:     "invalid selector",
			favorite: testFavorite("team-a", OperationRead, "secret/a", ""),
			selector: "01",
			profiles: mutationProfiles("team-a"),
			wantErr:  "selector",
		},
		{
			name:     "orphaned favorite",
			favorite: testFavorite("missing", OperationRead, "secret/a", ""),
			selector: "1",
			profiles: mutationProfiles("team-a"),
			wantErr:  "profile does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := Configuration{Favorites: []Favorite{tt.favorite}}
			favorites := &mutationFavoriteStore{configuration: original}
			service := NewMutationService(favorites, tt.profiles)

			err := service.Remove(context.Background(), tt.selector)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Remove() error = %v, want %q", err, tt.wantErr)
			}
			if !reflect.DeepEqual(favorites.configuration, original) || favorites.saveCalls != 0 {
				t.Fatalf("state changed after failed Remove(): configuration = %#v, saves = %d", favorites.configuration, favorites.saveCalls)
			}
		})
	}
}

func TestMutationServiceRemoveRestoresStateAfterPersistenceFailure(t *testing.T) {
	original := Configuration{Favorites: []Favorite{testFavorite("team-a", OperationRead, "secret/a", "original")}}
	favorites := &mutationFavoriteStore{configuration: original, saveErrAt: 1, saveAfterErr: true}
	service := NewMutationService(favorites, mutationProfiles("team-a"))

	err := service.Remove(context.Background(), "1")
	if err == nil || !strings.Contains(err.Error(), "persist favorite removal") {
		t.Fatalf("Remove() error = %v, want persistence error", err)
	}
	if !reflect.DeepEqual(favorites.configuration, original) {
		t.Fatalf("configuration after failed Remove() = %#v, want %#v", favorites.configuration, original)
	}
}

func TestMutationServiceHonorsCanceledContextBeforeLoading(t *testing.T) {
	favorites := &mutationFavoriteStore{}
	profiles := mutationProfiles("team-a")
	service := NewMutationService(favorites, profiles)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "add", run: func() error { return service.Add(ctx, testFavorite("team-a", OperationRead, "secret/a", "")) }},
		{name: "update", run: func() error { return service.Update(ctx, "1", FavoriteChanges{}) }},
		{name: "remove", run: func() error { return service.Remove(ctx, "1") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, context.Canceled) {
				t.Fatalf("operation error = %v, want context.Canceled", err)
			}
		})
	}
	if favorites.loadCalls != 0 || profiles.loadCalls != 0 {
		t.Fatalf("canceled operations loaded state: favorites = %d, profiles = %d", favorites.loadCalls, profiles.loadCalls)
	}
}

func stringPointer(value string) *string {
	return &value
}
