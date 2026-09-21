package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"vlt/internal/config"
	"vlt/internal/profile"
)

func TestProfileCurrentPrintsActiveNameOnly(t *testing.T) {
	store := &fakeProfileStore{configuration: config.Configuration{
		ActiveProfile: "team-a", Profiles: []profile.Profile{{Name: "team-a"}},
	}}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Profiles: store, Output: &output})
	if err := handler(context.Background(), []string{"current"}); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "team-a\n" {
		t.Fatalf("current output = %q", got)
	}
}

func TestProfileCurrentExplainsMissingActiveProfile(t *testing.T) {
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{Profiles: &fakeProfileStore{}, Output: &output})
	err := handler(context.Background(), []string{"current"})
	if err == nil || !strings.Contains(err.Error(), "no active profile") || !strings.Contains(err.Error(), "vlt switch") {
		t.Fatalf("current error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("current output = %q", output.String())
	}
}
