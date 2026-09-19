package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"vlt/internal/config"
	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

type ConfigurationLoader interface {
	Load(context.Context) (config.Configuration, error)
}

type CredentialPreflight interface {
	Prepare(context.Context, profile.Profile) (string, error)
}

type VaultExecutor interface {
	FindVault() (string, error)
	Execute(context.Context, vaultexec.Invocation) (vaultexec.Result, error)
}

type DelegateDependencies struct {
	Profiles  ConfigurationLoader
	Preflight CredentialPreflight
	Vault     VaultExecutor
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

func NewDelegateHandler(dependencies DelegateDependencies) Handler {
	return func(ctx context.Context, args []string) error {
		profileName, vaultArguments, err := parseDelegateArguments(args)
		if err != nil {
			return err
		}
		if dependencies.Profiles == nil {
			return errors.New("delegate Vault command: profile configuration is not configured")
		}
		if dependencies.Preflight == nil {
			return errors.New("delegate Vault command: credential preflight is not configured")
		}
		if dependencies.Vault == nil {
			return errors.New("delegate Vault command: Vault executor is not configured")
		}

		configuration, err := dependencies.Profiles.Load(ctx)
		if err != nil {
			return fmt.Errorf("delegate Vault command: load profile configuration: %w", err)
		}
		if profileName == "" {
			profileName = configuration.ActiveProfile
			if profileName == "" {
				return errors.New("no profile is selected; add one with `vlt profile add` or select one with `vlt switch NAME`")
			}
		}
		selected, err := profile.NewService(configuration.Profiles).Find(profileName)
		if err != nil {
			return fmt.Errorf("select profile %q for this invocation: %w", profileName, err)
		}

		if _, err := dependencies.Vault.FindVault(); err != nil {
			return err
		}
		token, err := dependencies.Preflight.Prepare(ctx, selected)
		if err != nil {
			return err
		}
		defer func() { token = "" }()

		_, err = dependencies.Vault.Execute(ctx, vaultexec.Invocation{
			Arguments:   append([]string(nil), vaultArguments...),
			Environment: vaultexec.ProfileEnvironment(selected.Address, token, selected.Namespace),
			Mode:        vaultexec.Delegated,
			Stdin:       dependencies.Stdin,
			Stdout:      dependencies.Stdout,
			Stderr:      dependencies.Stderr,
		})
		return err
	}
}

func parseDelegateArguments(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, errors.New("delegate Vault command: command is required")
	}
	if args[0] != "--profile" {
		return "", args, nil
	}
	if len(args) == 1 {
		return "", nil, errors.New("--profile requires a profile name")
	}
	if len(args) == 2 {
		return "", nil, errors.New("delegate Vault command: command is required after --profile NAME")
	}
	if err := profile.ValidateName(args[1]); err != nil {
		return "", nil, fmt.Errorf("--profile value is invalid: %w", err)
	}
	return args[1], args[2:], nil
}
