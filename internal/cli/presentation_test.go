package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"vlt/internal/config"
	"vlt/internal/favorite"
	"vlt/internal/profile"
)

type fixedTerminal struct {
	color bool
}

func (fixedTerminal) InputIsTerminal() bool   { return false }
func (fixedTerminal) DisplayIsTerminal() bool { return false }
func (fixedTerminal) PromptsEnabled() bool    { return false }
func (t fixedTerminal) ColorEnabled() bool    { return t.color }

func TestProfilePresentationStylesOnlyEligibleTerminals(t *testing.T) {
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles: []profile.Profile{{
			Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc",
		}},
		ActiveProfile: "team-a",
	}}

	var plain bytes.Buffer
	plainHandler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Output: &plain, Terminal: fixedTerminal{color: false},
	})
	if err := plainHandler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("plain profile list error = %v", err)
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatalf("plain output contains ANSI: %q", plain.String())
	}

	var styled bytes.Buffer
	styledHandler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Output: &styled, Terminal: fixedTerminal{color: true},
	})
	if err := styledHandler(context.Background(), []string{"list"}); err != nil {
		t.Fatalf("styled profile list error = %v", err)
	}
	if !strings.Contains(styled.String(), "\x1b[") {
		t.Fatalf("styled output contains no ANSI: %q", styled.String())
	}
	if got := ansi.Strip(styled.String()); got != plain.String() {
		t.Errorf("unstyled styled output = %q, want plain output %q", got, plain.String())
	}
}

func TestProfileListPresentationPlainAndStyledAreExact(t *testing.T) {
	profiles := []profile.Profile{
		{Name: "team-b", Address: "https://vault-b.example", Namespace: ""},
		{Name: "team-a", Address: "https://vault-a.example", Namespace: "engineering"},
	}
	want := "#  ACTIVE  NAME    ADDRESS                  NAMESPACE    ALLOW HTTP\n" +
		"1  *       team-a  https://vault-a.example  engineering  no\n" +
		"2          team-b  https://vault-b.example  -            no\n"

	plain := profileListOutput(profiles, "team-a", fixedTerminal{color: false})
	if plain != want {
		t.Fatalf("plain profile list = %q, want %q", plain, want)
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain profile list contains ANSI: %q", plain)
	}
	styled := profileListOutput(profiles, "team-a", fixedTerminal{color: true})
	if !strings.Contains(styled, "\x1b[") {
		t.Fatalf("styled profile list contains no ANSI: %q", styled)
	}
	if got := ansi.Strip(styled); got != plain {
		t.Fatalf("unstyled profile list = %q, want plain output %q", got, plain)
	}
}

func TestProfileShowPresentationPlainAndStyledAreExact(t *testing.T) {
	candidate := profile.Profile{
		Name: "team-a", Address: "https://vault-a.example", Username: "alice",
		AuthPath: "company-oidc", Namespace: "",
	}
	want := "Name:       team-a\n" +
		"Address:    https://vault-a.example\n" +
		"Username:   alice\n" +
		"Auth path:  company-oidc\n" +
		"Namespace:  -\n" +
		"Allow HTTP: no\n" +
		"Active:     yes\n"

	plain := profileShowOutput(candidate, true, fixedTerminal{color: false})
	if plain != want {
		t.Fatalf("plain profile show = %q, want %q", plain, want)
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain profile show contains ANSI: %q", plain)
	}
	styled := profileShowOutput(candidate, true, fixedTerminal{color: true})
	if !strings.Contains(styled, "\x1b[") {
		t.Fatalf("styled profile show contains no ANSI: %q", styled)
	}
	if got := ansi.Strip(styled); got != plain {
		t.Fatalf("unstyled profile show = %q, want plain output %q", got, plain)
	}
}

