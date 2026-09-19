package cli

import (
	"context"
	"fmt"
	"io"
)

// Handler runs one command family with the arguments following its route.
type Handler func(context.Context, []string) error

type AutomaticHelp struct {
	Text string
}

func (h AutomaticHelp) Error() string {
	return h.Text
}

// Dependencies are the command handlers and output boundary used by Dispatcher.
type Dependencies struct {
	Output     io.Writer
	Profile    Handler
	Switch     Handler
	Favorite   Handler
	Completion Handler
	Vault      Handler
}

// Dispatcher routes vlt management commands and leaves Vault arguments opaque.
type Dispatcher struct {
	dependencies Dependencies
}

// NewDispatcher constructs a dispatcher with explicit command dependencies.
func NewDispatcher(dependencies Dependencies) *Dispatcher {
	return &Dispatcher{dependencies: dependencies}
}

// Dispatch routes one invocation to the appropriate handler.
func (d *Dispatcher) Dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return AutomaticHelp{Text: helpText}
	}
	if args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprint(d.dependencies.Output, helpText)
		return err
	}

	switch args[0] {
	case "profile":
		return d.dependencies.Profile(ctx, args[1:])
	case "switch":
		return d.dependencies.Switch(ctx, args[1:])
	case "favorite":
		return d.dependencies.Favorite(ctx, args[1:])
	case "completion":
		return d.dependencies.Completion(ctx, args[1:])
	default:
		return d.dependencies.Vault(ctx, args)
	}
}

const helpText = `Manage Vault profiles and delegate Vault commands.

Usage: vlt [--profile NAME] VAULT_ARGUMENT...
       vlt profile COMMAND [ARGUMENT...]
       vlt switch [NAME|NUMBER]
       vlt favorite COMMAND [ARGUMENT...]
       vlt completion SHELL

Commands:
  profile     Manage Vault profiles
  switch      Show or select the active profile
  favorite    Manage favorite Vault read targets
  completion  Generate shell completion

Options:
  -h, --help  Show help

Examples:
  vlt profile list
  vlt switch team-a
  vlt favorite list
  vlt completion bash
  vlt status

Every other command is forwarded to the official Vault CLI.
`
