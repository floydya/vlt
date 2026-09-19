package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

const (
	profileUsage       = "vlt profile COMMAND [ARGUMENT...]"
	profileAddUsage    = "vlt profile add [NAME] [--address URL] [--username USER] [--auth-path PATH] [--namespace NAMESPACE]"
	profileListUsage   = "vlt profile list"
	profileShowUsage   = "vlt profile show [NAME]"
	profileUpdateUsage = "vlt profile update NAME [--address URL] [--username USER] [--auth-path PATH] [--namespace NAMESPACE]"
	profileRemoveUsage = "vlt profile remove NAME"
	switchUsage        = "vlt switch [NAME|NUMBER]"
)

const profileHelpText = `Manage Vault profiles.

Usage:
  vlt profile COMMAND [ARGUMENT...]

Commands:
  add     Add and authenticate a profile
  list    List configured profiles
  show    Show profile metadata
  update  Update and reauthenticate a profile
  remove  Remove a profile and its stored credential

Options:
  -h, --help  Show help

Examples:
  vlt profile list
  vlt profile show team-a
`

const profileAddHelpText = `Add a Vault profile and authenticate it.

Usage:
  vlt profile add [NAME] [--address URL] [--username USER] [--auth-path PATH] [--namespace NAMESPACE]

Options:
  --address URL          Vault server URL
  --username USER        OIDC username
  --auth-path PATH       OIDC mount path (default: oidc)
  --namespace NAMESPACE  Vault Enterprise namespace
  -h, --help             Show help

Examples:
  vlt profile add
  vlt profile add team-a --address https://vault.example.com --username alice
`

const profileListHelpText = `List Vault profiles in stable selection order.

Usage:
  vlt profile list

Options:
  -h, --help  Show help

Examples:
  vlt profile list
`

const profileShowHelpText = `Show a Vault profile without credentials.

Usage:
  vlt profile show [NAME]

Options:
  -h, --help  Show help

Examples:
  vlt profile show team-a
`

const profileUpdateHelpText = `Update a Vault profile and reauthenticate when required.

Usage:
  vlt profile update NAME [--address URL] [--username USER] [--auth-path PATH] [--namespace NAMESPACE]

Options:
  --address URL          Vault server URL
  --username USER        OIDC username
  --auth-path PATH       OIDC mount path
  --namespace NAMESPACE  Vault Enterprise namespace
  -h, --help             Show help

Examples:
  vlt profile update team-a --namespace platform
`

const profileRemoveHelpText = `Remove a Vault profile and its stored credential.

Usage:
  vlt profile remove NAME

Options:
  -h, --help  Show help

Examples:
  vlt profile remove team-a
`

const switchHelpText = `Select the active Vault profile or show the current selection.

Usage:
  vlt switch [NAME|NUMBER]

Options:
  -h, --help  Show help

Examples:
  vlt switch team-a
  vlt switch 1
`

var profileCommands = []string{"add", "list", "show", "update", "remove"}

type ProfileMutator interface {
	Add(context.Context, profile.Profile) error
	Update(context.Context, string, profile.ProfileChanges) error
	Remove(context.Context, string) error
}

type ProfileDependencies struct {
	Profiles  ConfigurationLoader
	Mutations ProfileMutator
	Output    io.Writer
	Terminal  Terminal
	Selector  ProfileSelector
	Form      ProfileForm
}

type ActiveProfileStore interface {
	ConfigurationLoader
	SetActiveProfile(context.Context, string) error
}

type SwitchDependencies struct {
	Profiles ActiveProfileStore
	Output   io.Writer
	Terminal Terminal
	Selector ProfileSelector
}

func NewProfileHandler(dependencies ProfileDependencies) Handler {
	return func(ctx context.Context, args []string) error {
		if len(args) == 0 {
			return managementUsageError("profile command is required", profileUsage, "vlt profile")
		}
		if isHelpFlag(args[0]) {
			return writeManagementHelp(dependencies.Output, "profile", profileHelpText)
		}

		switch args[0] {
		case "add":
			return profileAdd(ctx, dependencies, args[1:])
		case "list":
			return profileList(ctx, dependencies, args[1:])
		case "show":
			return profileShow(ctx, dependencies, args[1:])
		case "update":
			return profileUpdateCommand(ctx, dependencies, args[1:])
		case "remove":
			return profileRemove(ctx, dependencies, args[1:])
		default:
			diagnostic := fmt.Sprintf("unknown profile command %q", args[0])
			if suggestion := suggestProfileCommand(args[0]); suggestion != "" {
				diagnostic += fmt.Sprintf(". Did you mean %q?", suggestion)
			}
			return safeManagementError(managementUsageError(diagnostic, profileUsage, "vlt profile"))
		}
	}
}

