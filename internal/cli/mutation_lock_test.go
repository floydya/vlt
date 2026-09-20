package cli

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"vlt/internal/config"
	"vlt/internal/favorite"
	"vlt/internal/profile"
	"vlt/internal/statelock"
)

type blockingFavoriteStore struct {
	store   *favorite.Store
	loaded  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingFavoriteStore) Load(ctx context.Context) (favorite.Configuration, error) {
	configuration, err := s.store.Load(ctx)
	if err != nil {
		return favorite.Configuration{}, err
	}
	s.once.Do(func() { close(s.loaded) })
	select {
	case <-ctx.Done():
		return favorite.Configuration{}, ctx.Err()
	case <-s.release:
		return configuration, nil
	}
}

func (s *blockingFavoriteStore) Save(ctx context.Context, configuration favorite.Configuration) error {
	return s.store.Save(ctx, configuration)
}

type observedFavoriteStore struct {
	store  *favorite.Store
	loaded chan struct{}
	once   sync.Once
}

type blockingProfileStore struct {
	store   *config.Store
	loaded  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingProfileStore) Load(ctx context.Context) (profile.Configuration, error) {
	configuration, err := s.store.Load(ctx)
	if err != nil {
		return profile.Configuration{}, err
	}
	s.once.Do(func() { close(s.loaded) })
	select {
	case <-ctx.Done():
		return profile.Configuration{}, ctx.Err()
	case <-s.release:
		return configuration, nil
	}
}

func (s *blockingProfileStore) Save(ctx context.Context, configuration profile.Configuration) error {
	return s.store.Save(ctx, configuration)
}

type observedProfileStore struct {
	store  *config.Store
	loaded chan struct{}
	once   sync.Once
}

func (s *observedProfileStore) Load(ctx context.Context) (profile.Configuration, error) {
	s.once.Do(func() { close(s.loaded) })
	return s.store.Load(ctx)
}

func (s *observedProfileStore) Save(ctx context.Context, configuration profile.Configuration) error {
	return s.store.Save(ctx, configuration)
}

type mutationLockCredentialStore struct{}

func (mutationLockCredentialStore) Get(context.Context, string) (string, error) {
	return "", profile.ErrCredentialNotFound
}

func (mutationLockCredentialStore) Set(context.Context, string, string) error {
	return nil
}

func (mutationLockCredentialStore) Delete(context.Context, string) error {
	return nil
}

type mutationLockAuthenticator struct{}

func (mutationLockAuthenticator) Login(context.Context, profile.Profile) error {
	return nil
}

type rejectingMutationLock struct {
	calls int
}

func (l *rejectingMutationLock) WithLock(context.Context, func(context.Context) error) error {
	l.calls++
	return errors.New("synthetic lock failure")
}

type blockingActiveProfileStore struct {
	store   *config.Store
	loaded  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingActiveProfileStore) Load(ctx context.Context) (profile.Configuration, error) {
	return s.store.Load(ctx)
}

func (s *blockingActiveProfileStore) SetActiveProfile(ctx context.Context, selector string) error {
	configuration, err := s.store.Load(ctx)
	if err != nil {
		return err
	}
	s.once.Do(func() { close(s.loaded) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.release:
	}
	selected, err := profile.NewService(configuration.Profiles).Resolve(selector)
	if err != nil {
		return err
	}
	configuration.ActiveProfile = selected.Name
	return s.store.Save(ctx, configuration)
}

func (s *observedFavoriteStore) Load(ctx context.Context) (favorite.Configuration, error) {
	s.once.Do(func() { close(s.loaded) })
	return s.store.Load(ctx)
}

func (s *observedFavoriteStore) Save(ctx context.Context, configuration favorite.Configuration) error {
	return s.store.Save(ctx, configuration)
}

func TestMutationLockPreventsConcurrentFavoriteUpdatesFromLosingData(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vlt")
	profilePath := filepath.Join(directory, "profiles.json")
	favoritePath := filepath.Join(directory, "favorites.json")
	profiles := config.NewStore(profilePath)
	if err := profiles.Save(context.Background(), config.Configuration{Profiles: []profile.Profile{{
		Name: "team-a", Address: "https://vault.example.com", Username: "user", AuthPath: "oidc",
	}}}); err != nil {
		t.Fatalf("seed profiles: %v", err)
	}

	firstLoaded := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondLoaded := make(chan struct{})
	firstStore := &blockingFavoriteStore{
		store: favorite.NewStore(favoritePath), loaded: firstLoaded, release: releaseFirst,
	}
	secondStore := &observedFavoriteStore{store: favorite.NewStore(favoritePath), loaded: secondLoaded}
	firstHandler := NewFavoriteHandler(FavoriteDependencies{
		Mutations: favorite.NewMutationService(firstStore, config.NewStore(profilePath)),
		Output:    io.Discard,
		Lock:      statelock.New(directory),
	})
	secondHandler := NewFavoriteHandler(FavoriteDependencies{
		Mutations: favorite.NewMutationService(secondStore, config.NewStore(profilePath)),
		Output:    io.Discard,
		Lock:      statelock.New(directory),
	})

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- firstHandler(context.Background(), []string{
			"add", "secret/first", "--profile", "team-a", "--operation", "read",
		})
	}()
	<-firstLoaded

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- secondHandler(context.Background(), []string{
			"add", "secret/second", "--profile", "team-a", "--operation", "read",
		})
	}()
	select {
	case <-secondLoaded:
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)

	if err := <-firstDone; err != nil {
		t.Fatalf("first favorite add error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second favorite add error = %v", err)
	}
	configuration, err := favorite.NewStore(favoritePath).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(configuration.Favorites) != 2 {
		t.Fatalf("favorite count = %d, want 2: %#v", len(configuration.Favorites), configuration.Favorites)
	}
}

