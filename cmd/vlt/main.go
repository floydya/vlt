package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"vlt/internal/cli"
	"vlt/internal/config"
	"vlt/internal/credential"
	"vlt/internal/favorite"
	"vlt/internal/profile"
	"vlt/internal/statelock"
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
		_, _ = fmt.Fprintln(stderr, processDiagnostic(err))
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
	favorites := favorite.NewStore(filepath.Join(configDirectory, "vlt", "favorites.json"))
	mutationLock := statelock.New(filepath.Join(configDirectory, "vlt"))
	terminal := cli.NewTerminal(stdin, stdout, os.Environ())
	if terminal.PromptsEnabled() && terminal.ColorEnabled() {
		terminal = cli.WithAccent(terminal, activeProfileAccent(profiles))
	}
	sharedSelector := cli.NewSharedSelector(stdin, stdout, terminal)
	selector := cli.NewSharedProfileSelector(sharedSelector)
	favoriteSelector := cli.NewSharedFavoriteSelector(sharedSelector)
	form := cli.NewHuhProfileForm(stdin, stdout, terminal)
	removalConfirmer := cli.NewHuhProfileRemovalConfirmer(stdin, stdout, terminal)
	favoriteManagementSelector := cli.NewSharedFavoriteManagementSelector(sharedSelector)
	favoriteForm := cli.NewHuhFavoriteForm(stdin, stdout, sharedSelector, terminal)
	favoriteRemovalConfirmer := cli.NewHuhFavoriteRemovalConfirmer(stdin, stdout, terminal)
	vault := vaultexec.NewOSExecutor()
	credentials := credential.NewNativeStore()
	authenticator := credential.NewAuthenticator(vault, credentials)
	mutations := profile.NewMutationService(profiles, credentials, authenticator)
	favoriteMutations := favorite.NewMutationService(favorites, profiles)
	cascade := favorite.NewCascadeService(favorites, mutations)
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
			Profiles: profiles, Mutations: mutations, Lock: mutationLock, Output: stdout, Terminal: terminal,
			Selector: selector, Form: form, RemovalConfirmer: removalConfirmer, Cascade: cascade,
		}),
		Switch: cli.NewSwitchHandler(cli.SwitchDependencies{Profiles: profiles, Lock: mutationLock, Output: stdout, Terminal: terminal, Selector: selector}),
		Favorite: cli.NewFavoriteHandler(cli.FavoriteDependencies{
			Profiles: profiles, Favorites: favorites, Mutations: favoriteMutations, Recorder: favoriteMutations, Lock: mutationLock, Output: stdout, Terminal: terminal,
			Selector: favoriteSelector, Vault: delegate, ManagementSelector: favoriteManagementSelector,
			Form: favoriteForm, RemovalConfirmer: favoriteRemovalConfirmer,
		}),
		Completion: cli.NewCompletionHandler(cli.CompletionDependencies{Profiles: profiles, Output: stdout}),
		Vault:      delegate,
	})
}

func activeProfileAccent(store *config.Store) string {
	configuration, err := store.Load(context.Background())
	if err != nil || configuration.ActiveProfile == "" {
		return ""
	}
	for _, candidate := range configuration.Profiles {
		if candidate.Name == configuration.ActiveProfile {
			return candidate.Color
		}
	}
	return ""
}

func dispatch(ctx context.Context, dispatcher *cli.Dispatcher, args []string, stderr io.Writer) int {
	if err := dispatcher.Dispatch(ctx, args); err != nil {
		var automaticHelp cli.AutomaticHelp
		if errors.As(err, &automaticHelp) {
			_, _ = fmt.Fprint(stderr, automaticHelp.Text)
			return 1
		}
		_, _ = fmt.Fprintln(stderr, processDiagnostic(err))
		return vaultexec.ExitCode(err)
	}
	return 0
}

func processDiagnostic(err error) string {
	diagnostic := ansi.Strip(err.Error())
	for strings.HasPrefix(diagnostic, "vlt: ") {
		diagnostic = strings.TrimPrefix(diagnostic, "vlt: ")
	}
	return "vlt: " + diagnostic
}

func unavailable(capability string) cli.Handler {
	return func(context.Context, []string) error {
		return errors.New(capability + " is not implemented yet")
	}
}
