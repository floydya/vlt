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
	favoriteUpdateUsage = "vlt favorite update NUMBER [--profile NAME] [--operation read|kv-get] [--path PATH] [--note NOTE]"
	favoriteRemoveUsage = "vlt favorite remove NUMBER"
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

Usage:
  vlt favorite update NUMBER [--profile NAME] [--operation read|kv-get] [--path PATH] [--note NOTE]

Options:
  --profile NAME           Stored profile name
  --operation read|kv-get  Vault read operation
  --path PATH              Vault secret path
  --note NOTE              Optional display and search context; use --note= to clear
  -h, --help               Show help

Examples:
  vlt favorite update 1 --note reporting
  vlt favorite update 2 --operation read --path secret/app
`

const favoriteRemoveHelpText = `Remove a favorite Vault read target.

Usage:
  vlt favorite remove NUMBER

Options:
  -h, --help  Show help

Examples:
  vlt favorite remove 1
`

var favoriteCommands = []string{"add", "list", "update", "remove"}

type FavoriteConfigurationLoader interface {
	Load(context.Context) (favorite.Configuration, error)
}

type FavoriteMutator interface {
	Add(context.Context, favorite.Favorite) error
	Update(context.Context, string, favorite.FavoriteChanges) error
	Remove(context.Context, string) error
}

type FavoriteDependencies struct {
	Profiles           ConfigurationLoader
	Favorites          FavoriteConfigurationLoader
	Mutations          FavoriteMutator
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
			return AutomaticHelp{Text: favoriteHelpText}
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
		profiles, err := favoriteFormProfiles(ctx, dependencies)
		if err != nil {
			return interactiveFavoriteError(err, favoriteAddUsage, "vlt favorite add")
		}
		if dependencies.Form == nil {
			return errors.New("add favorite: interactive form is not configured")
		}
		candidate, err = dependencies.Form.Run(ctx, FavoriteFormRequest{
			Favorite: favorite.Favorite{Operation: favorite.OperationRead}, Profiles: profiles,
		})
		if err != nil {
			return safeManagementError(fmt.Errorf("add favorite: %w", err))
		}
	}
	if err := dependencies.Mutations.Add(ctx, candidate); err != nil {
		return safeManagementError(err)
	}
	if _, err := fmt.Fprintf(dependencies.Output, "Added favorite %q for profile %q.\n", candidate.Path, candidate.Profile); err != nil {
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
	ordered := favorite.NewService(configuration.Favorites).List()
	if _, err := io.WriteString(dependencies.Output, favoriteListOutput(ordered, dependencies.Terminal)); err != nil {
		return fmt.Errorf("display favorites: %w", err)
	}
	return nil
}

func favoriteListOutput(favorites []favorite.Favorite, terminal Terminal) string {
	rows := [][]profileTableCell{{
		{value: "#", header: true},
		{value: "OPERATION", header: true},
		{value: "PROFILE", header: true},
		{value: "PATH", header: true},
		{value: "NOTE", header: true},
	}}
	for index, candidate := range favorites {
		note := candidate.Note
		if note == "" {
			note = "-"
		}
		rows = append(rows, []profileTableCell{
			{value: strconv.Itoa(index + 1)},
			{value: sanitizeFavoriteDisplay(candidate.Operation)},
			{value: sanitizeFavoriteDisplay(candidate.Profile)},
			{value: sanitizeFavoriteDisplay(candidate.Path)},
			{value: sanitizeFavoriteDisplay(note)},
		})
	}
	return newProfilePresentation(terminal).table(rows)
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
	if len(options.set) == 0 {
		selection, err := selectFavoriteForManagement(ctx, dependencies, selector)
		if err != nil {
			return interactiveFavoriteError(err, favoriteUpdateUsage, "vlt favorite update")
		}
		profiles, err := favoriteFormProfiles(ctx, dependencies)
		if err != nil {
			return interactiveFavoriteError(err, favoriteUpdateUsage, "vlt favorite update")
		}
		if dependencies.Form == nil {
			return errors.New("update favorite: interactive form is not configured")
		}
		updated, err := dependencies.Form.Run(ctx, FavoriteFormRequest{Favorite: selection.favorite, Profiles: profiles})
		if err != nil {
			return safeManagementError(fmt.Errorf("update favorite: %w", err))
		}
		selector = selection.selector
		changes = favoriteChangesBetween(selection.favorite, updated)
	}
	if err := dependencies.Mutations.Update(ctx, selector, changes); err != nil {
		return safeManagementError(err)
	}
	if _, err := fmt.Fprintf(dependencies.Output, "Updated favorite %s.\n", selector); err != nil {
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
			return safeManagementError(fmt.Errorf("remove favorite: %w", err))
		}
		if !confirmed {
			return nil
		}
		selector = selection.selector
	}
	if err := dependencies.Mutations.Remove(ctx, selector); err != nil {
		return safeManagementError(err)
	}
	if _, err := fmt.Fprintf(dependencies.Output, "Removed favorite %s.\n", selector); err != nil {
		return fmt.Errorf("display removed favorite: %w", err)
	}
	return nil
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
