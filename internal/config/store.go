// Package config persists non-secret Vault profile configuration.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"vlt/internal/profile"
)

const schemaVersion = 1

// ErrDuplicateProfile identifies a configuration containing repeated profile names.
var ErrDuplicateProfile = errors.New("duplicate profile name")

// Configuration is the persisted profile collection and active selection.
type Configuration struct {
	Profiles      []profile.Profile
	ActiveProfile string
}

type document struct {
	Version       *int              `json:"version"`
	Profiles      []profile.Profile `json:"profiles"`
	ActiveProfile string            `json:"active_profile"`
}

type saveDocument struct {
	Version       int               `json:"version"`
	Profiles      []profile.Profile `json:"profiles"`
	ActiveProfile string            `json:"active_profile"`
}

// Store loads and atomically saves a configuration at one path.
type Store struct {
	path       string
	renameFile func(string, string) error
}

// NewStore creates a configuration store for path.
func NewStore(path string) *Store {
	return &Store{path: path, renameFile: os.Rename}
}

// Load reads and strictly validates the configuration. A missing file is an
// empty configuration.
func (s *Store) Load(ctx context.Context) (Configuration, error) {
	if err := ctx.Err(); err != nil {
		return Configuration{}, err
	}
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Configuration{}, nil
	}
	if err != nil {
		return Configuration{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var stored document
	if err := decoder.Decode(&stored); err != nil {
		return Configuration{}, fmt.Errorf("decode configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Configuration{}, errors.New("decode configuration: expected a single JSON document")
		}
		return Configuration{}, fmt.Errorf("decode configuration: expected a single JSON document: %w", err)
	}
	if stored.Version == nil || *stored.Version != schemaVersion {
		if stored.Version == nil {
			return Configuration{}, errors.New("configuration version is required")
		}
		return Configuration{}, fmt.Errorf("configuration version %d is unsupported", *stored.Version)
	}
	configuration := Configuration{Profiles: stored.Profiles, ActiveProfile: stored.ActiveProfile}
	if err := validate(configuration); err != nil {
		return Configuration{}, err
	}
	if err := ctx.Err(); err != nil {
		return Configuration{}, err
	}
	return configuration, nil
}

// Save validates and atomically replaces the persisted configuration.
func (s *Store) Save(ctx context.Context, configuration Configuration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validate(configuration); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(saveDocument{
		Version:       schemaVersion,
		Profiles:      configuration.Profiles,
		ActiveProfile: configuration.ActiveProfile,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	contents = append(contents, '\n')

	directory := filepath.Dir(s.path)
	if err := makePrivateDirectories(directory); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(s.path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := false
	defer func() {
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	fail := func(operation string, operationErr error) error {
		_ = temporary.Close()
		return fmt.Errorf("%s configuration: %w", operation, operationErr)
	}
	if err := temporary.Chmod(0o600); err != nil {
		return fail("set temporary permissions for", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		return fail("write temporary", err)
	}
	if err := temporary.Sync(); err != nil {
		return fail("sync temporary", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.renameFile(temporaryPath, s.path); err != nil {
		return fmt.Errorf("rename temporary configuration: %w", err)
	}
	keepTemporary = true // The temporary path is now the destination path.
	return nil
}

func validate(configuration Configuration) error {
	names := make(map[string]struct{}, len(configuration.Profiles))
	for index, candidate := range configuration.Profiles {
		if err := candidate.Validate(); err != nil {
			return fmt.Errorf("profile %d: %w", index+1, err)
		}
		if _, found := names[candidate.Name]; found {
			return fmt.Errorf("%w %q", ErrDuplicateProfile, candidate.Name)
		}
		names[candidate.Name] = struct{}{}
	}
	if configuration.ActiveProfile != "" {
		if err := profile.ValidateName(configuration.ActiveProfile); err != nil {
			return fmt.Errorf("active profile: %w", err)
		}
		if _, found := names[configuration.ActiveProfile]; !found {
			return fmt.Errorf("active profile %q does not exist", configuration.ActiveProfile)
		}
	}
	return nil
}

func makePrivateDirectories(path string) error {
	var missing []string
	for current := path; ; current = filepath.Dir(current) {
		_, err := os.Stat(current)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	for _, directory := range missing {
		if err := os.Chmod(directory, 0o700); err != nil {
			return err
		}
	}
	return nil
}