func TestMutationLockPreventsConcurrentProfileUpdatesFromLosingData(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vlt")
	profilePath := filepath.Join(directory, "profiles.json")
	firstLoaded := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondLoaded := make(chan struct{})
	firstStore := &blockingProfileStore{
		store: config.NewStore(profilePath), loaded: firstLoaded, release: releaseFirst,
	}
	secondStore := &observedProfileStore{store: config.NewStore(profilePath), loaded: secondLoaded}
	credentials := mutationLockCredentialStore{}
	authenticator := mutationLockAuthenticator{}
	firstHandler := NewProfileHandler(ProfileDependencies{
		Mutations: profile.NewMutationService(firstStore, credentials, authenticator),
		Output:    io.Discard,
		Lock:      statelock.New(directory),
	})
	secondHandler := NewProfileHandler(ProfileDependencies{
		Mutations: profile.NewMutationService(secondStore, credentials, authenticator),
		Output:    io.Discard,
		Lock:      statelock.New(directory),
	})

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- firstHandler(context.Background(), []string{
			"add", "team-a", "--address", "https://a.example.com", "--username", "user-a",
		})
	}()
	<-firstLoaded
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- secondHandler(context.Background(), []string{
			"add", "team-b", "--address", "https://b.example.com", "--username", "user-b",
		})
	}()
	select {
	case <-secondLoaded:
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)

	if err := <-firstDone; err != nil {
		t.Fatalf("first profile add error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second profile add error = %v", err)
	}
	configuration, err := config.NewStore(profilePath).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(configuration.Profiles) != 2 {
		t.Fatalf("profile count = %d, want 2: %#v", len(configuration.Profiles), configuration.Profiles)
	}
}

func TestMutationLockPreventsSwitchFromOverwritingConcurrentProfileAdd(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vlt")
	profilePath := filepath.Join(directory, "profiles.json")
	store := config.NewStore(profilePath)
	if err := store.Save(context.Background(), profile.Configuration{
		Profiles: []profile.Profile{
			{Name: "team-a", Address: "https://a.example.com", Username: "user-a", AuthPath: "oidc"},
			{Name: "team-b", Address: "https://b.example.com", Username: "user-b", AuthPath: "oidc"},
		},
		ActiveProfile: "team-a",
	}); err != nil {
		t.Fatalf("seed profiles: %v", err)
	}

	switchLoaded := make(chan struct{})
	releaseSwitch := make(chan struct{})
	addLoaded := make(chan struct{})
	switchHandler := NewSwitchHandler(SwitchDependencies{
		Profiles: &blockingActiveProfileStore{
			store: config.NewStore(profilePath), loaded: switchLoaded, release: releaseSwitch,
		},
		Output: io.Discard,
		Lock:   statelock.New(directory),
	})
	addStore := &observedProfileStore{store: config.NewStore(profilePath), loaded: addLoaded}
	addHandler := NewProfileHandler(ProfileDependencies{
		Mutations: profile.NewMutationService(addStore, mutationLockCredentialStore{}, mutationLockAuthenticator{}),
		Output:    io.Discard,
		Lock:      statelock.New(directory),
	})

	switchDone := make(chan error, 1)
	go func() { switchDone <- switchHandler(context.Background(), []string{"team-b"}) }()
	<-switchLoaded
	addDone := make(chan error, 1)
	go func() {
		addDone <- addHandler(context.Background(), []string{
			"add", "team-c", "--address", "https://c.example.com", "--username", "user-c",
		})
	}()
	select {
	case <-addLoaded:
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseSwitch)

	if err := <-switchDone; err != nil {
		t.Fatalf("switch error = %v", err)
	}
	if err := <-addDone; err != nil {
		t.Fatalf("profile add error = %v", err)
	}
	configuration, err := config.NewStore(profilePath).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(configuration.Profiles) != 3 {
		t.Fatalf("profile count = %d, want 3: %#v", len(configuration.Profiles), configuration.Profiles)
	}
	if configuration.ActiveProfile != "team-b" {
		t.Fatalf("active profile = %q, want team-b", configuration.ActiveProfile)
	}
}

