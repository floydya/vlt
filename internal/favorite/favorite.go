package favorite

import (
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
	Profile   string `json:"profile"`
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Note      string `json:"note"`
}

func (f Favorite) Validate() error {
	if err := profile.ValidateName(f.Profile); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if f.Operation != OperationRead && f.Operation != OperationKVGet {
		return errors.New("operation must be read or kv-get")
	}
	if strings.TrimSpace(f.Path) == "" {
		return errors.New("path must not be empty or whitespace-only")
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
