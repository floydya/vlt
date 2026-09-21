package favorite

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
)

func validID(id string) bool {
	if len(id) != 18 || id[:2] != "f_" {
		return false
	}
	for _, character := range id[2:] {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func randomID(reader io.Reader) (string, error) {
	var value [8]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", fmt.Errorf("generate favorite ID: %w", err)
	}
	return "f_" + hex.EncodeToString(value[:]), nil
}

func newRandomID() (string, error) {
	return randomID(rand.Reader)
}

func assignMissingIDs(favorites []Favorite) {
	indices := make([]int, len(favorites))
	used := make(map[string]struct{}, len(favorites))
	for index, candidate := range favorites {
		indices[index] = index
		if candidate.ID != "" {
			used[candidate.ID] = struct{}{}
		}
	}
	sort.Slice(indices, func(left, right int) bool {
		a, b := favorites[indices[left]], favorites[indices[right]]
		if a.Profile != b.Profile {
			return a.Profile < b.Profile
		}
		if a.Operation != b.Operation {
			return a.Operation < b.Operation
		}
		return a.Path < b.Path
	})
	for _, index := range indices {
		if favorites[index].ID != "" {
			continue
		}
		for attempt := uint64(0); ; attempt++ {
			id := legacyID(favorites[index], attempt)
			if _, exists := used[id]; exists {
				continue
			}
			favorites[index].ID = id
			used[id] = struct{}{}
			break
		}
	}
}

func legacyID(candidate Favorite, attempt uint64) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("vlt favorite ID v1"))
	var length [8]byte
	for _, field := range []string{candidate.Profile, candidate.Operation, candidate.Path} {
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(field))
	}
	binary.BigEndian.PutUint64(length[:], attempt)
	_, _ = hash.Write(length[:])
	return "f_" + hex.EncodeToString(hash.Sum(nil)[:8])
}