func NewSwitchHandler(dependencies SwitchDependencies) Handler {
	return func(ctx context.Context, args []string) error {
		if containsHelpFlag(args) {
			return writeManagementHelp(dependencies.Output, "switch", switchHelpText)
		}
		if len(args) > 1 {
			return managementUsageError(fmt.Sprintf("unexpected argument %q", args[1]), switchUsage, "vlt switch")
		}
		if dependencies.Profiles == nil {
			return errors.New("switch profile: profile configuration is not configured")
		}
		if dependencies.Output == nil {
			return errors.New("switch profile: output is not configured")
		}
		var selected profile.Profile
		if len(args) == 0 {
			var err error
			selected, _, err = selectProfileInteractively(ctx, dependencies.Profiles, dependencies.Terminal, dependencies.Selector)
			if err != nil {
				return interactiveProfileError(err, switchUsage, "vlt switch")
			}
		} else {
			configuration, err := dependencies.Profiles.Load(ctx)
			if err != nil {
				return safeManagementError(fmt.Errorf("switch profile: load profiles: %w", err))
			}
			selected, err = profile.NewService(configuration.Profiles).Resolve(args[0])
			if err != nil {
				return safeManagementError(fmt.Errorf("select profile %q: %w", args[0], err))
			}
		}
		if err := dependencies.Profiles.SetActiveProfile(ctx, selected.Name); err != nil {
			return safeManagementError(fmt.Errorf("switch to profile %q: %w", selected.Name, err))
		}
		if _, err := fmt.Fprintf(dependencies.Output, "Switched to profile %q.\n", selected.Name); err != nil {
			return fmt.Errorf("display selected profile: %w", err)
		}
		return nil
	}
}

func profileAdd(ctx context.Context, dependencies ProfileDependencies, args []string) error {
	if containsHelpFlag(args) {
		return writeManagementHelp(dependencies.Output, "profile add", profileAddHelpText)
	}
	name := ""
	optionArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		optionArgs = args[1:]
	}
	options, err := parseProfileOptions("profile add", optionArgs, "oidc")
	if err != nil {
		return managementUsageError(fmt.Sprintf("invalid profile add option: %v", err), profileAddUsage, "vlt profile add")
	}
	candidate := profile.Profile{
		Name: name, Address: options.address, Username: options.username,
		AuthPath: options.authPath, Namespace: options.namespace,
	}
	missing := missingProfileAddFields(candidate)
	if len(missing) != 0 && (dependencies.Terminal == nil || !dependencies.Terminal.PromptsEnabled()) {
		return managementUsageError(missingProfileAddDiagnostic(missing), profileAddUsage, "vlt profile add")
	}
	if dependencies.Mutations == nil {
		return errors.New("add profile: profile mutations are not configured")
	}
	if dependencies.Output == nil {
		return errors.New("add profile: output is not configured")
	}
	if len(missing) != 0 {
		if dependencies.Form == nil {
			return errors.New("add profile: interactive form is not configured")
		}
		candidate, err = dependencies.Form.Run(ctx, candidate)
		if err != nil {
			return safeManagementError(fmt.Errorf("add profile: %w", err))
		}
	}
	if err := dependencies.Mutations.Add(ctx, candidate); err != nil {
		return safeManagementError(err)
	}
	if _, err := fmt.Fprintf(dependencies.Output, "Added profile %q.\n", candidate.Name); err != nil {
		return fmt.Errorf("display added profile: %w", err)
	}
	return nil
}

