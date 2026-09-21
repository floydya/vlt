package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"vlt/internal/favorite"
)

const (
	favoriteUsage       = "vlt favorite COMMAND [ARGUMENT...]"
	favoriteAddUsage    = "vlt favorite add PATH --profile NAME --operation read|kv-get [--note NOTE]"
	favoriteListUsage   = "vlt favorite list"
	favoriteUpdateUsage = "vlt favorite update ID|NUMBER [--profile NAME] [--operation read|kv-get] [--path PATH] [--note NOTE]"
	favoriteRemoveUsage = "vlt favorite remove ID|NUMBER"
)

const favoriteHelpText = `Manage favorite Vault read targets.

Usage:
  vlt favorite COMMAND [ARGUMENT...]

Commands:
  add     Add a favorite
  list    List favorites in stable selection order
  update  Update a favorite
  remove  Remove a favorite

Options:
  -h, --help  Show help

Examples:
  vlt favorite list
  vlt favorite add secret/data/app --profile team-a --operation kv-get
`

const favoriteAddHelpText = `Add a favorite Vault read target.

Usage:
  vlt favorite add PATH --profile NAME --operation read|kv-get [--note NOTE]

Options:
  --profile NAME           Stored profile name
  --operation read|kv-get  Vault read operation
  --note NOTE              Optional display and search context
  -h, --help               Show help

Examples:
  vlt favorite add secret/app --profile team-a --operation read
  vlt favorite add secret/data/app --profile team-a --operation kv-get --note daily
`

const favoriteListHelpText = `List favorite Vault read targets in stable selection order.

Usage:
  vlt favorite list

Options:
  -h, --help  Show help

Examples:
  vlt favorite list
`

const favoriteUpdateHelpText = `Update a favorite Vault read target.

Use its stable ID from 'vlt favorite list', or a current list number.

Usage:
  vlt favorite update ID|NUMBER [--profile NAME] [--operation read|kv-get] [--path PATH] [--note NOTE]

Options:
  --profile NAME           Stored profile name
  --operation read|kv-get  Vault read operation
  --path PATH              Vault secret path
  --note NOTE              Optional display and search context; use --note= to clear
  -h, --help               Show help

Examples:
  vlt favorite update f_0123456789abcdef --note reporting
  vlt favorite update 2 --operation read --path secret/app
`

const favoriteRemoveHelpText = `Remove a favorite Vault read target.

Use its stable ID from 'vlt favorite list', or a current list number.

Usage:
  vlt favorite remove ID|NUMBER

Options:
  -h, --help  Show help

Examples:
  vlt favorite remove f_0123456789abcdef
`

var favoriteCommands = []string{"add", "list", "update", "remove"}

type FavoriteConfigurationLoader interface {
	Load(context.Context) (favorite.Configuration, error)
}

type FavoriteMutator interface {
	AddWithResult(context.Context, favorite.Favorite) (favorite.Favorite, error)
	UpdateWithResult(context.Context, string, favorite.FavoriteChanges, *favorite.Favorite) (favorite.Favorite, error)
	RemoveWithResult(context.Context, string, *favorite.Favorite) (favorite.Favorite, error)
}

type FavoriteUseRecorder interface {
	RecordUse(context.Context, favorite.Favorite) error
}

type FavoriteDependencies struct {
	Profiles           ConfigurationLoader
	Favorites          FavoriteConfigurationLoader
	Mutations          FavoriteMutator
	Recorder           FavoriteUseRecorder
	Lock               MutationLock
	Output             io.Writer
	Terminal           Terminal
	Selector           FavoriteSelector
	Vault              Handler
	ManagementSelector FavoriteManagementSelector
	Form               FavoriteForm
	RemovalConfirmer   FavoriteRemovalConfirmer
}

