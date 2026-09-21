package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"vlt/internal/favorite"
)

var ErrFavoriteSelectionCanceled = errors.New("favorite selection canceled")

type FavoriteSelector interface {
	Select(context.Context, []favorite.Favorite) (favorite.Favorite, error)
}

type sharedFavoriteSelector struct {
	selector SharedSelector
	profiles ConfigurationLoader
}

func NewSharedFavoriteSelector(selector SharedSelector, profiles ConfigurationLoader) FavoriteSelector {
	return sharedFavoriteSelector{selector: selector, profiles: profiles}
}

func (s sharedFavoriteSelector) Select(ctx context.Context, favorites []favorite.Favorite) (favorite.Favorite, error) {
	if ctx == nil {
		return favorite.Favorite{}, errors.New("select favorite: context is not configured")
	}
	if err := ctx.Err(); err != nil {
		return favorite.Favorite{}, err
	}
	ordered := favorite.NewService(favorites).List()
	if len(ordered) == 0 {
		return favorite.Favorite{}, errors.New("select favorite: no favorites configured; add one with 'vlt favorite add'")
	}
	if s.selector == nil {
		return favorite.Favorite{}, errors.New("select favorite: shared selector is not configured")
	}
	colors, err := loadProfileColors(ctx, s.profiles)
	if err != nil {
		return favorite.Favorite{}, fmt.Errorf("select favorite: %w", err)
	}

	items := make([]SharedSelectorItem, 0, len(ordered))
	identities := make(map[string]favorite.Favorite, len(ordered))
	for index, candidate := range ordered {
		identifier := candidate.ID
		if identifier == "" {
			identifier = fmt.Sprintf("favorite-%06d", index+1)
		}
		note := candidate.Note
		if note == "" {
			note = "-"
		}
		label := fmt.Sprintf("%d  %s  (%s runs)", index+1, sanitizeFavoriteDisplay(candidate.Path), strconv.FormatInt(candidate.RunCount, 10))
		detail := fmt.Sprintf("ID: %s\nProfile: %s\nOperation: %s\nNote: %s", identifier, sanitizeFavoriteDisplay(candidate.Profile), sanitizeFavoriteDisplay(candidate.Operation), sanitizeFavoriteDisplay(note))
		search := fmt.Sprintf("%d  %d  %s  %s  %s  %s  %s", index+1, candidate.RunCount, candidate.Operation, candidate.Profile, candidate.Path, note, identifier)
		items = append(items, SharedSelectorItem{ID: identifier, Label: label, Detail: detail, SearchText: search, Color: colors[candidate.Profile]})
		identities[identifier] = candidate
	}
	selectedID, err := s.selector.Select(ctx, "Select a favorite", "#  PATH  RUNS", items, "")
	if err != nil {
		if errors.Is(err, ErrSharedSelectorCanceled) {
			return favorite.Favorite{}, ErrFavoriteSelectionCanceled
		}
		return favorite.Favorite{}, err
	}
	selected, found := identities[selectedID]
	if !found {
		return favorite.Favorite{}, errors.New("select favorite: shared selector returned an unknown selection")
	}
	return selected, nil
}

func loadProfileColors(ctx context.Context, loader ConfigurationLoader) (map[string]string, error) {
	colors := make(map[string]string)
	if loader == nil {
		return colors, nil
	}
	configuration, err := loader.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load profiles: %w", err)
	}
	for _, candidate := range configuration.Profiles {
		colors[candidate.Name] = candidate.Color
	}
	return colors, nil
}
