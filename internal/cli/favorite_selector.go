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
}

func NewSharedFavoriteSelector(selector SharedSelector) FavoriteSelector {
	return sharedFavoriteSelector{selector: selector}
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

	items := make([]SharedSelectorItem, 0, len(ordered))
	identities := make(map[string]favorite.Favorite, len(ordered))
	for index, candidate := range ordered {
		identifier := fmt.Sprintf("favorite-%06d", index+1)
		note := candidate.Note
		if note == "" {
			note = "-"
		}
		label := fmt.Sprintf(
			"%d  %s  %s  %s  %s  %s",
			index+1,
			strconv.FormatInt(candidate.RunCount, 10),
			sanitizeFavoriteDisplay(candidate.Operation),
			sanitizeFavoriteDisplay(candidate.Profile),
			sanitizeFavoriteDisplay(candidate.Path),
			sanitizeFavoriteDisplay(note),
		)
		items = append(items, SharedSelectorItem{ID: identifier, Label: label, SearchText: label})
		identities[identifier] = candidate
	}

	selectedID, err := s.selector.Select(ctx, "Select a favorite", items, "")
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
