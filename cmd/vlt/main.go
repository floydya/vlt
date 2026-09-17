package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"vlt/internal/cli"
)

func main() {
	dispatcher := cli.NewDispatcher(cli.Dependencies{
		Output:  os.Stdout,
		Profile: unavailable("profile management"),
		Switch:  unavailable("profile switching"),
		Vault:   unavailable("Vault delegation"),
	})

	if err := dispatcher.Dispatch(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "vlt: %v\n", err)
		os.Exit(1)
	}
}

func unavailable(capability string) cli.Handler {
	return func(context.Context, []string) error {
		return errors.New(capability + " is not implemented yet")
	}
}
