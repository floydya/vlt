package favorite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"vlt/internal/profile"
)

const (
	OperationRead  = "read"
	OperationKVGet = "kv-get"
)

type Favorite struct {
	ID        string `json:"id"`
	Profile   string `json:"profile"`
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Note      string `json:"note"`
	RunCount  int64  `json:"run_count,omitempty"`
}

func (f *Favorite) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if count, found := fields["run_count"]; found && bytes.Equal(bytes.TrimSpace(count), []byte("null")) {
		return errors.New("json: invalid run count")
	}
	type favoriteJSON Favorite
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded favoriteJSON
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*f = Favorite(decoded)
	return nil
}

func (f Favorite) Validate() error {
	if f.ID != "" && !validID(f.ID) {
		return errors.New("favorite ID is invalid")
	}
	if err := profile.ValidateName(f.Profile); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if f.Operation != OperationRead && f.Operation != OperationKVGet {
		return errors.New("operation must be read or kv-get")
	}
	if strings.TrimSpace(f.Path) == "" {
		return errors.New("path must not be empty or whitespace-only")
	}
	if f.RunCount < 0 {
		return errors.New("run count must not be negative")
	}
	return nil
}

func (f Favorite) SameIdentity(other Favorite) bool {
	return f.Profile == other.Profile && f.Operation == other.Operation && f.Path == other.Path
}

type Service struct {
	favorites []Favorite
}

func NewService(favorites []Favorite) Service {
	return Service{favorites: append([]Favorite(nil), favorites...)}
}

func (s Service) List() []Favorite {
	favorites := append([]Favorite(nil), s.favorites...)
	sort.Slice(favorites, func(left, right int) bool {
		if favorites[left].RunCount != favorites[right].RunCount {
			return favorites[left].RunCount > favorites[right].RunCount
		}
		if favorites[left].Path != favorites[right].Path {
			return favorites[left].Path < favorites[right].Path
		}
		if favorites[left].Profile != favorites[right].Profile {
			return favorites[left].Profile < favorites[right].Profile
		}
		return favorites[left].Operation < favorites[right].Operation
	})
	return favorites
}

func (s Service) Resolve(selector string) (Favorite, error) {
	if strings.HasPrefix(selector, "f_") {
		if !validID(selector) {
			return Favorite{}, errors.New("favorite ID is invalid")
		}
		for _, candidate := range s.favorites {
			if candidate.ID == selector {
				return candidate, nil
			}
		}
		return Favorite{}, errors.New("favorite ID is unknown")
	}
	if !isCanonicalIndex(selector) {
		return Favorite{}, errors.New("favorite selector is invalid")
	}
	index, err := strconv.ParseUint(selector, 10, 64)
	if err != nil {
		return Favorite{}, errors.New("favorite selector is out of range")
	}
	favorites := s.List()
	if index > uint64(len(favorites)) {
		return Favorite{}, errors.New("favorite selector is out of range")
	}
	return favorites[index-1], nil
}

func isCanonicalIndex(selector string) bool {
	if selector == "" || selector[0] < '1' || selector[0] > '9' {
		return false
	}
	for index := 1; index < len(selector); index++ {
		if selector[index] < '0' || selector[index] > '9' {
			return false
		}
	}
	return true
}
