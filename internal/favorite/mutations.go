package favorite

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	_, err := s.AddWithResult(ctx, candidate)
	return err
}

func (s *MutationService) AddWithResult(ctx context.Context, candidate Favorite) (Favorite, error) {
	if err := ctx.Err(); err != nil {
		return Favorite{}, err
	}
	if err := candidate.Validate(); err != nil {
		return Favorite{}, fmt.Errorf("add favorite: invalid favorite: %w", err)
	}
	if err := s.validateDependencies(); err != nil {
		return Favorite{}, fmt.Errorf("add favorite: %w", err)
	}
	profiles, err := s.profiles.Load(ctx)
	if err != nil {
		return Favorite{}, fmt.Errorf("add favorite: load profiles: %w", err)
	}
	if !profileExists(profiles, candidate.Profile) {
		return Favorite{}, fmt.Errorf("add favorite: profile does not exist")
	}
	original, err := s.favorites.Load(ctx)
	if err != nil {
		return Favorite{}, fmt.Errorf("add favorite: load favorites: %w", err)
	}
	for _, existing := range original.Favorites {
		if existing.SameIdentity(candidate) {
			return Favorite{}, errors.New("add favorite: favorite already exists; use favorite update")
		}
	}
	if candidate.ID == "" {
		used := make(map[string]struct{}, len(original.Favorites))
		for _, existing := range original.Favorites {
			used[existing.ID] = struct{}{}
		}
		for {
			candidate.ID, err = newRandomID()
			if err != nil {
				return Favorite{}, fmt.Errorf("add favorite: %w", err)
			}
			if _, found := used[candidate.ID]; !found {
				break
			}
		}
	} else {
		for _, existing := range original.Favorites {
			if existing.ID == candidate.ID {
				return Favorite{}, errors.New("add favorite: favorite ID already exists")
			}
		}
	}

	updated := cloneFavoriteConfiguration(original)
	updated.Favorites = append(updated.Favorites, candidate)
	if err := s.favorites.Save(ctx, updated); err != nil {
		return Favorite{}, s.persistenceError(ctx, "add favorite: persist favorite", err, original)
	}
	return candidate, nil
}

func (s *MutationService) Update(ctx context.Context, selector string, changes FavoriteChanges) error {
	_, err := s.UpdateWithResult(ctx, selector, changes, nil)
	return err
}

func (s *MutationService) UpdateWithResult(ctx context.Context, selector string, changes FavoriteChanges, expected *Favorite) (Favorite, error) {
	if err := ctx.Err(); err != nil {
		return Favorite{}, err
	}
	if err := s.validateDependencies(); err != nil {
		return Favorite{}, fmt.Errorf("update favorite: %w", err)
	}
	original, err := s.favorites.Load(ctx)
	if err != nil {
		return Favorite{}, fmt.Errorf("update favorite: load favorites: %w", err)
	}
	selected, err := NewService(original.Favorites).Resolve(selector)
	if err != nil {
		return Favorite{}, fmt.Errorf("update favorite: %w", err)
	}
	selectedIndex := favoriteIndex(original.Favorites, selected)
	if selectedIndex < 0 {
		return Favorite{}, errors.New("update favorite: selected favorite is unavailable")
	}
	if expected != nil && !sameReviewedFavorite(selected, *expected) {
		return Favorite{}, errors.New("update favorite: favorite changed; select it again")
	}

	updatedFavorite := applyFavoriteChanges(selected, changes)
	if err := updatedFavorite.Validate(); err != nil {
		return Favorite{}, fmt.Errorf("update favorite: invalid favorite: %w", err)
	}
	profiles, err := s.profiles.Load(ctx)
	if err != nil {
		return Favorite{}, fmt.Errorf("update favorite: load profiles: %w", err)
	}
	if !profileExists(profiles, updatedFavorite.Profile) {
		return Favorite{}, errors.New("update favorite: profile does not exist")
	}
	for index, existing := range original.Favorites {
		if index != selectedIndex && existing.SameIdentity(updatedFavorite) {
			return Favorite{}, errors.New("update favorite: favorite already exists")
		}
	}
	if updatedFavorite == selected {
		return selected, nil
	}

	updated := cloneFavoriteConfiguration(original)
	updated.Favorites[selectedIndex] = updatedFavorite
	if err := s.favorites.Save(ctx, updated); err != nil {
		return Favorite{}, s.persistenceError(ctx, "update favorite: persist favorite update", err, original)
	}
	return updatedFavorite, nil
}

func (s *MutationService) Remove(ctx context.Context, selector string) error {
	_, err := s.RemoveWithResult(ctx, selector, nil)
	return err
}

func (s *MutationService) RemoveWithResult(ctx context.Context, selector string, expected *Favorite) (Favorite, error) {
	if err := ctx.Err(); err != nil {
		return Favorite{}, err
	}
	if err := s.validateDependencies(); err != nil {
		return Favorite{}, fmt.Errorf("remove favorite: %w", err)
	}
	original, err := s.favorites.Load(ctx)
	if err != nil {
		return Favorite{}, fmt.Errorf("remove favorite: load favorites: %w", err)
	}
	selected, err := NewService(original.Favorites).Resolve(selector)
	if err != nil {
		return Favorite{}, fmt.Errorf("remove favorite: %w", err)
	}
	selectedIndex := favoriteIndex(original.Favorites, selected)
	if selectedIndex < 0 {
		return Favorite{}, errors.New("remove favorite: selected favorite is unavailable")
	}
	if expected != nil && !sameReviewedFavorite(selected, *expected) {
		return Favorite{}, errors.New("remove favorite: favorite changed; select it again")
	}
	profiles, err := s.profiles.Load(ctx)
	if err != nil {
		return Favorite{}, fmt.Errorf("remove favorite: load profiles: %w", err)
	}
	if !profileExists(profiles, selected.Profile) {
		return Favorite{}, errors.New("remove favorite: profile does not exist")
	}

	updated := cloneFavoriteConfiguration(original)
	updated.Favorites = append(updated.Favorites[:selectedIndex], updated.Favorites[selectedIndex+1:]...)
	if err := s.favorites.Save(ctx, updated); err != nil {
		return Favorite{}, s.persistenceError(ctx, "remove favorite: persist favorite removal", err, original)
	}
	return selected, nil
}

func sameReviewedFavorite(current, reviewed Favorite) bool {
	return current.ID == reviewed.ID && current.Profile == reviewed.Profile &&
		current.Operation == reviewed.Operation && current.Path == reviewed.Path && current.Note == reviewed.Note
}

func (s *MutationService) RecordUse(ctx context.Context, selected Favorite) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.favorites == nil {
		return errors.New("record favorite use: favorite store is not configured")
	}
	original, err := s.favorites.Load(ctx)
	if err != nil {
		return fmt.Errorf("record favorite use: load favorites: %w", err)
	}
	index := favoriteIndex(original.Favorites, selected)
	if index < 0 || original.Favorites[index].RunCount == math.MaxInt64 {
		return nil
	}
	updated := cloneFavoriteConfiguration(original)
	updated.Favorites[index].RunCount++
	if err := s.favorites.Save(ctx, updated); err != nil {
		return s.persistenceError(ctx, "record favorite use: persist count", err, original)
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
	if !updated.SameIdentity(original) {
		updated.RunCount = 0
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
