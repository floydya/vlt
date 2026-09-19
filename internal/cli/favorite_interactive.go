package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"charm.land/huh/v2"

	"vlt/internal/favorite"
	"vlt/internal/profile"
)

var errNoFavoritesConfigured = errors.New("no favorites configured")

type FavoriteManagementSelector interface {
	Select(context.Context, []favorite.Favorite) (favorite.Favorite, error)
}

type FavoriteFormRequest struct {
	Favorite favorite.Favorite
	Profiles []profile.Profile
}

type FavoriteForm interface {
	Run(context.Context, FavoriteFormRequest) (favorite.Favorite, error)
}

type FavoriteRemovalConfirmation struct {
	Favorite favorite.Favorite
}

type FavoriteRemovalConfirmer interface {
	Confirm(context.Context, FavoriteRemovalConfirmation) (bool, error)
}

type huhFavoriteManagementSelector struct {
	input      io.Reader
	output     io.Writer
	accessible bool
}

type huhFavoriteForm struct {
	input      io.Reader
	output     io.Writer
	accessible bool
}

type huhFavoriteRemovalConfirmer struct {
	input      io.Reader
	output     io.Writer
	accessible bool
}

func NewHuhFavoriteManagementSelector(input io.Reader, output io.Writer) FavoriteManagementSelector {
	return huhFavoriteManagementSelector{input: input, output: output}
}

func NewHuhFavoriteForm(input io.Reader, output io.Writer) FavoriteForm {
	return huhFavoriteForm{input: input, output: output}
}

func NewHuhFavoriteRemovalConfirmer(input io.Reader, output io.Writer) FavoriteRemovalConfirmer {
	return huhFavoriteRemovalConfirmer{input: input, output: output}
}

func (s huhFavoriteManagementSelector) Select(ctx context.Context, candidates []favorite.Favorite) (favorite.Favorite, error) {
	if len(candidates) == 0 {
		return favorite.Favorite{}, errors.New("select favorite: no favorite choices")
	}
	if s.input == nil {
		return favorite.Favorite{}, errors.New("select favorite: input is not configured")
	}
	if s.output == nil {
		return favorite.Favorite{}, errors.New("select favorite: output is not configured")
	}
	selected := candidates[0]
	options := make([]huh.Option[favorite.Favorite], 0, len(candidates))
	for index, candidate := range candidates {
		label := fmt.Sprintf(
			"%d  %s  %s  %s  %s",
			index+1,
			sanitizeFavoriteDisplay(candidate.Operation),
			sanitizeFavoriteDisplay(candidate.Profile),
			sanitizeFavoriteDisplay(candidate.Path),
			sanitizeFavoriteDisplay(favoriteNote(candidate.Note)),
		)
		options = append(options, huh.NewOption(label, candidate))
	}
	field := huh.NewSelect[favorite.Favorite]().
		Title("Select a favorite").
		Options(options...).
		Value(&selected)
	form := huh.NewForm(huh.NewGroup(field)).
		WithInput(s.input).
		WithOutput(s.output).
		WithAccessible(s.accessible)
	if err := form.RunWithContext(ctx); err != nil {
		return favorite.Favorite{}, fmt.Errorf("select favorite: %w", err)
	}
	return selected, nil
}

func (f huhFavoriteForm) Run(ctx context.Context, request FavoriteFormRequest) (favorite.Favorite, error) {
	if f.input == nil {
		return favorite.Favorite{}, errors.New("favorite form: input is not configured")
	}
	if f.output == nil {
		return favorite.Favorite{}, errors.New("favorite form: output is not configured")
	}
	if len(request.Profiles) == 0 {
		return favorite.Favorite{}, errors.New("favorite form: no profile choices")
	}
	candidate := request.Favorite
	if candidate.Profile == "" {
		candidate.Profile = request.Profiles[0].Name
	}
	if candidate.Operation == "" {
		candidate.Operation = favorite.OperationRead
	}
	profileOptions := make([]huh.Option[string], 0, len(request.Profiles))
	for _, candidateProfile := range request.Profiles {
		profileOptions = append(profileOptions, huh.NewOption(candidateProfile.Name, candidateProfile.Name))
	}
	operationOptions := []huh.Option[string]{
		huh.NewOption("read", favorite.OperationRead),
		huh.NewOption("kv-get", favorite.OperationKVGet),
	}
	pathValidator := func(value string) error {
		probe := candidate
		probe.Path = value
		return probe.Validate()
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Profile").Options(profileOptions...).Value(&candidate.Profile),
		huh.NewSelect[string]().Title("Operation").Options(operationOptions...).Value(&candidate.Operation),
		huh.NewInput().Title("Path").Value(&candidate.Path).Validate(pathValidator),
		huh.NewInput().Title("Note").Value(&candidate.Note),
	).Title("Favorite details")).
		WithInput(f.input).
		WithOutput(f.output).
		WithAccessible(f.accessible)
	if err := form.RunWithContext(ctx); err != nil {
		return favorite.Favorite{}, fmt.Errorf("favorite form: %w", err)
	}
	if err := candidate.Validate(); err != nil {
		return favorite.Favorite{}, fmt.Errorf("favorite form: validate result: %w", err)
	}
	return candidate, nil
}