func NewFavoriteHandler(dependencies FavoriteDependencies) Handler {
	return func(ctx context.Context, args []string) error {
		if len(args) == 0 {
			if dependencies.Terminal == nil || !dependencies.Terminal.PromptsEnabled() {
				return AutomaticHelp{Text: favoriteHelpText}
			}
			return favoriteExecute(ctx, dependencies)
		}
		if isHelpFlag(args[0]) {
			return writeManagementHelp(dependencies.Output, "favorite", favoriteHelpText)
		}

		switch args[0] {
		case "add":
			return favoriteAdd(ctx, dependencies, args[1:])
		case "list":
			return favoriteList(ctx, dependencies, args[1:])
		case "update":
			return favoriteUpdate(ctx, dependencies, args[1:])
		case "remove":
			return favoriteRemove(ctx, dependencies, args[1:])
		default:
			diagnostic := fmt.Sprintf("unknown favorite command %q", args[0])
			if suggestion := suggestFavoriteCommand(args[0]); suggestion != "" {
				diagnostic += fmt.Sprintf(". Did you mean %q?", suggestion)
			}
			return safeManagementError(managementUsageError(diagnostic, favoriteUsage, "vlt favorite"))
		}
	}
}

func favoriteExecute(ctx context.Context, dependencies FavoriteDependencies) error {
	if dependencies.Favorites == nil {
		return errors.New("execute favorite: favorite configuration is not configured")
	}
	if dependencies.Selector == nil {
		return errors.New("execute favorite: searchable selector is not configured")
	}
	if dependencies.Vault == nil {
		return errors.New("execute favorite: Vault delegation is not configured")
	}
	configuration, err := dependencies.Favorites.Load(ctx)
	if err != nil {
		return safeManagementError(fmt.Errorf("execute favorite: load favorites: %w", err))
	}
	ordered := favorite.NewService(configuration.Favorites).List()
	if len(ordered) == 0 {
		return interactiveFavoriteError(errNoFavoritesConfigured, favoriteUsage, "vlt favorite")
	}
	selected, err := dependencies.Selector.Select(ctx, ordered)
	if err != nil {
		return interactiveOperationError(fmt.Errorf("execute favorite: %w", err))
	}
	resolved := favorite.Favorite{}
	found := false
	for _, candidate := range ordered {
		if candidate.SameIdentity(selected) {
			resolved = candidate
			found = true
			break
		}
	}
	if !found {
		return errors.New("execute favorite: selected favorite is unavailable")
	}

	arguments := []string{"--profile", resolved.Profile}
	switch resolved.Operation {
	case favorite.OperationRead:
		arguments = append(arguments, "read", resolved.Path)
	case favorite.OperationKVGet:
		arguments = append(arguments, "kv", "get", resolved.Path)
	default:
		return errors.New("execute favorite: stored operation is unsupported")
	}
	if err := dependencies.Vault(ctx, arguments); err != nil {
		return err
	}
	if dependencies.Recorder != nil {
		_ = withMutationLock(ctx, dependencies.Lock, func(lockContext context.Context) error {
			return dependencies.Recorder.RecordUse(lockContext, resolved)
		})
	}
	return nil
}

