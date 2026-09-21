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

type sharedFavoriteManagementSelector struct {
	selector SharedSelector
	profiles ConfigurationLoader
}

type huhFavoriteForm struct {
	input        io.Reader
	output       io.Writer
	accessible   bool
	selector     SharedSelector
	presentation presentation
}

type huhFavoriteRemovalConfirmer struct {
	input        io.Reader
	output       io.Writer
	accessible   bool
	presentation presentation
}

func NewSharedFavoriteManagementSelector(selector SharedSelector, profiles ConfigurationLoader) FavoriteManagementSelector {
	return sharedFavoriteManagementSelector{selector: selector, profiles: profiles}
}

func NewHuhFavoriteForm(input io.Reader, output io.Writer, selector SharedSelector, terminal Terminal) FavoriteForm {
	return huhFavoriteForm{
		input: input, output: output, selector: selector, presentation: newPresentation(terminal),
	}
}

func NewHuhFavoriteRemovalConfirmer(input io.Reader, output io.Writer, terminal ...Terminal) FavoriteRemovalConfirmer {
	return huhFavoriteRemovalConfirmer{input: input, output: output, presentation: presentationForOptionalTerminal(terminal)}
}

func (s sharedFavoriteManagementSelector) Select(ctx context.Context, candidates []favorite.Favorite) (favorite.Favorite, error) {
	return sharedFavoriteSelector(s).Select(ctx, candidates)
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
	if f.selector == nil {
		return favorite.Favorite{}, errors.New("favorite form: shared selector is not configured")
	}
	profiles := profile.NewService(request.Profiles).List()
	candidate := request.Favorite
	if candidate.Profile == "" {
		candidate.Profile = profiles[0].Name
	}
	if candidate.Operation == "" {
		candidate.Operation = favorite.OperationRead
	}
	profileRows := make([][]string, 0, len(profiles))
	for index, candidateProfile := range profiles {
		namespace := candidateProfile.Namespace
		if namespace == "" {
			namespace = "-"
		}
		color := candidateProfile.Color
		if color == "" {
			color = "-"
		}
		profileRows = append(profileRows, []string{fmt.Sprint(index + 1), candidateProfile.Name, candidateProfile.Address, namespace, color})
	}
	profileHeader, profileLabels := selectorTableRows([]string{"#", "NAME", "ADDRESS", "NAMESPACE", "COLOR"}, profileRows)
	profileItems := make([]SharedSelectorItem, 0, len(profiles))
	for index, candidateProfile := range profiles {
		profileItems = append(profileItems, SharedSelectorItem{
			ID: candidateProfile.Name, Label: profileLabels[index], SearchText: profileLabels[index], Color: candidateProfile.Color,
		})
	}
	selectedProfile, err := f.selector.Select(ctx, "Select a profile", profileHeader, profileItems, candidate.Profile)
	if err != nil {
		return favorite.Favorite{}, fmt.Errorf("favorite form: select profile: %w", err)
	}
	candidate.Profile = selectedProfile

	operationItems := []SharedSelectorItem{
		{ID: favorite.OperationRead, Label: favorite.OperationRead, SearchText: favorite.OperationRead},
		{ID: favorite.OperationKVGet, Label: favorite.OperationKVGet, SearchText: favorite.OperationKVGet},
	}
	selectedOperation, err := f.selector.Select(ctx, "Select an operation", "", operationItems, candidate.Operation)
	if err != nil {
		return favorite.Favorite{}, fmt.Errorf("favorite form: select operation: %w", err)
	}
	candidate.Operation = selectedOperation

	pathValidator := func(value string) error {
		probe := candidate
		probe.Path = value
		return probe.Validate()
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Path").Value(&candidate.Path).Validate(pathValidator),
		huh.NewInput().Title("Note").Value(&candidate.Note),
	).Title("Favorite details")).
		WithInput(f.input).
		WithOutput(f.output).
		WithAccessible(f.accessible).
		WithTheme(f.presentation.huhTheme())
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
	if !c.presentation.colorEnabled {
		form = form.WithTheme(c.presentation.huhTheme())
	}
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

func interactiveFavoriteError(err error, usage, command string) error {
	switch {
	case errors.Is(err, errProfileSelectionRequiresTerminal):
		return managementUsageError(err.Error(), usage, command)
	case errors.Is(err, errNoProfilesConfigured):
		return managementUsageError("no profiles configured; add one with 'vlt profile add'", usage, command)
	case errors.Is(err, errNoFavoritesConfigured):
		return managementUsageError("no favorites configured; add one with 'vlt favorite add'", usage, command)
	default:
		return interactiveOperationError(err)
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
