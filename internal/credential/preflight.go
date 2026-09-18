package credential

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

const renewalWindow = 5 * time.Minute
const maxTTLSeconds = (1<<63 - 1) / int64(time.Second)

type profileAuthenticator interface {
	Login(context.Context, profile.Profile) error
}

type Preflight struct {
	executor      vaultExecutor
	store         Store
	authenticator profileAuthenticator
	now           func() time.Time
	warnings      io.Writer
}

func NewPreflight(executor vaultExecutor, store Store, authenticator profileAuthenticator, now func() time.Time, warnings io.Writer) *Preflight {
	return &Preflight{
		executor:      executor,
		store:         store,
		authenticator: authenticator,
		now:           now,
		warnings:      warnings,
	}
}

func (p *Preflight) Prepare(ctx context.Context, selected profile.Profile) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := selected.Validate(); err != nil {
		return "", fmt.Errorf("credential preflight: invalid profile: %w", err)
	}
	if err := p.validateDependencies(); err != nil {
		return "", err
	}

	token, err := p.store.Get(ctx, selected.Name)
	if errors.Is(err, ErrNotFound) {
		return p.authenticate(ctx, selected)
	}
	if err != nil {
		return "", fmt.Errorf("credential preflight: load credential: %w", err)
	}
	if token == "" {
		return p.authenticate(ctx, selected)
	}

	result, err := p.executor.Execute(ctx, vaultexec.Invocation{
		Arguments:   []string{"token", "lookup", "-format=json"},
		Environment: vaultexec.ProfileEnvironment(selected.Address, token, selected.Namespace),
		Mode:        vaultexec.Captured,
	})
	if err != nil {
		clear(result.Stdout)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if explicitlyInvalid(result.Stderr, err) {
			return p.authenticate(ctx, selected)
		}
		return "", preflightVaultError("validate credential", err, token)
	}

	checkedAt := p.now()
	lookup, err := parseTokenLookup(result.Stdout, checkedAt)
	clear(result.Stdout)
	if err != nil {
		return "", err
	}
	if !lookup.expiry.IsZero() && !lookup.expiry.After(checkedAt) {
		return p.authenticate(ctx, selected)
	}
	if !lookup.renewable || lookup.expiry.IsZero() || lookup.expiry.Sub(checkedAt) > renewalWindow {
		return token, nil
	}

	result, err = p.executor.Execute(ctx, vaultexec.Invocation{
		Arguments:   []string{"token", "renew", "-format=json"},
		Environment: vaultexec.ProfileEnvironment(selected.Address, token, selected.Namespace),
		Mode:        vaultexec.Captured,
	})
	clear(result.Stdout)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	if err == nil {
		return token, nil
	}
	if lookup.expiry.After(p.now()) {
		fmt.Fprintln(p.warnings, "warning: Vault token renewal failed; continuing with the existing credential")
		return token, nil
	}
	return p.authenticate(ctx, selected)
}

func (p *Preflight) validateDependencies() error {
	if p == nil {
		return errors.New("credential preflight is not configured")
	}
	if p.executor == nil {
		return errors.New("credential preflight: Vault executor is not configured")
	}
	if p.store == nil {
		return errors.New("credential preflight: credential store is not configured")
	}
	if p.authenticator == nil {
		return errors.New("credential preflight: authenticator is not configured")
	}
	if p.now == nil {
		return errors.New("credential preflight: clock is not configured")
	}
	if p.warnings == nil {
		return errors.New("credential preflight: warning output is not configured")
	}
	return nil
}

func (p *Preflight) authenticate(ctx context.Context, selected profile.Profile) (string, error) {
	if err := p.authenticator.Login(ctx, selected); err != nil {
		return "", fmt.Errorf("credential preflight: authenticate: %w", err)
	}
	token, err := p.store.Get(ctx, selected.Name)
	if err != nil {
		return "", fmt.Errorf("credential preflight: load authenticated credential: %w", err)
	}
	if token == "" {
		return "", errors.New("credential preflight: authentication did not store a credential")
	}
	return token, nil
}

type tokenLookup struct {
	expiry    time.Time
	renewable bool
}

func parseTokenLookup(output []byte, now time.Time) (tokenLookup, error) {
	var response struct {
		Data *struct {
			ExpireTime *string `json:"expire_time"`
			Renewable  bool    `json:"renewable"`
			TTL        *int64  `json:"ttl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &response); err != nil || response.Data == nil {
		return tokenLookup{}, errors.New("credential preflight: Vault returned malformed token lookup response")
	}

	lookup := tokenLookup{renewable: response.Data.Renewable}
	if response.Data.ExpireTime != nil && *response.Data.ExpireTime != "" {
		expiry, err := time.Parse(time.RFC3339Nano, *response.Data.ExpireTime)
		if err != nil {
			return tokenLookup{}, errors.New("credential preflight: Vault returned malformed token expiry")
		}
		lookup.expiry = expiry
		return lookup, nil
	}
	if response.Data.TTL != nil {
		switch {
		case *response.Data.TTL < 0:
			lookup.expiry = now.Add(-time.Nanosecond)
		case *response.Data.TTL > maxTTLSeconds:
			return tokenLookup{}, errors.New("credential preflight: Vault returned malformed token TTL")
		case *response.Data.TTL > 0:
			lookup.expiry = now.Add(time.Duration(*response.Data.TTL) * time.Second)
		}
	}
	if lookup.renewable && lookup.expiry.IsZero() {
		return tokenLookup{}, errors.New("credential preflight: Vault returned renewable token without an expiry")
	}
	return lookup, nil
}

func explicitlyInvalid(stderr []byte, err error) bool {
	diagnostic := strings.ToLower(string(stderr))
	if diagnostic == "" && err != nil {
		diagnostic = strings.ToLower(err.Error())
	}
	return strings.Contains(diagnostic, "invalid token") ||
		strings.Contains(diagnostic, "token is expired") ||
		strings.Contains(diagnostic, "token expired")
}

func preflightVaultError(operation string, err error, token string) error {
	diagnostic := vaultexec.RedactDiagnostic(err.Error(), token)
	return errors.New("credential preflight: " + operation + ": " + diagnostic)
}