func TestFavoriteListPresentationPlainAndStyledAreExact(t *testing.T) {
	favorites := []favorite.Favorite{
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/z"},
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/a", Note: "line\nbreak\x1b[31m"},
		{Profile: "team-a", Operation: favorite.OperationKVGet, Path: "secret/a", Note: "daily"},
	}
	want := "#  OPERATION  PROFILE  PATH      NOTE\n" +
		"1  kv-get     team-a   secret/a  daily\n" +
		"2  read       team-b   secret/a  line break [31m\n" +
		"3  read       team-b   secret/z  -\n"

	plain := favoriteListOutput(favorites, fixedTerminal{color: false})
	if plain != want {
		t.Fatalf("plain favorite list = %q, want %q", plain, want)
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain favorite list contains ANSI: %q", plain)
	}
	styled := favoriteListOutput(favorites, fixedTerminal{color: true})
	if !strings.Contains(styled, "\x1b[") {
		t.Fatalf("styled favorite list contains no ANSI: %q", styled)
	}
	if got := ansi.Strip(styled); got != plain {
		t.Fatalf("unstyled favorite list = %q, want plain output %q", got, plain)
	}
}

func TestPresentationPlainPrimitivesAreExactAndANSIFree(t *testing.T) {
	presentation := newPresentation(fixedTerminal{color: false})

	table := presentation.renderTable([][]presentationCell{
		{
			{value: "#", role: presentationHeading},
			{value: "NAME", role: presentationHeading},
			{value: "STATE", role: presentationHeading},
		},
		{
			{value: "1"},
			{value: "téam-a", role: presentationSelected},
			{value: "active", role: presentationMuted},
		},
	})
	details := presentation.renderDetails([]presentationDetail{
		{label: "Name", value: "team-a"},
		{label: "Active", value: "yes", role: presentationSelected},
	})
	status := presentation.status("Added profile \"team-a\".")
	cancellation := presentation.cancellation("Profile selection canceled.")
	diagnostic := presentation.diagnostic("Profile name is required.")

	got := table + details + status + cancellation + diagnostic
	want := "#  NAME    STATE\n" +
		"1  téam-a  active\n" +
		"Name:   team-a\n" +
		"Active: yes\n" +
		"Added profile \"team-a\".\n" +
		"Profile selection canceled.\n" +
		"Profile name is required.\n"
	if got != want {
		t.Fatalf("plain presentation = %q, want %q", got, want)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("plain presentation contains ANSI: %q", got)
	}
	if got := presentation.renderTable(nil); got != "" {
		t.Errorf("empty table = %q, want empty output", got)
	}
	if got := presentation.renderDetails(nil); got != "" {
		t.Errorf("empty details = %q, want empty output", got)
	}
}

func TestPresentationStyledPrimitivesStripExactlyToPlain(t *testing.T) {
	plain := newPresentation(fixedTerminal{color: false})
	styled := newPresentation(fixedTerminal{color: true})
	table := [][]presentationCell{
		{{value: "NAME", role: presentationHeading}, {value: "STATE", role: presentationHeading}},
		{{value: "team-a", role: presentationSelected}, {value: "ready", role: presentationMuted}},
	}
	details := []presentationDetail{
		{label: "Name", value: "team-a"},
		{label: "Active", value: "yes", role: presentationSelected},
	}

	plainOutput := plain.renderTable(table) +
		plain.renderDetails(details) +
		plain.status("Added profile \"team-a\".") +
		plain.cancellation("Profile selection canceled.") +
		plain.diagnostic("Profile name is required.")
	styledOutput := styled.renderTable(table) +
		styled.renderDetails(details) +
		styled.status("Added profile \"team-a\".") +
		styled.cancellation("Profile selection canceled.") +
		styled.diagnostic("Profile name is required.")

	if !strings.Contains(styledOutput, "\x1b[") {
		t.Fatalf("styled presentation contains no ANSI: %q", styledOutput)
	}
	if got := ansi.Strip(styledOutput); got != plainOutput {
		t.Fatalf("unstyled presentation = %q, want plain output %q", got, plainOutput)
	}
}

func TestPresentationHuhThemeUsesSemanticRolesAndHonorsPlainMode(t *testing.T) {
	plainPresentation := newPresentation(fixedTerminal{color: false})
	plainTheme := plainPresentation.huhTheme().Theme(true)
	plainOutput := plainTheme.Group.Title.Render("Heading") +
		plainTheme.Focused.SelectSelector.String() +
		plainTheme.Focused.ErrorMessage.Render("Error") +
		plainTheme.Help.ShortDesc.Render("Help")
	if strings.Contains(plainOutput, "\x1b[") {
		t.Fatalf("plain Huh theme contains ANSI: %q", plainOutput)
	}

	styledPresentation := newPresentation(fixedTerminal{color: true})
	styledTheme := styledPresentation.huhTheme().Theme(true)
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "heading", got: styledTheme.Group.Title.Render("Heading"), want: styledPresentation.render(presentationHeading, "Heading")},
		{name: "selection", got: styledTheme.Focused.SelectedOption.Render("Selected"), want: styledPresentation.render(presentationSelected, "Selected")},
		{name: "muted", got: styledTheme.Help.ShortDesc.Render("Help"), want: styledPresentation.render(presentationMuted, "Help")},
		{name: "error", got: styledTheme.Focused.ErrorMessage.Render("Error"), want: styledPresentation.render(presentationError, "Error")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Huh role = %q, want shared role %q", tt.got, tt.want)
			}
		})
	}
}
