package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"vlt/internal/config"
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
