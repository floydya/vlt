// Package credential stores Vault credentials exclusively in the operating
// system's native credential store.
package credential

import (
	"context"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"

	"vlt/internal/profile"
)

const serviceName = "vlt"

var (
	// ErrNotFound indicates that a profile has no stored credential.
	ErrNotFound = profile.ErrCredentialNotFound
	// ErrUnavailable indicates that the native credential store could not
	// complete an operation. Backend details are intentionally omitted because
	// they may contain credential values.
	ErrUnavailable = errors.New("native credential store is unavailable or locked")
)

// Store is the credential boundary used by profile and Vault workflows.
type Store interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string) error
	Delete(context.Context, string) error
}

// NativeStore maps stable profile names to entries in the operating system's
// native credential store under the vlt service namespace.
type NativeStore struct {
	backend  backend
	notFound error
}

// NewNativeStore creates a credential store backed by Secret Service on Linux,
// Keychain on macOS, and Credential Manager on Windows.
func NewNativeStore() *NativeStore {
	return newStore(nativeBackend{}, keyring.ErrNotFound)
}

func newStore(backend backend, notFound error) *NativeStore {
	return &NativeStore{backend: backend, notFound: notFound}
}

// Get loads the credential stored for profileName.
func (s *NativeStore) Get(ctx context.Context, profileName string) (string, error) {
	if err := validateRequest(ctx, profileName); err != nil {
		return "", err
	}
	credential, err := s.backend.Get(serviceName, profileName)
	if errors.Is(err, s.notFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("load credential: %w", ErrUnavailable)
	}
	return credential, nil
}

// Set creates or replaces the credential stored for profileName.
func (s *NativeStore) Set(ctx context.Context, profileName, credential string) error {
	if err := validateRequest(ctx, profileName); err != nil {
		return err
	}
	if credential == "" {
		return errors.New("credential token must not be empty")
	}
	if err := s.backend.Set(serviceName, profileName, credential); err != nil {
		return fmt.Errorf("store credential: %w", ErrUnavailable)
	}
	return nil
}

// Delete removes the credential stored for profileName.
func (s *NativeStore) Delete(ctx context.Context, profileName string) error {
	if err := validateRequest(ctx, profileName); err != nil {
		return err
	}
	if err := s.backend.Delete(serviceName, profileName); errors.Is(err, s.notFound) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("delete credential: %w", ErrUnavailable)
	}
	return nil
}

func validateRequest(ctx context.Context, profileName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := profile.ValidateName(profileName); err != nil {
		return fmt.Errorf("profile identity is invalid: %w", err)
	}
	return nil
}

type backend interface {
	Get(service, account string) (string, error)
	Set(service, account, credential string) error
	Delete(service, account string) error
}

type nativeBackend struct{}

func (nativeBackend) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

func (nativeBackend) Set(service, account, credential string) error {
	return keyring.Set(service, account, credential)
}

func (nativeBackend) Delete(service, account string) error {
	return keyring.Delete(service, account)
}
