package favorite

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"vlt/internal/profile"
)

type ConfigurationStore interface {
	Load(context.Context) (Configuration, error)
	Save(context.Context, Configuration) error
}

type ProfileStore interface {
	Load(context.Context) (profile.Configuration, error)
}

type MutationService struct {
	favorites ConfigurationStore
	profiles  ProfileStore
}

type FavoriteChanges struct {
	Profile   *string
	Operation *string
	Path      *string
	Note      *string
}

func NewMutationService(favorites ConfigurationStore, profiles ProfileStore) *MutationService {
	return &MutationService{favorites: favorites, profiles: profiles}
}

func (s *MutationService) Add(ctx context.Context, candidate Favorite) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("add favorite: invalid favorite: %w", err)
	}
	if err := s.validateDependencies(); err != nil {
		return fmt.Errorf("add favorite: %w", err)
	}
	profiles, err := s.profiles.Load(ctx)
	if err != nil {
		return fmt.Errorf("add favorite: load profiles: %w", err)
	}
	if !profileExists(profiles, candidate.Profile) {
		return fmt.Errorf("add favorite: profile does not exist")
	}
	original, err := s.favorites.Load(ctx)
	if err != nil {
		return fmt.Errorf("add favorite: load favorites: %w", err)
	}
	for _, existing := range original.Favorites {
		if existing.SameIdentity(candidate) {
			return errors.New("add favorite: favorite already exists; use favorite update")
		}
	}

	updated := cloneFavoriteConfiguration(original)
	updated.Favorites = append(updated.Favorites, candidate)
	if err := s.favorites.Save(ctx, updated); err != nil {
		return s.persistenceError(ctx, "add favorite: persist favorite", err, original)
	}
	return nil
}

func (s *MutationService) Update(ctx context.Context, selector string, changes FavoriteChanges) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.validateDependencies(); err != nil {
		return fmt.Errorf("update favorite: %w", err)
	}
	original, err := s.favorites.Load(ctx)
	if err != nil {
		return fmt.Errorf("update favorite: load favorites: %w", err)
	}
	selected, err := NewService(original.Favorites).Resolve(selector)
	if err != nil {
		return fmt.Errorf("update favorite: %w", err)
	}
	selectedIndex := favoriteIndex(original.Favorites, selected)
	if selectedIndex < 0 {
		return errors.New("update favorite: selected favorite is unavailable")
	}

	updatedFavorite := applyFavoriteChanges(selected, changes)
	if err := updatedFavorite.Validate(); err != nil {
		return fmt.Errorf("update favorite: invalid favorite: %w", err)
	}
	profiles, err := s.profiles.Load(ctx)
	if err != nil {
		return fmt.Errorf("update favorite: load profiles: %w", err)
	}
	if !profileExists(profiles, updatedFavorite.Profile) {
		return errors.New("update favorite: profile does not exist")
	}
	for index, existing := range original.Favorites {
		if index != selectedIndex && existing.SameIdentity(updatedFavorite) {
			return errors.New("update favorite: favorite already exists")
		}
	}
	if updatedFavorite == selected {
		return nil
	}

	updated := cloneFavoriteConfiguration(original)
	updated.Favorites[selectedIndex] = updatedFavorite
	if err := s.favorites.Save(ctx, updated); err != nil {
		return s.persistenceError(ctx, "update favorite: persist favorite update", err, original)
	}
	return nil
}

func (s *MutationService) Remove(ctx context.Context, selector string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.validateDependencies(); err != nil {
		return fmt.Errorf("remove favorite: %w", err)
	}
	original, err := s.favorites.Load(ctx)
	if err != nil {
		return fmt.Errorf("remove favorite: load favorites: %w", err)
	}
	selected, err := NewService(original.Favorites).Resolve(selector)
	if err != nil {
		return fmt.Errorf("remove favorite: %w", err)
	}
	selectedIndex := favoriteIndex(original.Favorites, selected)
	if selectedIndex < 0 {
		return errors.New("remove favorite: selected favorite is unavailable")
	}
	profiles, err := s.profiles.Load(ctx)
	if err != nil {
		return fmt.Errorf("remove favorite: load profiles: %w", err)
	}
	if !profileExists(profiles, selected.Profile) {
		return errors.New("remove favorite: profile does not exist")
	}

	updated := cloneFavoriteConfiguration(original)
	updated.Favorites = append(updated.Favorites[:selectedIndex], updated.Favorites[selectedIndex+1:]...)
	if err := s.favorites.Save(ctx, updated); err != nil {
		return s.persistenceError(ctx, "remove favorite: persist favorite removal", err, original)
	}
	return nil
}

func (s *MutationService) validateDependencies() error {
	if s == nil || s.favorites == nil {
		return errors.New("favorite store is not configured")
	}
	if s.profiles == nil {
		return errors.New("profile store is not configured")
	}
	return nil
}

func (s *MutationService) persistenceError(ctx context.Context, operation string, primary error, original Configuration) error {
	restoreErr := restoreFavoriteConfiguration(context.WithoutCancel(ctx), s.favorites, original)
	if restoreErr == nil {
		return fmt.Errorf("%s: %w", operation, primary)
	}
	return errors.Join(
		fmt.Errorf("%s: %w", operation, primary),
		fmt.Errorf("restore favorite configuration: %w", restoreErr),
	)
}

func applyFavoriteChanges(original Favorite, changes FavoriteChanges) Favorite {
	updated := original
	if changes.Profile != nil {
		updated.Profile = *changes.Profile
	}
	if changes.Operation != nil {
		updated.Operation = *changes.Operation
	}
	if changes.Path != nil {
		updated.Path = *changes.Path
	}
	if changes.Note != nil {
		updated.Note = *changes.Note
	}
	return updated
}

func favoriteIndex(favorites []Favorite, selected Favorite) int {
	for index, candidate := range favorites {
		if candidate.SameIdentity(selected) {
			return index
		}
	}
	return -1
}

func profileExists(configuration profile.Configuration, name string) bool {
	for _, candidate := range configuration.Profiles {
		if candidate.Name == name {
			return true
		}
	}
	return false
}

func restoreFavoriteConfiguration(ctx context.Context, store ConfigurationStore, original Configuration) error {
	current, loadErr := store.Load(ctx)
	if loadErr == nil && reflect.DeepEqual(current, original) {
		return nil
	}
	return store.Save(ctx, cloneFavoriteConfiguration(original))
}

func cloneFavoriteConfiguration(configuration Configuration) Configuration {
	configuration.Favorites = append([]Favorite(nil), configuration.Favorites...)
	return configuration
}
