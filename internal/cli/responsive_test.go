package cli

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"vlt/internal/favorite"
	"vlt/internal/profile"
)

func TestNarrowListsWrapCompleteValues(t *testing.T) {
	const width = 52
	profiles := []profile.Profile{{
		Name: "team-a", Address: "https://vault.platform.example.invalid",
		Namespace: "platform", Color: "#3366CC",
	}}
	gotProfiles := profileListOutput(profiles, "team-a", fixedTerminal{width: width})
	for _, value := range []string{"team-a", profiles[0].Address, "platform", "#3366CC"} {
		if !strings.Contains(gotProfiles, value) {
			t.Errorf("wrapped profiles = %q, missing %q", gotProfiles, value)
		}
	}
	if !strings.Contains(gotProfiles, "Address:") {
		t.Fatalf("wrapped profiles = %q, want labeled details", gotProfiles)
	}
	assertLinesFit(t, gotProfiles, width)

	favorites := []favorite.Favorite{{
		ID: "f_0123456789abcdef", Profile: "team-a", Operation: favorite.OperationKVGet,
		Path: "secret/data/platform/database", Note: "daily", RunCount: 23,
	}}
	gotFavorites := favoriteListOutput(favorites, nil, fixedTerminal{width: width})
	for _, value := range []string{favorites[0].ID, favorites[0].Path, "team-a", "kv-get", "daily"} {
		if !strings.Contains(gotFavorites, value) {
			t.Errorf("wrapped favorites = %q, missing %q", gotFavorites, value)
		}
	}
	assertLinesFit(t, gotFavorites, width)
}

func TestNarrowProfileColorMarksIdentityOnly(t *testing.T) {
	candidate := profile.Profile{
		Name: "team-a", Address: "https://vault.example.invalid", Color: "#112233",
	}
	styled := profileListOutput([]profile.Profile{candidate}, "team-a", fixedTerminal{color: true, width: 45})
	plain := ansi.Strip(styled)
	if !strings.Contains(plain, "team-a") || !strings.Contains(plain, candidate.Address) {
		t.Fatalf("profile list = %q", plain)
	}
	if count := strings.Count(styled, "38;2;17;34;51m"); count != 2 {
		t.Fatalf("saved profile color count = %d, want name and active marker only: %q", count, styled)
	}
}

func TestNarrowFavoriteColorStaysOnLinkedValues(t *testing.T) {
	candidate := favorite.Favorite{
		ID: "f_0123456789abcdef", Profile: "team-a", Operation: favorite.OperationRead,
		Path: "secret/team-a", Note: "daily",
	}
	styled := favoriteListOutput([]favorite.Favorite{candidate}, map[string]string{"team-a": "#112233"}, fixedTerminal{color: true, width: 45})
	if !strings.Contains(styled, "38;2;17;34;51m") {
		t.Fatalf("wrapped favorite has no linked profile color: %q", styled)
	}
	for _, line := range strings.Split(styled, "\n") {
		if strings.Contains(line, "ID:") || strings.Contains(line, "Profile:") {
			if strings.HasPrefix(line, "\x1b[38;2;17;34;51m") {
				t.Errorf("favorite label uses linked profile color: %q", line)
			}
		}
	}
	assertLinesFit(t, styled, 45)
}

func assertLinesFit(t *testing.T, output string, width int) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSuffix(ansi.Strip(output), "\n"), "\n") {
		if lipgloss.Width(line) > width {
			t.Errorf("line width %d exceeds %d: %q", lipgloss.Width(line), width, line)
		}
	}
}

func TestSelectorShowsDetailsForCurrentChoice(t *testing.T) {
	items := []SharedSelectorItem{
		{ID: "a", Label: "team-a", Detail: "Address: https://alpha.example.invalid", SearchText: "team-a https://alpha.example.invalid"},
		{ID: "b", Label: "team-b", Detail: "Address: https://beta.example.invalid", SearchText: "team-b https://beta.example.invalid"},
	}
	model, err := newSharedSelectorModel("Select a profile", "", items, "", newPresentation(fixedTerminal{width: 50}))
	if err != nil {
		t.Fatal(err)
	}
	first := ansi.Strip(model.View().Content)
	if !strings.Contains(first, "alpha.example.invalid") || strings.Contains(first, "beta.example.invalid") {
		t.Fatalf("initial details = %q", first)
	}
	if matches := filterSharedSelectorItems("beta", items); len(matches) != 1 || matches[0].ID != "b" {
		t.Fatalf("detail search matches = %#v", matches)
	}
}

func TestNarrowSelectorWrapsLongChoiceAndDetails(t *testing.T) {
	const width = 24
	items := []SharedSelectorItem{{
		ID: "one", Label: "1  secret/data/platform/database",
		Detail: "Profile: platform-development\nOperation: kv-get",
	}}
	model, err := newSharedSelectorModel("Select a favorite", "", items, "", newPresentation(fixedTerminal{width: width}))
	if err != nil {
		t.Fatal(err)
	}
	got := model.View().Content
	assertLinesFit(t, got, width)
	if !strings.Contains(strings.ReplaceAll(got, "\n", ""), "secret/data/platform/database") {
		t.Fatalf("selector lost path: %q", got)
	}
}
