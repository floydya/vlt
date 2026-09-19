package favorite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type cascadeFavoriteStore struct {
	configuration Configuration
	loadErr       error
	loadCalls     int
	saveCalls     int
	saveErrors    map[int]error
	saveAfterErr  map[int]bool
	events        *[]string
}

func (s *cascadeFavoriteStore) Load(context.Context) (Configuration, error) {
	s.loadCalls++
	if s.loadErr != nil {
		return Configuration{}, s.loadErr
	}
	return cloneFavoriteConfiguration(s.configuration), nil
}

func (s *cascadeFavoriteStore) Save(_ context.Context, configuration Configuration) error {
	s.saveCalls++
	if s.events != nil {
		*s.events = append(*s.events, "save favorites")
	}
	err := s.saveErrors[s.saveCalls]
	if err != nil && !s.saveAfterErr[s.saveCalls] {
		return err
	}
	s.configuration = cloneFavoriteConfiguration(configuration)
	return err
}

type cascadeProfileRemover struct {
	names  []string
	err    error
	events *[]string
}

func (r *cascadeProfileRemover) Remove(_ context.Context, name string) error {
	r.names = append(r.names, name)
	if r.events != nil {
		*r.events = append(*r.events, "remove profile")
	}
	return r.err
}

func cascadeTestConfiguration() Configuration {
	return Configuration{Favorites: []Favorite{
		testFavorite("team-a", OperationRead, "secret/a", "first"),
		testFavorite("team-b", OperationRead, "secret/b", "unrelated"),
		testFavorite("team-a", OperationKVGet, "secret/c", "second"),
	}}
}

