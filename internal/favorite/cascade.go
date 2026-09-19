package favorite

import (
	"context"
	"errors"
	"fmt"

	"vlt/internal/profile"
)

type ProfileRemover interface {
	Remove(context.Context, string) error
}

type LinkedFavoritesError struct {
	Count int
}

func (e LinkedFavoritesError) Error() string {
	return fmt.Sprintf("profile has %d linked favorites and requires cascade approval", e.Count)
}

type CascadeService struct {
	favorites ConfigurationStore
	profiles  ProfileRemover
}

func NewCascadeService(favorites ConfigurationStore, profiles ProfileRemover) *CascadeService {
	return &CascadeService{favorites: favorites, profiles: profiles}
}

func (s *CascadeService) LinkedCount(ctx context.Context, profileName string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := profile.ValidateName(profileName); err != nil {
		return 0, fmt.Errorf("count linked favorites: invalid profile name: %w", err)
	}
	if err := s.validateDependencies(); err != nil {
		return 0, fmt.Errorf("count linked favorites: %w", err)
	}
	configuration, err := s.favorites.Load(ctx)
	if err != nil {
		return 0, fmt.Errorf("count linked favorites: load favorites: %w", err)
	}
	return linkedFavoriteCount(configuration.Favorites, profileName), nil
}

func (s *CascadeService) Remove(ctx context.Context, profileName string, approved bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := profile.ValidateName(profileName); err != nil {
		return fmt.Errorf("remove profile cascade: invalid profile name: %w", err)
	}
	if err := s.validateDependencies(); err != nil {
		return fmt.Errorf("remove profile cascade: %w", err)
	}
	original, err := s.favorites.Load(ctx)
	if err != nil {
		return fmt.Errorf("remove profile cascade: load favorites: %w", err)
	}
	linkedCount := linkedFavoriteCount(original.Favorites, profileName)
	if linkedCount == 0 {
		if err := s.profiles.Remove(ctx, profileName); err != nil {
			return fmt.Errorf("remove profile: %w", err)
		}
		return nil
	}
	if !approved {
		return LinkedFavoritesError{Count: linkedCount}
	}

	updated := Configuration{Favorites: make([]Favorite, 0, len(original.Favorites)-linkedCount)}
	for _, candidate := range original.Favorites {
		if candidate.Profile != profileName {
			updated.Favorites = append(updated.Favorites, candidate)
		}
	}
	if err := s.favorites.Save(ctx, updated); err != nil {
		return cascadePersistenceError(
			fmt.Errorf("persist linked favorite removal: %w", err),
			restoreFavoriteConfiguration(context.WithoutCancel(ctx), s.favorites, original),
		)
	}
	if err := s.profiles.Remove(ctx, profileName); err != nil {
		return cascadePersistenceError(
			fmt.Errorf("remove profile: %w", err),
			restoreFavoriteConfiguration(context.WithoutCancel(ctx), s.favorites, original),
		)
	}
	return nil
}

func (s *CascadeService) validateDependencies() error {
	if s == nil || s.favorites == nil {
		return errors.New("favorite store is not configured")
	}
	if s.profiles == nil {
		return errors.New("profile remover is not configured")
	}
	return nil
}

func linkedFavoriteCount(favorites []Favorite, profileName string) int {
	count := 0
	for _, candidate := range favorites {
		if candidate.Profile == profileName {
			count++
		}
	}
	return count
}

func cascadePersistenceError(primary, restoreErr error) error {
	if restoreErr == nil {
		return primary
	}
	return errors.Join(primary, fmt.Errorf("restore favorite configuration: %w", restoreErr))
}