func TestEveryMutationRouteUsesOneLockBoundary(t *testing.T) {
	tests := []struct {
		name string
		run  func(*rejectingMutationLock) error
	}{
		{
			name: "profile add",
			run: func(lock *rejectingMutationLock) error {
				return NewProfileHandler(ProfileDependencies{
					Mutations: &fakeProfileMutator{}, Lock: lock, Output: io.Discard,
				})(context.Background(), []string{
					"add", "team-a", "--address", "https://vault.example.com", "--username", "user",
				})
			},
		},
		{
			name: "profile update",
			run: func(lock *rejectingMutationLock) error {
				return NewProfileHandler(ProfileDependencies{
					Mutations: &fakeProfileMutator{}, Lock: lock, Output: io.Discard,
				})(context.Background(), []string{"update", "team-a", "--namespace", "engineering"})
			},
		},
		{
			name: "profile remove",
			run: func(lock *rejectingMutationLock) error {
				return NewProfileHandler(ProfileDependencies{
					Mutations: &fakeProfileMutator{}, Lock: lock, Output: io.Discard,
				})(context.Background(), []string{"remove", "team-a"})
			},
		},
		{
			name: "profile cascade",
			run: func(lock *rejectingMutationLock) error {
				return NewProfileHandler(ProfileDependencies{
					Cascade: &fakeProfileCascade{}, Lock: lock, Output: io.Discard,
				})(context.Background(), []string{"remove", "team-a"})
			},
		},
		{
			name: "active selection",
			run: func(lock *rejectingMutationLock) error {
				return NewSwitchHandler(SwitchDependencies{
					Profiles: &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{{Name: "team-a"}}}},
					Lock:     lock, Output: io.Discard,
				})(context.Background(), []string{"team-a"})
			},
		},
		{
			name: "favorite add",
			run: func(lock *rejectingMutationLock) error {
				return NewFavoriteHandler(FavoriteDependencies{
					Mutations: &recordingFavoriteMutator{}, Lock: lock, Output: io.Discard,
				})(context.Background(), []string{
					"add", "secret/a", "--profile", "team-a", "--operation", "read",
				})
			},
		},
		{
			name: "favorite update",
			run: func(lock *rejectingMutationLock) error {
				return NewFavoriteHandler(FavoriteDependencies{
					Mutations: &recordingFavoriteMutator{}, Lock: lock, Output: io.Discard,
				})(context.Background(), []string{"update", "1", "--note", "updated"})
			},
		},
		{
			name: "favorite remove",
			run: func(lock *rejectingMutationLock) error {
				return NewFavoriteHandler(FavoriteDependencies{
					Mutations: &recordingFavoriteMutator{}, Lock: lock, Output: io.Discard,
				})(context.Background(), []string{"remove", "1"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock := &rejectingMutationLock{}
			if err := tt.run(lock); err == nil {
				t.Fatal("handler error = nil, want synthetic lock failure")
			}
			if lock.calls != 1 {
				t.Fatalf("lock calls = %d, want 1", lock.calls)
			}
		})
	}
}

func TestReadOnlyRoutesDoNotAcquireMutationLock(t *testing.T) {
	lock := &rejectingMutationLock{}
	profileHandler := NewProfileHandler(ProfileDependencies{
		Profiles: &fakeProfileStore{}, Lock: lock, Output: io.Discard,
	})
	if err := profileHandler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("profile list error = %v", err)
	}
	favoriteHandler := NewFavoriteHandler(FavoriteDependencies{
		Favorites: &fakeFavoriteStore{}, Lock: lock, Output: io.Discard,
	})
	if err := favoriteHandler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("favorite list error = %v", err)
	}
	if lock.calls != 0 {
		t.Fatalf("read-only lock calls = %d, want 0", lock.calls)
	}
}
