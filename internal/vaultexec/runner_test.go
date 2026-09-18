package vaultexec

import (
	"context"
	"reflect"
	"testing"
)

func TestLoginEnvironmentSetsProfileValuesAndRemovesInheritedCredentials(t *testing.T) {
	runner := &recordingRunner{}
	executor := NewExecutor(Dependencies{
		LookPath: func(string) (string, error) { return "/test/vault", nil },
		Environment: func() []string {
			return []string{
				"VAULT_ADDR=https://inherited.example",
				"VAULT_TOKEN=inherited-token",
				"VAULT_NAMESPACE=inherited-namespace",
				"OTHER=preserved",
			}
		},
		Runner: runner,
	})

	_, err := executor.Execute(context.Background(), Invocation{
		Environment: LoginEnvironment("https://profile.example", "profile-namespace"),
		Mode:        Captured,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := []string{
		"OTHER=preserved",
		"VAULT_ADDR=https://profile.example",
		"VAULT_NAMESPACE=profile-namespace",
	}
	if !reflect.DeepEqual(runner.command.Environment, want) {
		t.Fatalf("login environment = %#v, want %#v", runner.command.Environment, want)
	}
}

func TestLoginEnvironmentRemovesInheritedNamespaceWhenProfileHasNone(t *testing.T) {
	runner := &recordingRunner{}
	executor := NewExecutor(Dependencies{
		LookPath: func(string) (string, error) { return "/test/vault", nil },
		Environment: func() []string {
			return []string{
				"VAULT_TOKEN=inherited-token",
				"VAULT_NAMESPACE=inherited-namespace",
			}
		},
		Runner: runner,
	})

	_, err := executor.Execute(context.Background(), Invocation{
		Environment: LoginEnvironment("https://profile.example", ""),
		Mode:        Captured,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := []string{"VAULT_ADDR=https://profile.example"}
	if !reflect.DeepEqual(runner.command.Environment, want) {
		t.Fatalf("login environment = %#v, want %#v", runner.command.Environment, want)
	}
}
