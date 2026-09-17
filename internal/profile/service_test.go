package profile

import (
	"reflect"
	"slices"
	"testing"
)

func serviceTestProfile(name string) Profile {
	return Profile{
		Name:     name,
		Address:  "https://vault.example.com/" + name,
		Username: "user-" + name,
		AuthPath: "oidc",
	}
}

func TestServiceListReturnsLexicographicallySortedProfiles(t *testing.T) {
	tests := []struct {
		name     string
		profiles []Profile
		want     []string
	}{
		{name: "empty"},
		{name: "one profile", profiles: []Profile{serviceTestProfile("team-a")}, want: []string{"team-a"}},
		{
			name: "multiple profiles",
			profiles: []Profile{
				serviceTestProfile("team-2"),
				serviceTestProfile("alpha"),
				serviceTestProfile("team-10"),
				serviceTestProfile("Team"),
			},
			want: []string{"Team", "alpha", "team-10", "team-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(tt.profiles)
			got := service.List()
			gotNames := make([]string, len(got))
			for index := range got {
				gotNames[index] = got[index].Name
			}
			if !slices.Equal(gotNames, tt.want) {
				t.Fatalf("List() names = %v, want %v", gotNames, tt.want)
			}
		})
	}
}

func TestServiceListReturnsIndependentSnapshots(t *testing.T) {
	profiles := []Profile{serviceTestProfile("team-b"), serviceTestProfile("team-a")}
	service := NewService(profiles)

	profiles[0].Name = "changed-input"
	first := service.List()
	first[0].Name = "changed-result"
	got := service.List()

	want := []Profile{serviceTestProfile("team-a"), serviceTestProfile("team-b")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List() after caller mutations = %#v, want %#v", got, want)
	}
}

func TestServiceFindUsesExactCaseSensitiveName(t *testing.T) {
	service := NewService([]Profile{serviceTestProfile("Team"), serviceTestProfile("team"), serviceTestProfile("2")})

	for _, name := range []string{"Team", "team", "2"} {
		got, err := service.Find(name)
		if err != nil {
			t.Fatalf("Find(%q) error = %v", name, err)
		}
		if got.Name != name {
			t.Fatalf("Find(%q).Name = %q, want exact match", name, got.Name)
		}
	}
	for _, name := range []string{"TEAM", "", "missing"} {
		if _, err := service.Find(name); err == nil {
			t.Fatalf("Find(%q) error = nil, want not found", name)
		}
	}
}

func TestServiceResolveCanonicalNumericSelectorUsesSortedIndex(t *testing.T) {
	service := NewService([]Profile{
		serviceTestProfile("zulu"),
		serviceTestProfile("2"),
		serviceTestProfile("alpha"),
	})

	got, err := service.Resolve("2")
	if err != nil {
		t.Fatalf("Resolve(%q) error = %v", "2", err)
	}
	if got.Name != "alpha" {
		t.Fatalf("Resolve(%q).Name = %q, want profile at sorted index 2", "2", got.Name)
	}
}

func TestServiceResolveUsesOneBasedSortedNumericIndex(t *testing.T) {
	service := NewService([]Profile{
		serviceTestProfile("zulu"),
		serviceTestProfile("alpha"),
		serviceTestProfile("beta"),
	})

	for _, tt := range []struct {
		selector string
		want     string
	}{
		{selector: "1", want: "alpha"},
		{selector: "2", want: "beta"},
		{selector: "3", want: "zulu"},
		{selector: "beta", want: "beta"},
	} {
		got, err := service.Resolve(tt.selector)
		if err != nil {
			t.Fatalf("Resolve(%q) error = %v", tt.selector, err)
		}
		if got.Name != tt.want {
			t.Fatalf("Resolve(%q).Name = %q, want %q", tt.selector, got.Name, tt.want)
		}
	}
}

func TestServiceResolveRejectsMalformedAndOutOfRangeSelectors(t *testing.T) {
	service := NewService([]Profile{serviceTestProfile("alpha"), serviceTestProfile("beta")})

	for _, selector := range []string{
		"", "0", "3", "-1", "+1", "01", " 1", "1 ", "１", "1.0", "missing",
		"999999999999999999999999999999999999999999999999999999999999999999",
	} {
		t.Run(selector, func(t *testing.T) {
			if _, err := service.Resolve(selector); err == nil {
				t.Fatalf("Resolve(%q) error = nil, want rejection", selector)
			}
		})
	}
}