func TestCascadeServiceLinkedCountIsExactAndCaseSensitive(t *testing.T) {
	store := &cascadeFavoriteStore{configuration: cascadeTestConfiguration()}
	service := NewCascadeService(store, &cascadeProfileRemover{})

	tests := []struct {
		name string
		want int
	}{
		{name: "team-a", want: 2},
		{name: "team-b", want: 1},
		{name: "Team-A", want: 0},
		{name: "missing", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.LinkedCount(context.Background(), tt.name)
			if err != nil {
				t.Fatalf("LinkedCount() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("LinkedCount(%q) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestCascadeServiceRemovesProfileImmediatelyWithoutLinkedFavorites(t *testing.T) {
	original := cascadeTestConfiguration()
	store := &cascadeFavoriteStore{configuration: original}
	profiles := &cascadeProfileRemover{}
	service := NewCascadeService(store, profiles)

	if err := service.Remove(context.Background(), "team-c", false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !reflect.DeepEqual(profiles.names, []string{"team-c"}) {
		t.Fatalf("profile removals = %#v, want team-c", profiles.names)
	}
	if store.saveCalls != 0 || !reflect.DeepEqual(store.configuration, original) {
		t.Fatalf("favorite state changed: saves=%d configuration=%#v", store.saveCalls, store.configuration)
	}
}

func TestCascadeServiceRequiresApprovalForLinkedFavorites(t *testing.T) {
	original := cascadeTestConfiguration()
	store := &cascadeFavoriteStore{configuration: original}
	profiles := &cascadeProfileRemover{}
	service := NewCascadeService(store, profiles)

	err := service.Remove(context.Background(), "team-a", false)
	var approvalErr LinkedFavoritesError
	if !errors.As(err, &approvalErr) {
		t.Fatalf("Remove() error = %v, want LinkedFavoritesError", err)
	}
	if approvalErr.Count != 2 {
		t.Fatalf("linked count = %d, want 2", approvalErr.Count)
	}
	if store.saveCalls != 0 || len(profiles.names) != 0 || !reflect.DeepEqual(store.configuration, original) {
		t.Fatalf("unapproved removal changed state: saves=%d profiles=%#v configuration=%#v", store.saveCalls, profiles.names, store.configuration)
	}
}

func TestCascadeServiceApprovedRemovalDeletesLinkedFavoritesBeforeProfile(t *testing.T) {
	original := cascadeTestConfiguration()
	events := []string{}
	store := &cascadeFavoriteStore{configuration: original, events: &events}
	profiles := &cascadeProfileRemover{events: &events}
	service := NewCascadeService(store, profiles)

	if err := service.Remove(context.Background(), "team-a", true); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	wantFavorites := []Favorite{original.Favorites[1]}
	if !reflect.DeepEqual(store.configuration.Favorites, wantFavorites) {
		t.Fatalf("favorites after cascade = %#v, want %#v", store.configuration.Favorites, wantFavorites)
	}
	if !reflect.DeepEqual(profiles.names, []string{"team-a"}) {
		t.Fatalf("profile removals = %#v, want team-a", profiles.names)
	}
	if !reflect.DeepEqual(events, []string{"save favorites", "remove profile"}) {
		t.Fatalf("cascade events = %#v, want favorite persistence before profile removal", events)
	}
}

func TestCascadeServiceFavoritePersistenceFailurePreventsProfileRemoval(t *testing.T) {
	for _, saveAfterErr := range []bool{false, true} {
		name := "before replacement"
		if saveAfterErr {
			name = "after replacement"
		}
		t.Run(name, func(t *testing.T) {
			original := cascadeTestConfiguration()
			store := &cascadeFavoriteStore{
				configuration: original,
				saveErrors:    map[int]error{1: errors.New("favorite persistence failed")},
				saveAfterErr:  map[int]bool{1: saveAfterErr},
			}
			profiles := &cascadeProfileRemover{}
			service := NewCascadeService(store, profiles)

			err := service.Remove(context.Background(), "team-a", true)
			if err == nil || !strings.Contains(err.Error(), "persist linked favorite removal") {
				t.Fatalf("Remove() error = %v, want favorite persistence error", err)
			}
			if !reflect.DeepEqual(store.configuration, original) {
				t.Fatalf("favorites after failed persistence = %#v, want %#v", store.configuration, original)
			}
			if len(profiles.names) != 0 {
				t.Fatalf("profile removals = %#v, want none", profiles.names)
			}
		})
	}
}

func TestCascadeServiceProfileFailureRestoresFavorites(t *testing.T) {
	original := cascadeTestConfiguration()
	store := &cascadeFavoriteStore{configuration: original}
	profiles := &cascadeProfileRemover{err: errors.New("profile removal failed")}
	service := NewCascadeService(store, profiles)

	err := service.Remove(context.Background(), "team-a", true)
	if err == nil || !strings.Contains(err.Error(), "remove profile") {
		t.Fatalf("Remove() error = %v, want profile removal error", err)
	}
	if !reflect.DeepEqual(store.configuration, original) {
		t.Fatalf("favorites after profile failure = %#v, want %#v", store.configuration, original)
	}
	if store.saveCalls != 2 {
		t.Fatalf("favorite saves = %d, want removal and restoration", store.saveCalls)
	}
}

func TestCascadeServiceReportsPrimaryAndRollbackFailures(t *testing.T) {
	original := cascadeTestConfiguration()
	store := &cascadeFavoriteStore{
		configuration: original,
		saveErrors: map[int]error{
			2: errors.New("favorite rollback failed"),
		},
	}
	profiles := &cascadeProfileRemover{err: errors.New("profile removal failed")}
	service := NewCascadeService(store, profiles)

	err := service.Remove(context.Background(), "team-a", true)
	if err == nil {
		t.Fatal("Remove() error = nil, want joined failure")
	}
	for _, text := range []string{"profile removal failed", "restore favorite configuration", "favorite rollback failed"} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("Remove() error = %q, want %q", err, text)
		}
	}
	if len(store.configuration.Favorites) != 1 {
		t.Fatalf("failed rollback silently reported original state: %#v", store.configuration.Favorites)
	}
}

func TestCascadeServiceHonorsCanceledContextBeforeLoading(t *testing.T) {
	store := &cascadeFavoriteStore{}
	profiles := &cascadeProfileRemover{}
	service := NewCascadeService(store, profiles)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Remove(ctx, "team-a", true); !errors.Is(err, context.Canceled) {
		t.Fatalf("Remove() error = %v, want context.Canceled", err)
	}
	if store.loadCalls != 0 || len(profiles.names) != 0 {
		t.Fatalf("canceled cascade used dependencies: loads=%d profiles=%#v", store.loadCalls, profiles.names)
	}
}