func favoriteAdd(ctx context.Context, dependencies FavoriteDependencies, args []string) error {
	if containsHelpFlag(args) {
		return writeManagementHelp(dependencies.Output, "favorite add", favoriteAddHelpText)
	}
	path := ""
	optionArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		path = args[0]
		optionArgs = args[1:]
	}
	options, err := parseFavoriteOptions("favorite add", optionArgs, false)
	if err != nil {
		return managementUsageError(fmt.Sprintf("invalid favorite add option: %v", err), favoriteAddUsage, "vlt favorite add")
	}
	explicit := path != "" || len(options.set) != 0
	if explicit && (path == "" || options.profile == "" || options.operation == "") {
		return AutomaticHelp{Text: favoriteAddHelpText}
	}
	if !explicit && (dependencies.Terminal == nil || !dependencies.Terminal.PromptsEnabled()) {
		return AutomaticHelp{Text: favoriteAddHelpText}
	}
	if dependencies.Mutations == nil {
		return errors.New("add favorite: favorite mutations are not configured")
	}
	if dependencies.Output == nil {
		return errors.New("add favorite: output is not configured")
	}
	candidate := favorite.Favorite{
		Profile: options.profile, Operation: options.operation, Path: path, Note: options.note,
	}
	if !explicit {
		profiles, active, err := favoriteFormProfiles(ctx, dependencies)
		if err != nil {
			return interactiveFavoriteError(err, favoriteAddUsage, "vlt favorite add")
		}
		if dependencies.Form == nil {
			return errors.New("add favorite: interactive form is not configured")
		}
		candidate, err = dependencies.Form.Run(ctx, FavoriteFormRequest{
			Favorite: favorite.Favorite{Operation: favorite.OperationRead}, Profiles: profiles, ActiveProfile: active,
		})
		if err != nil {
			return interactiveOperationError(fmt.Errorf("add favorite: %w", err))
		}
	}
	var added favorite.Favorite
	if err := withMutationLock(ctx, dependencies.Lock, func(lockContext context.Context) error {
		var addErr error
		added, addErr = dependencies.Mutations.AddWithResult(lockContext, candidate)
		return addErr
	}); err != nil {
		return safeManagementError(err)
	}
	status := newPresentation(dependencies.Terminal).status(favoriteMutationMessage("Added", added, candidate, ""))
	if _, err := io.WriteString(dependencies.Output, status); err != nil {
		return fmt.Errorf("display added favorite: %w", err)
	}
	return nil
}

func favoriteList(ctx context.Context, dependencies FavoriteDependencies, args []string) error {
	if containsHelpFlag(args) {
		return writeManagementHelp(dependencies.Output, "favorite list", favoriteListHelpText)
	}
	if len(args) != 0 {
		return managementUsageError(fmt.Sprintf("unexpected argument %q", args[0]), favoriteListUsage, "vlt favorite list")
	}
	if dependencies.Favorites == nil {
		return errors.New("list favorites: favorite configuration is not configured")
	}
	if dependencies.Output == nil {
		return errors.New("list favorites: output is not configured")
	}
	configuration, err := dependencies.Favorites.Load(ctx)
	if err != nil {
		return safeManagementError(fmt.Errorf("list favorites: load configuration: %w", err))
	}
	var colors map[string]string
	if dependencies.Terminal != nil && dependencies.Terminal.ColorEnabled() {
		colors, err = loadProfileColors(ctx, dependencies.Profiles)
		if err != nil {
			return safeManagementError(fmt.Errorf("list favorites: %w", err))
		}
	}
	if _, err := io.WriteString(dependencies.Output, favoriteListOutput(configuration.Favorites, colors, dependencies.Terminal)); err != nil {
		return fmt.Errorf("display favorites: %w", err)
	}
	return nil
}

func favoriteListOutput(favorites []favorite.Favorite, colors map[string]string, terminal Terminal) string {
	rows := [][]presentationCell{{
		{value: "#", role: presentationHeading},
		{value: "ID", role: presentationHeading},
		{value: "RUNS", role: presentationHeading},
		{value: "OPERATION", role: presentationHeading},
		{value: "PROFILE", role: presentationHeading},
		{value: "PATH", role: presentationHeading},
		{value: "NOTE", role: presentationHeading},
	}}
	for index, candidate := range favorite.NewService(favorites).List() {
		note := candidate.Note
		if note == "" {
			note = "-"
		}
		row := []presentationCell{
			{value: strconv.Itoa(index + 1)},
			{value: candidate.ID},
			{value: strconv.FormatInt(candidate.RunCount, 10)},
			{value: sanitizeFavoriteDisplay(candidate.Operation)},
			{value: sanitizeFavoriteDisplay(candidate.Profile)},
			{value: sanitizeFavoriteDisplay(candidate.Path)},
			{value: sanitizeFavoriteDisplay(note)},
		}
		for index := range row {
			row[index].color = colors[candidate.Profile]
		}
		rows = append(rows, row)
	}
	p := newPresentation(terminal)
	if p.tableFits(rows) {
		return p.renderTable(rows)
	}
	var output strings.Builder
	for index, candidate := range favorite.NewService(favorites).List() {
		color := colors[candidate.Profile]
		colored := func(value string) string { return p.renderColored(presentationPlain, value, color) }
		output.WriteString(p.wrappedLine(fmt.Sprintf("%d  %s", index+1, colored(sanitizeFavoriteDisplay(candidate.Path)))))
		output.WriteString(p.wrappedField("ID", colored(candidate.ID)))
		output.WriteString(p.wrappedField("Runs", colored(strconv.FormatInt(candidate.RunCount, 10))))
		output.WriteString(p.wrappedField("Profile", colored(sanitizeFavoriteDisplay(candidate.Profile))))
		output.WriteString(p.wrappedField("Operation", colored(sanitizeFavoriteDisplay(candidate.Operation))))
		note := candidate.Note
		if note == "" {
			note = "-"
		}
		output.WriteString(p.wrappedField("Note", colored(sanitizeFavoriteDisplay(note))))
	}
	return output.String()
}

