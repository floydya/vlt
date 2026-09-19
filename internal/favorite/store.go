package favorite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const schemaVersion = 1

var ErrDuplicateFavorite = errors.New("duplicate favorite")

type Configuration struct {
	Favorites []Favorite
}

type document struct {
	Version   *int       `json:"version"`
	Favorites []Favorite `json:"favorites"`
}

type saveDocument struct {
	Version   int        `json:"version"`
	Favorites []Favorite `json:"favorites"`
}

type Store struct {
	path          string
	renameFile    func(string, string) error
	syncDirectory func(string) error
}

func NewStore(path string) *Store {
	return &Store{
		path:          path,
		renameFile:    os.Rename,
		syncDirectory: syncDirectory,
	}
}

func (s *Store) Load(ctx context.Context) (Configuration, error) {
	if err := ctx.Err(); err != nil {
		return Configuration{}, err
	}
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Configuration{}, nil
	}
	if err != nil {
		return Configuration{}, fmt.Errorf("open favorites: %w", err)
	}
	defer func() { _ = file.Close() }()

	if runtime.GOOS != "windows" {
		info, err := file.Stat()
		if err != nil {
			return Configuration{}, fmt.Errorf("inspect favorite permissions: %w", err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return Configuration{}, errors.New("favorite file permissions must not allow group or other access")
		}
	}

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var stored document
	if err := decoder.Decode(&stored); err != nil {
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			return Configuration{}, errors.New("decode favorites: unknown field")
		}
		return Configuration{}, errors.New("decode favorites: invalid JSON or schema")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Configuration{}, errors.New("decode favorites: expected a single JSON document")
	}
	if stored.Version == nil || *stored.Version != schemaVersion {
		if stored.Version == nil {
			return Configuration{}, errors.New("favorite version is required")
		}
		return Configuration{}, errors.New("favorite version is unsupported")
	}
	configuration := Configuration{Favorites: stored.Favorites}
	if err := validate(configuration); err != nil {
		return Configuration{}, err
	}
	if err := ctx.Err(); err != nil {
		return Configuration{}, err
	}
	return configuration, nil
}

func (s *Store) Save(ctx context.Context, configuration Configuration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validate(configuration); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(saveDocument{
		Version:   schemaVersion,
		Favorites: configuration.Favorites,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode favorites: %w", err)
	}
	contents = append(contents, '\n')

	directory := filepath.Dir(s.path)
	if err := makePrivateDirectories(directory); err != nil {
		return fmt.Errorf("create favorite directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(s.path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary favorites: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	fail := func(operation string, operationErr error) error {
		_ = temporary.Close()
		return fmt.Errorf("%s favorites: %w", operation, operationErr)
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
		return fmt.Errorf("close temporary favorites: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.renameFile(temporaryPath, s.path); err != nil {
		return fmt.Errorf("rename temporary favorites: %w", err)
	}
	if err := s.syncDirectory(directory); err != nil {
		return fmt.Errorf("favorites were replaced but sync containing directory failed: %w", err)
	}
	return nil
}

func validate(configuration Configuration) error {
	identities := make(map[[3]string]struct{}, len(configuration.Favorites))
	for index, candidate := range configuration.Favorites {
		if err := candidate.Validate(); err != nil {
			return fmt.Errorf("favorite %d: %w", index+1, err)
		}
		identity := [3]string{candidate.Profile, candidate.Operation, candidate.Path}
		if _, found := identities[identity]; found {
			return ErrDuplicateFavorite
		}
		identities[identity] = struct{}{}
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

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}