func missingProfileAddFields(candidate profile.Profile) []string {
	var missing []string
	if candidate.Name == "" {
		missing = append(missing, "profile NAME")
	}
	if candidate.Address == "" {
		missing = append(missing, "--address")
	}
	if strings.TrimSpace(candidate.Username) == "" {
		missing = append(missing, "--username")
	}
	return missing
}

func missingProfileAddDiagnostic(missing []string) string {
	if len(missing) == 1 {
		return missing[0] + " is required outside an interactive terminal"
	}
	if len(missing) == 2 {
		return missing[0] + " and " + missing[1] + " are required outside an interactive terminal"
	}
	return strings.Join(missing[:len(missing)-1], ", ") + ", and " + missing[len(missing)-1] +
		" are required outside an interactive terminal"
}

func profileList(ctx context.Context, dependencies ProfileDependencies, args []string) error {
	if containsHelpFlag(args) {
		return writeManagementHelp(dependencies.Output, "profile list", profileListHelpText)
	}
	if len(args) != 0 {
		return managementUsageError(fmt.Sprintf("unexpected argument %q", args[0]), profileListUsage, "vlt profile list")
	}
	if dependencies.Profiles == nil {
		return errors.New("list profiles: profile configuration is not configured")
	}
	if dependencies.Output == nil {
		return errors.New("list profiles: output is not configured")
	}
	configuration, err := dependencies.Profiles.Load(ctx)
	if err != nil {
		return safeManagementError(fmt.Errorf("list profiles: load configuration: %w", err))
	}
	var output strings.Builder
	output.WriteString(newProfilePresentation(dependencies.Terminal).list(
		profile.NewService(configuration.Profiles).List(),
		configuration.ActiveProfile,
	))
	if _, err := io.WriteString(dependencies.Output, output.String()); err != nil {
		return fmt.Errorf("display profiles: %w", err)
	}
	return nil
}

func profileShow(ctx context.Context, dependencies ProfileDependencies, args []string) error {
	if containsHelpFlag(args) {
		return writeManagementHelp(dependencies.Output, "profile show", profileShowHelpText)
	}
	if len(args) > 1 {
		return managementUsageError(fmt.Sprintf("unexpected argument %q", args[1]), profileShowUsage, "vlt profile show")
	}
	if dependencies.Profiles == nil {
		return errors.New("show profile: profile configuration is not configured")
	}
	if dependencies.Output == nil {
		return errors.New("show profile: output is not configured")
	}
	var selected profile.Profile
	activeProfile := ""
	if len(args) == 0 {
		var err error
		selected, activeProfile, err = selectProfileInteractively(ctx, dependencies.Profiles, dependencies.Terminal, dependencies.Selector)
		if err != nil {
			return interactiveProfileError(err, profileShowUsage, "vlt profile show")
		}
	} else {
		if err := profile.ValidateName(args[0]); err != nil {
			return managementUsageError(fmt.Sprintf("invalid profile NAME %q: %v", args[0], err), profileShowUsage, "vlt profile show")
		}
		configuration, err := dependencies.Profiles.Load(ctx)
		if err != nil {
			return safeManagementError(fmt.Errorf("show profile %q: load configuration: %w", args[0], err))
		}
		selected, err = profile.NewService(configuration.Profiles).Find(args[0])
		if err != nil {
			return fmt.Errorf("show profile %q: %w", args[0], err)
		}
		activeProfile = configuration.ActiveProfile
	}
	details := newProfilePresentation(dependencies.Terminal).details(selected, selected.Name == activeProfile)
	if _, err := io.WriteString(dependencies.Output, details); err != nil {
		return fmt.Errorf("display profile %q: %w", selected.Name, err)
	}
	return nil
}

