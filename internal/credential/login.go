package credential

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

type vaultExecutor interface {
	Execute(context.Context, vaultexec.Invocation) (vaultexec.Result, error)
}

type Authenticator struct {
	executor vaultExecutor
	store    Store
}

func NewAuthenticator(executor vaultExecutor, store Store) *Authenticator {
	return &Authenticator{executor: executor, store: store}
}

func (a *Authenticator) Login(ctx context.Context, selected profile.Profile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := selected.Validate(); err != nil {
		return fmt.Errorf("authenticate profile: invalid profile: %w", err)
	}
	if a == nil || a.executor == nil {
		return errors.New("authenticate profile: Vault executor is not configured")
	}
	if a.store == nil {
		return errors.New("authenticate profile: credential store is not configured")
	}

	result, err := a.executor.Execute(ctx, vaultexec.Invocation{
		Arguments: []string{
			"login",
			"-no-store",
			"-format=json",
			"-method=oidc",
			"-path=" + selected.AuthPath,
			"username=" + selected.Username,
		},
		Environment: vaultexec.LoginEnvironment(selected.Address, selected.Namespace),
		Mode:        vaultexec.Captured,
	})
	if err != nil {
		token := clientToken(result.Stdout)
		clear(result.Stdout)
		return loginFailure(ctx, "run Vault login", err, token)
	}

	token, err := parseClientToken(result.Stdout)
	clear(result.Stdout)
	if err != nil {
		return err
	}
	defer func() { token = "" }()

	if err := a.store.Set(ctx, selected.Name, token); err != nil {
		return loginFailure(ctx, "store authenticated credential", err, token)
	}
	return nil
}

func parseClientToken(output []byte) (string, error) {
	var response struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return "", errors.New("authenticate profile: Vault returned malformed login response")
	}
	if strings.TrimSpace(response.Auth.ClientToken) == "" {
		return "", errors.New("authenticate profile: Vault login response did not contain a client token")
	}
	return response.Auth.ClientToken, nil
}

func clientToken(output []byte) string {
	token, _ := parseClientToken(output)
	return token
}

func loginFailure(ctx context.Context, operation string, err error, sensitiveValues ...string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, ErrUnavailable) {
		return fmt.Errorf("authenticate profile: %s: %w", operation, ErrUnavailable)
	}
	diagnostic := vaultexec.RedactDiagnostic(err.Error(), sensitiveValues...)
	return errors.New("authenticate profile: " + operation + ": " + diagnostic)
}
