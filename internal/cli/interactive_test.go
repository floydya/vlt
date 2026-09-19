package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestHuhProfileSelectorShowsSortedNamesAndPreselectsActive(t *testing.T) {
	var output bytes.Buffer
	selector := huhProfileSelector{input: strings.NewReader("\n"), output: &output, accessible: true}

	selected, err := selector.Select(context.Background(), []string{"team-a", "team-b"}, "team-b")
	if err != nil {
		t.Fatalf("select profile: %v", err)
	}
	if selected != "team-b" {
		t.Errorf("selected profile = %q, want active team-b", selected)
	}
	teamA := strings.Index(output.String(), "1. team-a")
	teamB := strings.Index(output.String(), "2. team-b (active)")
	if teamA < 0 || teamB < 0 || teamA > teamB {
		t.Errorf("selector output = %q, want sorted names with active context", output.String())
	}
	for _, forbidden := range []string{"https://", "username", "namespace", "credential", "token"} {
		if strings.Contains(strings.ToLower(output.String()), forbidden) {
			t.Errorf("selector output exposed %q: %q", forbidden, output.String())
		}
	}
}

func TestHuhProfileSelectorReturnsChosenName(t *testing.T) {
	var output bytes.Buffer
	selector := huhProfileSelector{input: strings.NewReader("1\n"), output: &output, accessible: true}

	selected, err := selector.Select(context.Background(), []string{"team-a", "team-b"}, "team-b")
	if err != nil {
		t.Fatalf("select profile: %v", err)
	}
	if selected != "team-a" {
		t.Errorf("selected profile = %q, want team-a", selected)
	}
}