func profileUpdateCommand(ctx context.Context, dependencies ProfileDependencies, args []string) error {
	if containsHelpFlag(args) {
		return writeManagementHelp(dependencies.Output, "profile update", profileUpdateHelpText)
	}
	if len(args) < 1 {
		return managementUsageError("profile NAME is required", profileUpdateUsage, "vlt profile update")
	}
	options, err := parseProfileOptions("profile update", args[1:], "")
	if err != nil {
		return managementUsageError(fmt.Sprintf("invalid profile update option: %v", err), profileUpdateUsage, "vlt profile update")
	}
	if dependencies.Mutations == nil {
		return errors.New("update profile: profile mutations are not configured")
	}
	if dependencies.Output == nil {
		return errors.New("update profile: output is not configured")
	}

	changes := profile.ProfileChanges{}
	if options.set["address"] {
		changes.Address = &options.address
	}
	if options.set["username"] {
		changes.Username = &options.username
	}
	if options.set["auth-path"] {
		changes.AuthPath = &options.authPath
	}
	if options.set["namespace"] {
		changes.Namespace = &options.namespace
	}
	if err := dependencies.Mutations.Update(ctx, args[0], changes); err != nil {
		return safeManagementError(err)
	}
	if _, err := fmt.Fprintf(dependencies.Output, "Updated profile %q.\n", args[0]); err != nil {
		return fmt.Errorf("display updated profile: %w", err)
	}
	return nil
}

func profileRemove(ctx context.Context, dependencies ProfileDependencies, args []string) error {
	if containsHelpFlag(args) {
		return writeManagementHelp(dependencies.Output, "profile remove", profileRemoveHelpText)
	}
	if len(args) != 1 {
		diagnostic := "profile NAME is required"
		if len(args) > 1 {
			diagnostic = fmt.Sprintf("unexpected argument %q", args[1])
		}
		return managementUsageError(diagnostic, profileRemoveUsage, "vlt profile remove")
	}
	if dependencies.Mutations == nil {
		return errors.New("remove profile: profile mutations are not configured")
	}
	if dependencies.Output == nil {
		return errors.New("remove profile: output is not configured")
	}
	if err := dependencies.Mutations.Remove(ctx, args[0]); err != nil {
		return safeManagementError(err)
	}
	if _, err := fmt.Fprintf(dependencies.Output, "Removed profile %q.\n", args[0]); err != nil {
		return fmt.Errorf("display removed profile: %w", err)
	}
	return nil
}

type profileOptions struct {
	address   string
	username  string
	authPath  string
	namespace string
	set       map[string]bool
}

func parseProfileOptions(command string, args []string, defaultAuthPath string) (profileOptions, error) {
	options := profileOptions{authPath: defaultAuthPath, set: make(map[string]bool)}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.address, "address", "", "")
	flags.StringVar(&options.username, "username", "", "")
	flags.StringVar(&options.authPath, "auth-path", defaultAuthPath, "")
	flags.StringVar(&options.namespace, "namespace", "", "")
	if err := flags.Parse(args); err != nil {
		return profileOptions{}, err
	}
	if flags.NArg() != 0 {
		return profileOptions{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	flags.Visit(func(candidate *flag.Flag) {
		options.set[candidate.Name] = true
	})
	return options, nil
}

func safeManagementError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New(vaultexec.RedactDiagnostic(err.Error()))
}

func isHelpFlag(value string) bool {
	return value == "-h" || value == "--help"
}

func containsHelpFlag(args []string) bool {
	for _, arg := range args {
		if isHelpFlag(arg) {
			return true
		}
	}
	return false
}

func writeManagementHelp(output io.Writer, command, help string) error {
	if output == nil {
		return fmt.Errorf("display %s help: output is not configured", command)
	}
	if _, err := io.WriteString(output, help); err != nil {
		return fmt.Errorf("display %s help: %w", command, err)
	}
	return nil
}

func managementUsageError(diagnostic, usage, command string) error {
	message := fmt.Sprintf("%s\nUsage: %s\nRun '%s --help' for more information", diagnostic, usage, command)
	return errors.New(vaultexec.RedactDiagnostic(message))
}

func suggestProfileCommand(value string) string {
	best := ""
	bestDistance := 3
	tied := false
	for _, command := range profileCommands {
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

func editDistance(left, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	previous := make([]int, len(rightRunes)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex, leftRune := range leftRunes {
		current := make([]int, len(rightRunes)+1)
		current[0] = leftIndex + 1
		for rightIndex, rightRune := range rightRunes {
			cost := 1
			if leftRune == rightRune {
				cost = 0
			}
			current[rightIndex+1] = min(
				current[rightIndex]+1,
				previous[rightIndex+1]+1,
				previous[rightIndex]+cost,
			)
		}
		previous = current
	}
	return previous[len(rightRunes)]
}
