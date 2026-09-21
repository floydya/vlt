package cli

import (
	"context"
	"strings"
	"testing"

	"vlt/internal/profile"
)

type colorScopeTerminal struct{ fixedTerminal }

func (colorScopeTerminal) AccentColor() string { return "#FE5803" }

func TestSavedProfileColorDoesNotTintSharedUI(t *testing.T) {
	terminal := colorScopeTerminal{fixedTerminal{color: true}}
	p := newPresentation(terminal)
	const orange = "38;2;254;88;3m"
	for _, role := range []presentationRole{
		presentationHeading, presentationLabel, presentationSelected,
		presentationMuted, presentationSuccess, presentationError,
	} {
		if got := p.render(role, "Shared text"); strings.Contains(got, orange) {
			t.Errorf("shared role %d uses saved profile color: %q", role, got)
		}
	}
	theme := p.huhTheme().Theme(true)
	for name, value := range map[string]string{
		"title":  theme.Focused.Title.Render("Title"),
		"prompt": theme.Focused.TextInput.Prompt.Render("Prompt"),
		"choice": theme.Focused.SelectedOption.Render("Choice"),
		"border": theme.Focused.Base.Render("Field"),
	} {
		if strings.Contains(value, orange) {
			t.Errorf("form %s uses saved profile color: %q", name, value)
		}
	}
}

func TestSavedProfileColorOnlyMarksIdentityInListAndDetails(t *testing.T) {
	terminal := colorScopeTerminal{fixedTerminal{color: true}}
	profiles := []profile.Profile{
		{Name: "team-a", Address: "https://a.example.invalid", Color: "#FE5803"},
		{Name: "team-b", Address: "https://b.example.invalid", Color: "#394EFF"},
	}
	const orange = "38;2;254;88;3m"
	const blue = "38;2;57;78;255m"
	listed := profileListOutput(profiles, "team-a", terminal)
	if count := strings.Count(listed, orange); count != 2 {
		t.Errorf("active color in list = %d, want name and star only: %q", count, listed)
	}
	if count := strings.Count(listed, blue); count != 1 {
		t.Errorf("other color in list = %d, want name only: %q", count, listed)
	}
	shown := profileShowOutput(profiles[1], false, terminal)
	if count := strings.Count(shown, blue); count != 1 {
		t.Errorf("inactive color in details = %d, want name only: %q", count, shown)
	}
	if strings.Contains(shown, orange) {
		t.Errorf("active profile color leaked into other profile details: %q", shown)
	}
}

func TestSavedProfileColorOnlyMarksIdentityInPickers(t *testing.T) {
	terminal := colorScopeTerminal{fixedTerminal{color: true}}
	profiles := []profile.Profile{
		{Name: "team-a", Address: "https://a.example.invalid", Color: "#FE5803"},
		{Name: "team-b", Address: "https://b.example.invalid", Color: "#394EFF"},
	}
	shared := &recordingSharedSelector{selectedID: "team-b"}
	if _, err := NewSharedProfileSelector(shared).Select(context.Background(), profiles, "team-a"); err != nil {
		t.Fatal(err)
	}
	model, err := newSharedSelectorModel(shared.title, shared.items, "team-b", newPresentation(terminal))
	if err != nil {
		t.Fatal(err)
	}
	view := model.View().Content
	if count := strings.Count(view, "38;2;254;88;3m"); count != 2 {
		t.Errorf("active color in picker = %d, want name and star only: %q", count, view)
	}
	if count := strings.Count(view, "38;2;57;78;255m"); count != 1 {
		t.Errorf("other color in picker = %d, want name only: %q", count, view)
	}
	generic, err := newSharedSelectorModel("Select a favorite", []SharedSelectorItem{{ID: "one", Label: "secret/example", Detail: "Profile: team-a"}}, "", newPresentation(terminal))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(generic.View().Content, "38;2;254;88;3m") {
		t.Errorf("saved color leaked into favorite picker: %q", generic.View().Content)
	}
}
