package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"vlt/internal/cli"
	"vlt/internal/config"
	"vlt/internal/credential"
	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return dispatch(ctx, cli.NewDispatcher(cli.Dependencies{Output: stdout}), args, stderr)
	}
	dispatcher, err := newDispatcher(stdin, stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "vlt: %v\n", err)
		return 1
	}
	return dispatch(ctx, dispatcher, args, stderr)
}

func newDispatcher(stdin io.Reader, stdout, stderr io.Writer) (*cli.Dispatcher, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("locate user configuration directory: %w", err)
	}
	return newDispatcherAt(configDirectory, stdin, stdout, stderr), nil
}

func newDispatcherAt(configDirectory string, stdin io.Reader, stdout, stderr io.Writer) *cli.Dispatcher {
	profiles := config.NewStore(filepath.Join(configDirectory, "vlt", "profiles.json"))
	terminal := cli.NewTerminal(stdin, stdout, os.Environ())
	selector := cli.NewHuhProfileSelector(stdin, stdout)
	form := cli.NewHuhProfileForm(stdin, stdout)
	removalConfirmer := cli.NewHuhProfileRemovalConfirmer(stdin, stdout)
	vault := vaultexec.NewOSExecutor()
	credentials := credential.NewNativeStore()
	authenticator := credential.NewAuthenticator(vault, credentials)
	mutations := profile.NewMutationService(profiles, credentials, authenticator)
	preflight := credential.NewPreflight(vault, credentials, authenticator, time.Now, stderr)
	delegate := cli.NewDelegateHandler(cli.DelegateDependencies{
		Profiles:  profiles,
		Preflight: preflight,
		Vault:     vault,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
	})

	return cli.NewDispatcher(cli.Dependencies{
		Output: stdout,
		Profile: cli.NewProfileHandler(cli.ProfileDependencies{
			Profiles: profiles, Mutations: mutations, Output: stdout, Terminal: terminal,
			Selector: selector, Form: form, RemovalConfirmer: removalConfirmer,
		}),
		Switch:     cli.NewSwitchHandler(cli.SwitchDependencies{Profiles: profiles, Output: stdout, Terminal: terminal, Selector: selector}),
		Completion: cli.NewCompletionHandler(cli.CompletionDependencies{Profiles: profiles, Output: stdout}),
		Vault:      delegate,
	})
}

func dispatch(ctx context.Context, dispatcher *cli.Dispatcher, args []string, stderr io.Writer) int {
	if err := dispatcher.Dispatch(ctx, args); err != nil {
		var automaticHelp cli.AutomaticHelp
		if errors.As(err, &automaticHelp) {
			_, _ = fmt.Fprint(stderr, automaticHelp.Text)
			return 1
		}
		_, _ = fmt.Fprintf(stderr, "vlt: %v\n", err)
		return vaultexec.ExitCode(err)
	}
	return 0
}

func unavailable(capability string) cli.Handler {
	return func(context.Context, []string) error {
		return errors.New(capability + " is not implemented yet")
	}
}