func sanitizeFavoriteDisplay(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
}

func favoriteUpdate(ctx context.Context, dependencies FavoriteDependencies, args []string) error {
	if containsHelpFlag(args) {
		return writeManagementHelp(dependencies.Output, "favorite update", favoriteUpdateHelpText)
	}
	selector := ""
	optionArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		selector = args[0]
		optionArgs = args[1:]
	}
	options, err := parseFavoriteOptions("favorite update", optionArgs, true)
	if err != nil {
		return managementUsageError(fmt.Sprintf("invalid favorite update option: %v", err), favoriteUpdateUsage, "vlt favorite update")
	}
	if selector == "" && len(options.set) != 0 {
		return AutomaticHelp{Text: favoriteUpdateHelpText}
	}
	if len(options.set) == 0 && (dependencies.Terminal == nil || !dependencies.Terminal.PromptsEnabled()) {
		return AutomaticHelp{Text: favoriteUpdateHelpText}
	}
	if dependencies.Mutations == nil {
		return errors.New("update favorite: favorite mutations are not configured")
	}
	if dependencies.Output == nil {
		return errors.New("update favorite: output is not configured")
	}
	changes := favoriteChangesFromOptions(options)
	var expected *favorite.Favorite
	if len(options.set) == 0 {
		selection, err := selectFavoriteForManagement(ctx, dependencies, selector)
		if err != nil {
			return interactiveFavoriteError(err, favoriteUpdateUsage, "vlt favorite update")
		}
		profiles, _, err := favoriteFormProfiles(ctx, dependencies)
		if err != nil {
			return interactiveFavoriteError(err, favoriteUpdateUsage, "vlt favorite update")
		}
		if dependencies.Form == nil {
			return errors.New("update favorite: interactive form is not configured")
		}
		updated, err := dependencies.Form.Run(ctx, FavoriteFormRequest{Favorite: selection.favorite, Profiles: profiles})
		if err != nil {
			return interactiveOperationError(fmt.Errorf("update favorite: %w", err))
		}
		selector = selection.selector
		changes = favoriteChangesBetween(selection.favorite, updated)
		expected = &selection.favorite
	}
	var changed favorite.Favorite
	if err := withMutationLock(ctx, dependencies.Lock, func(lockContext context.Context) error {
		var updateErr error
		changed, updateErr = dependencies.Mutations.UpdateWithResult(lockContext, selector, changes, expected)
		return updateErr
	}); err != nil {
		return safeManagementError(err)
	}
	status := newPresentation(dependencies.Terminal).status(favoriteMutationMessage("Updated", changed, favorite.Favorite{}, selector))
	if _, err := io.WriteString(dependencies.Output, status); err != nil {
		return fmt.Errorf("display updated favorite: %w", err)
	}
	return nil
}

