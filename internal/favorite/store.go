package favorite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"vlt/internal/securefile"
)

const schemaVersion = 2

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
	return &Store{path: path}
}

func (s *Store) Load(ctx context.Context) (Configuration, error) {
	if err := ctx.Err(); err != nil {
		return Configuration{}, err
	}
	directory, err := securefile.OpenDirectory(filepath.Dir(s.path), false)
	if errors.Is(err, os.ErrNotExist) {
		return Configuration{}, nil
	}
	if err != nil {
		return Configuration{}, fmt.Errorf("open favorite directory: %w", err)
	}
	defer func() { _ = directory.Close() }()

	file, err := directory.OpenRegular(filepath.Base(s.path))
	if errors.Is(err, os.ErrNotExist) {
		return Configuration{}, nil
	}
	if err != nil {
		return Configuration{}, fmt.Errorf("open favorites: %w", err)
	}
	defer func() { _ = file.Close() }()

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
	if stored.Version == nil || (*stored.Version != 1 && *stored.Version != schemaVersion) {
		if stored.Version == nil {
			return Configuration{}, errors.New("favorite version is required")
		}
		return Configuration{}, errors.New("favorite version is unsupported")
	}
	configuration := Configuration{Favorites: stored.Favorites}
	if err := validate(configuration, *stored.Version == schemaVersion); err != nil {
		return Configuration{}, err
	}
	if *stored.Version == 1 {
		assignMissingIDs(configuration.Favorites)
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
	configuration.Favorites = append([]Favorite(nil), configuration.Favorites...)
	assignMissingIDs(configuration.Favorites)
	if err := validate(configuration, true); err != nil {
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
	secureDirectory, err := securefile.OpenDirectory(directory, true)
	if err != nil {
		return fmt.Errorf("create favorite directory: %w", err)
	}
	defer func() { _ = secureDirectory.Close() }()
	targetName := filepath.Base(s.path)
	if err := secureDirectory.ValidateTarget(targetName); err != nil {
		return fmt.Errorf("inspect existing favorites: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	temporary, temporaryName, err := secureDirectory.CreateTemp(targetName)
	if err != nil {
		return fmt.Errorf("create temporary favorites: %w", err)
	}
	defer func() { _ = secureDirectory.Remove(temporaryName) }()

	fail := func(operation string, operationErr error) error {
		_ = temporary.Close()
		return fmt.Errorf("%s favorites: %w", operation, operationErr)
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
	replace := secureDirectory.Replace
	if s.renameFile != nil {
		replace = s.renameFile
	}
	if err := replace(temporaryName, targetName); err != nil {
		return fmt.Errorf("rename temporary favorites: %w", err)
	}
	sync := func(string) error { return secureDirectory.Sync() }
	if s.syncDirectory != nil {
		sync = s.syncDirectory
	}
	if err := sync(directory); err != nil {
		return fmt.Errorf("favorites were replaced but sync containing directory failed: %w", err)
	}
	return nil
}

func validate(configuration Configuration, requireIDs bool) error {
	identities := make(map[[3]string]struct{}, len(configuration.Favorites))
	ids := make(map[string]struct{}, len(configuration.Favorites))
	for index, candidate := range configuration.Favorites {
		if err := candidate.Validate(); err != nil {
			return fmt.Errorf("favorite %d: %w", index+1, err)
		}
		if requireIDs && candidate.ID == "" {
			return fmt.Errorf("favorite %d: ID is required", index+1)
		}
		if candidate.ID != "" {
			if _, found := ids[candidate.ID]; found {
				return errors.New("favorite ID is duplicated")
			}
			ids[candidate.ID] = struct{}{}
		}
		identity := [3]string{candidate.Profile, candidate.Operation, candidate.Path}
		if _, found := identities[identity]; found {
			return ErrDuplicateFavorite
		}
		identities[identity] = struct{}{}
	}
	return nil
}
