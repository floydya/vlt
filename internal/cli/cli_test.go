package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDispatcherRoutesCommands(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		wantRoute string
		wantArgs  []string
	}{
		{
			name:      "profile command is reserved",
			arguments: []string{"profile", "add", "team-a", "--address", "https://vault.example.com"},
			wantRoute: "profile",
			wantArgs:  []string{"add", "team-a", "--address", "https://vault.example.com"},
		},
		{
			name:      "switch command is reserved",
			arguments: []string{"switch", "team-a"},
			wantRoute: "switch",
			wantArgs:  []string{"team-a"},
		},
		{
			name:      "completion command is reserved",
			arguments: []string{"completion", "bash", "unchanged"},
			wantRoute: "completion",
			wantArgs:  []string{"bash", "unchanged"},
		},
		{
			name:      "favorite command is reserved",
			arguments: []string{"favorite", "list"},
			wantRoute: "favorite",
			wantArgs:  []string{"list"},
		},
		{
			name:      "similar favorite command remains opaque",
			arguments: []string{"favorites", "list"},
			wantRoute: "vault",
			wantArgs:  []string{"favorites", "list"},
		},
		{
			name:      "similar top-level command remains opaque",
			arguments: []string{"complete", "status"},
			wantRoute: "vault",
			wantArgs:  []string{"complete", "status"},
		},
		{
			name:      "vault command remains opaque",
			arguments: []string{"kv", "put", "secret/example", "value=a b", "--format=json"},
			wantRoute: "vault",
			wantArgs:  []string{"kv", "put", "secret/example", "value=a b", "--format=json"},
		},
		{
			name:      "profile override is routed unchanged to vault",
			arguments: []string{"--profile", "team-b", "read", "secret/example"},
			wantRoute: "vault",
			wantArgs:  []string{"--profile", "team-b", "read", "secret/example"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotRoute string
			var gotArgs []string
			record := func(route string) Handler {
				return func(_ context.Context, args []string) error {
					gotRoute = route
					gotArgs = append([]string(nil), args...)
					return nil
				}
			}

			dispatcher := NewDispatcher(Dependencies{
				Output:     &bytes.Buffer{},
				Profile:    record("profile"),
				Switch:     record("switch"),
				Favorite:   record("favorite"),
				Completion: record("completion"),
				Vault:      record("vault"),
			})

			if err := dispatcher.Dispatch(context.Background(), tt.arguments); err != nil {
				t.Fatalf("Dispatch() error = %v", err)
			}
			if gotRoute != tt.wantRoute {
				t.Errorf("route = %q, want %q", gotRoute, tt.wantRoute)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("arguments = %#v, want %#v", gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestDispatcherDisplaysHelpWithoutCallingAHandler(t *testing.T) {
	for _, arguments := range [][]string{{"--help"}, {"-h"}} {
		arguments := arguments
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			var output bytes.Buffer
			called := false
			handler := func(context.Context, []string) error {
				called = true
				return nil
			}
			dispatcher := NewDispatcher(Dependencies{
				Output:  &output,
				Profile: handler,
				Switch:  handler,
				Vault:   handler,
			})

			if err := dispatcher.Dispatch(context.Background(), arguments); err != nil {
				t.Fatalf("Dispatch() error = %v", err)
			}
			if called {
				t.Fatal("Dispatch() called a handler for help")
			}
			for _, want := range []string{"Usage: vlt", "Commands:", "favorite", "completion", "Examples:"} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("help output = %q, want text %q", output.String(), want)
				}
			}
		})
	}
}

func TestDispatcherReturnsAutomaticHelpWithoutCallingAHandler(t *testing.T) {
	var output bytes.Buffer
	called := false
	handler := func(context.Context, []string) error {
		called = true
		return nil
	}
	dispatcher := NewDispatcher(Dependencies{
		Output:     &output,
		Profile:    handler,
		Switch:     handler,
		Completion: handler,
		Vault:      handler,
	})

	err := dispatcher.Dispatch(context.Background(), nil)

	var automaticHelp AutomaticHelp
	if !errors.As(err, &automaticHelp) {
		t.Fatalf("Dispatch() error = %v, want AutomaticHelp", err)
	}
	if got, want := automaticHelp.Text, helpText; got != want {
		t.Errorf("automatic help = %q, want %q", got, want)
	}
	if output.Len() != 0 {
		t.Errorf("output = %q, want empty", output.String())
	}
	if called {
		t.Fatal("Dispatch() called a handler for automatic help")
	}
}

func TestDispatcherReturnsHandlerError(t *testing.T) {
	wantErr := errors.New("profile unavailable")
	dispatcher := NewDispatcher(Dependencies{
		Output:  &bytes.Buffer{},
		Profile: func(context.Context, []string) error { return wantErr },
		Switch:  func(context.Context, []string) error { return nil },
		Vault:   func(context.Context, []string) error { return nil },
	})

	err := dispatcher.Dispatch(context.Background(), []string{"profile", "list"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Dispatch() error = %v, want %v", err, wantErr)
	}
}
