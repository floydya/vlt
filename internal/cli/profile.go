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
}

type ActiveProfileStore interface {
	ConfigurationLoader
	SetActiveProfile(context.Context, string) error
}

type SwitchDependencies struct {
	Profiles ActiveProfileStore
	Output   io.Writer
	Terminal Terminal
}

func NewProfileHandler(dependencies ProfileDependencies) Handler {
	return func(ctx context.Context, args []string) error {
		if len(args) == 0 {
			return errors.New("profile command is required: add, list, show, update, or remove")
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
			return fmt.Errorf("unknown profile command %q: use add, list, show, update, or remove", args[0])
		}
	}
}

func NewSwitchHandler(dependencies SwitchDependencies) Handler {
	return func(ctx context.Context, args []string) error {
		if dependencies.Profiles == nil {
			return errors.New("switch profile: profile configuration is not configured")
		}
		if dependencies.Output == nil {
			return errors.New("switch profile: output is not configured")
		}
		if len(args) > 1 {
			return errors.New("usage: vlt switch [NAME|NUMBER]")
		}

		configuration, err := dependencies.Profiles.Load(ctx)
		if err != nil {
			return safeManagementError(fmt.Errorf("switch profile: load profiles: %w", err))
		}
		profiles := profile.NewService(configuration.Profiles)
		if len(args) == 0 {
			active := configuration.ActiveProfile
			if active == "" {
				active = "none"
			}
			var output strings.Builder
			fmt.Fprintf(&output, "Active profile: %s\n", active)
			writeProfileList(&output, profiles.List())
			if _, err := io.WriteString(dependencies.Output, output.String()); err != nil {
				return fmt.Errorf("display profiles for switch: %w", err)
			}
			return nil
		}

		selected, err := profiles.Resolve(args[0])
		if err != nil {
			return safeManagementError(fmt.Errorf("select profile %q: %w", args[0], err))
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
	const usage = "usage: vlt profile add NAME --address URL --username USER [--auth-path PATH] [--namespace NAMESPACE]"
	if len(args) < 1 {
		return errors.New(usage)
	}
	options, err := parseProfileOptions("profile add", args[1:], "oidc")
	if err != nil {
		return fmt.Errorf("%s: %w", usage, err)
	}
	if !options.set["address"] {
		return errors.New(usage + ": --address is required")
	}
	if !options.set["username"] {
		return errors.New(usage + ": --username is required")
	}
	if dependencies.Mutations == nil {
		return errors.New("add profile: profile mutations are not configured")
	}
	if dependencies.Output == nil {
		return errors.New("add profile: output is not configured")
	}

	candidate := profile.Profile{
		Name: args[0], Address: options.address, Username: options.username,
		AuthPath: options.authPath, Namespace: options.namespace,
	}
	if err := dependencies.Mutations.Add(ctx, candidate); err != nil {
		return safeManagementError(err)
	}
	if _, err := fmt.Fprintf(dependencies.Output, "Added profile %q.\n", candidate.Name); err != nil {
		return fmt.Errorf("display added profile: %w", err)
	}
	return nil
}

func profileList(ctx context.Context, dependencies ProfileDependencies, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: vlt profile list")
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
	writeProfileList(&output, profile.NewService(configuration.Profiles).List())
	if _, err := io.WriteString(dependencies.Output, output.String()); err != nil {
		return fmt.Errorf("display profiles: %w", err)
	}
	return nil
}

func profileShow(ctx context.Context, dependencies ProfileDependencies, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: vlt profile show NAME")
	}
	if err := profile.ValidateName(args[0]); err != nil {
		return fmt.Errorf("profile show NAME: invalid name: %w", err)
	}
	if dependencies.Profiles == nil {
		return errors.New("show profile: profile configuration is not configured")
	}
	if dependencies.Output == nil {
		return errors.New("show profile: output is not configured")
	}
	configuration, err := dependencies.Profiles.Load(ctx)
	if err != nil {
		return safeManagementError(fmt.Errorf("show profile %q: load configuration: %w", args[0], err))
	}
	selected, err := profile.NewService(configuration.Profiles).Find(args[0])
	if err != nil {
		return fmt.Errorf("show profile %q: %w", args[0], err)
	}
	if _, err := fmt.Fprintf(dependencies.Output,
		"Name: %s\nAddress: %s\nUsername: %s\nAuth path: %s\nNamespace: %s\n",
		selected.Name, selected.Address, selected.Username, selected.AuthPath, selected.Namespace,
	); err != nil {
		return fmt.Errorf("display profile %q: %w", selected.Name, err)
	}
	return nil
}

func profileUpdateCommand(ctx context.Context, dependencies ProfileDependencies, args []string) error {
	const usage = "usage: vlt profile update NAME [--address URL] [--username USER] [--auth-path PATH] [--namespace NAMESPACE]"
	if len(args) < 1 {
		return errors.New(usage)
	}
	options, err := parseProfileOptions("profile update", args[1:], "")
	if err != nil {
		return fmt.Errorf("%s: %w", usage, err)
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
	if len(args) != 1 {
		return errors.New("usage: vlt profile remove NAME")
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

func writeProfileList(output io.Writer, profiles []profile.Profile) {
	for index, candidate := range profiles {
		_, _ = fmt.Fprintf(output, "%d. %s\n", index+1, candidate.Name)
	}
}

func safeManagementError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New(vaultexec.RedactDiagnostic(err.Error()))
}