func (c huhFavoriteRemovalConfirmer) Confirm(ctx context.Context, request FavoriteRemovalConfirmation) (bool, error) {
	if c.input == nil {
		return false, errors.New("confirm favorite removal: input is not configured")
	}
	if c.output == nil {
		return false, errors.New("confirm favorite removal: output is not configured")
	}
	confirmed := false
	title := fmt.Sprintf(
		"Remove %s favorite %q for profile %q?",
		sanitizeFavoriteDisplay(request.Favorite.Operation),
		sanitizeFavoriteDisplay(request.Favorite.Path),
		sanitizeFavoriteDisplay(request.Favorite.Profile),
	)
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Affirmative("Remove").Negative("Keep").Value(&confirmed),
	)).WithInput(c.input).WithOutput(c.output).WithAccessible(c.accessible)
	if err := form.RunWithContext(ctx); err != nil {
		return false, fmt.Errorf("confirm favorite removal: %w", err)
	}
	return confirmed, nil
}

type favoriteSelection struct {
	favorite favorite.Favorite
	selector string
}

func selectFavoriteForManagement(ctx context.Context, dependencies FavoriteDependencies, selector string) (favoriteSelection, error) {
	if dependencies.Favorites == nil {
		return favoriteSelection{}, errors.New("favorite selection: favorite configuration is not configured")
	}
	configuration, err := dependencies.Favorites.Load(ctx)
	if err != nil {
		return favoriteSelection{}, fmt.Errorf("favorite selection: load favorites: %w", err)
	}
	ordered := favorite.NewService(configuration.Favorites).List()
	if len(ordered) == 0 {
		return favoriteSelection{}, errNoFavoritesConfigured
	}
	if selector != "" {
		selected, err := favorite.NewService(ordered).Resolve(selector)
		if err != nil {
			return favoriteSelection{}, fmt.Errorf("favorite selection: %w", err)
		}
		return favoriteSelection{favorite: selected, selector: selector}, nil
	}
	if dependencies.ManagementSelector == nil {
		return favoriteSelection{}, errors.New("favorite selection: interactive selector is not configured")
	}
	selected, err := dependencies.ManagementSelector.Select(ctx, ordered)
	if err != nil {
		return favoriteSelection{}, fmt.Errorf("favorite selection: %w", err)
	}
	for index, candidate := range ordered {
		if candidate.SameIdentity(selected) {
			return favoriteSelection{favorite: candidate, selector: strconv.Itoa(index + 1)}, nil
		}
	}
	return favoriteSelection{}, errors.New("favorite selection: selected favorite is unavailable")
}

func favoriteFormProfiles(ctx context.Context, dependencies FavoriteDependencies) ([]profile.Profile, error) {
	if dependencies.Profiles == nil {
		return nil, errors.New("favorite form: profile configuration is not configured")
	}
	configuration, err := dependencies.Profiles.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("favorite form: load profiles: %w", err)
	}
	profiles := profile.NewService(configuration.Profiles).List()
	if len(profiles) == 0 {
		return nil, errNoProfilesConfigured
	}
	return profiles, nil
}

func favoriteNote(note string) string {
	if note == "" {
		return "-"
	}
	return note
}

func interactiveFavoriteError(err error, usage, command string) error {
	switch {
	case errors.Is(err, errProfileSelectionRequiresTerminal):
		return managementUsageError(err.Error(), usage, command)
	case errors.Is(err, errNoProfilesConfigured):
		return managementUsageError("no profiles configured; add one with 'vlt profile add'", usage, command)
	case errors.Is(err, errNoFavoritesConfigured):
		return managementUsageError("no favorites configured; add one with 'vlt favorite add'", usage, command)
	default:
		return safeManagementError(err)
	}
}

func favoriteChangesBetween(original, updated favorite.Favorite) favorite.FavoriteChanges {
	changes := favorite.FavoriteChanges{}
	if original.Profile != updated.Profile {
		changes.Profile = &updated.Profile
	}
	if original.Operation != updated.Operation {
		changes.Operation = &updated.Operation
	}
	if original.Path != updated.Path {
		changes.Path = &updated.Path
	}
	if original.Note != updated.Note {
		changes.Note = &updated.Note
	}
	return changes
}

func requireInteractiveFavoriteTerminal(terminal Terminal) error {
	if terminal == nil || !terminal.PromptsEnabled() {
		return errProfileSelectionRequiresTerminal
	}
	return nil
}
