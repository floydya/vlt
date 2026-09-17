package profile

import (
	"errors"
	"sort"
	"strconv"
)

// Service provides deterministic read-only operations over a profile snapshot.
type Service struct {
	profiles []Profile
}

// NewService copies profiles into an independent snapshot.
func NewService(profiles []Profile) Service {
	return Service{profiles: append([]Profile(nil), profiles...)}
}

// List returns a copy of the profiles sorted lexicographically by name.
func (s Service) List() []Profile {
	profiles := append([]Profile(nil), s.profiles...)
	sort.Slice(profiles, func(left, right int) bool {
		return profiles[left].Name < profiles[right].Name
	})
	return profiles
}

// Find returns the profile whose name exactly matches name.
func (s Service) Find(name string) (Profile, error) {
	for _, candidate := range s.profiles {
		if candidate.Name == name {
			return candidate, nil
		}
	}
	return Profile{}, errors.New("profile not found")
}

// Resolve finds an exact name first, then treats a canonical ASCII decimal as
// a one-based index into List's deterministic order.
func (s Service) Resolve(selector string) (Profile, error) {
	if candidate, err := s.Find(selector); err == nil {
		return candidate, nil
	}

	if selector == "0" {
		return Profile{}, errors.New("profile selector is out of range")
	}
	if !isCanonicalIndex(selector) {
		return Profile{}, errors.New("profile selector is invalid")
	}

	index, err := strconv.ParseUint(selector, 10, 64)
	if err != nil {
		return Profile{}, errors.New("profile selector is out of range")
	}
	profiles := s.List()
	if index > uint64(len(profiles)) {
		return Profile{}, errors.New("profile selector is out of range")
	}
	return profiles[index-1], nil
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
