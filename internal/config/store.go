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
	"strings"

	"vlt/internal/profile"
	"vlt/internal/securefile"
)

const schemaVersion = 1

// ErrDuplicateProfile identifies a configuration containing repeated profile names.
var ErrDuplicateProfile = errors.New("duplicate profile name")

type Configuration = profile.Configuration

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
	path          string
	renameFile    func(string, string) error
	syncDirectory func(string) error
}

// NewStore creates a configuration store for path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load reads and strictly validates the configuration. A missing file is an
// empty configuration.
func (s *Store) Load(ctx context.Context) (Configuration, error) {
	if err := ctx.Err(); err != nil {
		return Configuration{}, err
	}
	directory, err := securefile.OpenDirectory(filepath.Dir(s.path), false)
	if errors.Is(err, os.ErrNotExist) {
		return Configuration{}, nil
	}
	if err != nil {
		return Configuration{}, fmt.Errorf("open configuration directory: %w", err)
	}
	defer func() { _ = directory.Close() }()

	file, err := directory.OpenRegular(filepath.Base(s.path))
	if errors.Is(err, os.ErrNotExist) {
		return Configuration{}, nil
	}
	if err != nil {
		return Configuration{}, fmt.Errorf("open configuration: %w", err)
	}
	defer func() { _ = file.Close() }()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var stored document
	if err := decoder.Decode(&stored); err != nil {
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			return Configuration{}, errors.New("decode configuration: unknown field")
		}
		return Configuration{}, errors.New("decode configuration: invalid JSON or schema")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Configuration{}, errors.New("decode configuration: expected a single JSON document")
		}
		return Configuration{}, errors.New("decode configuration: expected a single JSON document")
	}
	if stored.Version == nil || *stored.Version != schemaVersion {
		if stored.Version == nil {
			return Configuration{}, errors.New("configuration version is required")
		}
		return Configuration{}, errors.New("configuration version is unsupported")
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

// SetActiveProfile resolves selector against one loaded snapshot and atomically
// persists the resolved stable profile name.
func (s *Store) SetActiveProfile(ctx context.Context, selector string) error {
	configuration, err := s.Load(ctx)
	if err != nil {
		return fmt.Errorf("load profiles for active selection: %w", err)
	}
	selected, err := profile.NewService(configuration.Profiles).Resolve(selector)
	if err != nil {
		return fmt.Errorf("resolve active profile: %w", err)
	}
	if configuration.ActiveProfile == selected.Name {
		return nil
	}
	configuration.ActiveProfile = selected.Name
	if err := s.Save(ctx, configuration); err != nil {
		return fmt.Errorf("persist active profile: %w", err)
	}
	return nil
}

// ClearActiveProfile removes the active selection without selecting a fallback.
func (s *Store) ClearActiveProfile(ctx context.Context) error {
	configuration, err := s.Load(ctx)
	if err != nil {
		return fmt.Errorf("load profiles for clearing active selection: %w", err)
	}
	if configuration.ActiveProfile == "" {
		return nil
	}
	configuration.ActiveProfile = ""
	if err := s.Save(ctx, configuration); err != nil {
		return fmt.Errorf("persist cleared active profile: %w", err)
	}
	return nil
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
	secureDirectory, err := securefile.OpenDirectory(directory, true)
	if err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	defer func() { _ = secureDirectory.Close() }()
	targetName := filepath.Base(s.path)
	if err := secureDirectory.ValidateTarget(targetName); err != nil {
		return fmt.Errorf("inspect existing configuration: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	temporary, temporaryName, err := secureDirectory.CreateTemp(targetName)
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	defer func() {
		_ = secureDirectory.Remove(temporaryName)
	}()

	fail := func(operation string, operationErr error) error {
		_ = temporary.Close()
		return fmt.Errorf("%s configuration: %w", operation, operationErr)
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
	replace := secureDirectory.Replace
	if s.renameFile != nil {
		replace = s.renameFile
	}
	if err := replace(temporaryName, targetName); err != nil {
		return fmt.Errorf("rename temporary configuration: %w", err)
	}
	sync := func(string) error { return secureDirectory.Sync() }
	if s.syncDirectory != nil {
		sync = s.syncDirectory
	}
	if err := sync(directory); err != nil {
		return fmt.Errorf("configuration was replaced but sync containing directory failed: %w", err)
	}
	return nil
}

func validate(configuration Configuration) error {
	names := make(map[string]struct{}, len(configuration.Profiles))
	for index, candidate := range configuration.Profiles {
		if err := candidate.Validate(); err != nil {
			return fmt.Errorf("profile %d: %w", index+1, err)
		}
		if _, found := names[candidate.Name]; found {
			return ErrDuplicateProfile
		}
		names[candidate.Name] = struct{}{}
	}
	if configuration.ActiveProfile != "" {
		if err := profile.ValidateName(configuration.ActiveProfile); err != nil {
			return fmt.Errorf("active profile: %w", err)
		}
		if _, found := names[configuration.ActiveProfile]; !found {
			return errors.New("active profile does not exist")
		}
	}
	return nil
}