func favoriteRemove(ctx context.Context, dependencies FavoriteDependencies, args []string) error {
	if containsHelpFlag(args) {
		return writeManagementHelp(dependencies.Output, "favorite remove", favoriteRemoveHelpText)
	}
	if len(args) == 0 && (dependencies.Terminal == nil || !dependencies.Terminal.PromptsEnabled()) {
		return AutomaticHelp{Text: favoriteRemoveHelpText}
	}
	if len(args) > 1 {
		return managementUsageError(fmt.Sprintf("unexpected argument %q", args[1]), favoriteRemoveUsage, "vlt favorite remove")
	}
	if dependencies.Mutations == nil {
		return errors.New("remove favorite: favorite mutations are not configured")
	}
	if dependencies.Output == nil {
		return errors.New("remove favorite: output is not configured")
	}
	selector := ""
	var expected *favorite.Favorite
	if len(args) == 1 {
		selector = args[0]
	} else {
		selection, err := selectFavoriteForManagement(ctx, dependencies, "")
		if err != nil {
			return interactiveFavoriteError(err, favoriteRemoveUsage, "vlt favorite remove")
		}
		if dependencies.RemovalConfirmer == nil {
			return errors.New("remove favorite: interactive confirmation is not configured")
		}
		confirmed, err := dependencies.RemovalConfirmer.Confirm(ctx, FavoriteRemovalConfirmation{Favorite: selection.favorite})
		if err != nil {
			return interactiveOperationError(fmt.Errorf("remove favorite: %w", err))
		}
		if !confirmed {
			return nil
		}
		selector = selection.selector
		expected = &selection.favorite
	}
	var removed favorite.Favorite
	if err := withMutationLock(ctx, dependencies.Lock, func(lockContext context.Context) error {
		var removeErr error
		removed, removeErr = dependencies.Mutations.RemoveWithResult(lockContext, selector, expected)
		return removeErr
	}); err != nil {
		return safeManagementError(err)
	}
	status := newPresentation(dependencies.Terminal).status(favoriteMutationMessage("Removed", removed, favorite.Favorite{}, selector))
	if _, err := io.WriteString(dependencies.Output, status); err != nil {
		return fmt.Errorf("display removed favorite: %w", err)
	}
	return nil
}

func favoriteMutationMessage(action string, result, fallback favorite.Favorite, selector string) string {
	if result.ID != "" {
		return fmt.Sprintf("%s favorite %q for profile %q (ID %s).", action,
			sanitizeFavoriteDisplay(result.Path), sanitizeFavoriteDisplay(result.Profile), result.ID)
	}
	if action == "Added" {
		return fmt.Sprintf("Added favorite %q for profile %q.", fallback.Path, fallback.Profile)
	}
	return fmt.Sprintf("%s favorite %s.", action, selector)
}

type favoriteOptions struct {
	profile   string
	operation string
	path      string
	note      string
	set       map[string]bool
}

func parseFavoriteOptions(command string, args []string, allowPath bool) (favoriteOptions, error) {
	options := favoriteOptions{set: make(map[string]bool)}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.profile, "profile", "", "")
	flags.StringVar(&options.operation, "operation", "", "")
	if allowPath {
		flags.StringVar(&options.path, "path", "", "")
	}
	flags.StringVar(&options.note, "note", "", "")
	if err := flags.Parse(args); err != nil {
		return favoriteOptions{}, err
	}
	if flags.NArg() != 0 {
		return favoriteOptions{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	flags.Visit(func(candidate *flag.Flag) {
		options.set[candidate.Name] = true
	})
	return options, nil
}

func favoriteChangesFromOptions(options favoriteOptions) favorite.FavoriteChanges {
	changes := favorite.FavoriteChanges{}
	if options.set["profile"] {
		changes.Profile = &options.profile
	}
	if options.set["operation"] {
		changes.Operation = &options.operation
	}
	if options.set["path"] {
		changes.Path = &options.path
	}
	if options.set["note"] {
		changes.Note = &options.note
	}
	return changes
}

func suggestFavoriteCommand(value string) string {
	best := ""
	bestDistance := 3
	tied := false
	for _, command := range favoriteCommands {
		distance := editDistance(value, command)
		switch {
		case distance < bestDistance:
			best = command
			bestDistance = distance
			tied = false
		case distance == bestDistance:
			tied = true
		}
	}
	if bestDistance > 2 || tied {
		return ""
	}
	return best
}
