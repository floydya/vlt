package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"vlt/internal/profile"
)

type promptLineReader struct {
	lines [][]byte
}

func (r *promptLineReader) Read(value []byte) (int, error) {
	if len(r.lines) == 0 {
		return 0, io.EOF
	}
	n := copy(value, r.lines[0])
	if n == len(r.lines[0]) {
		r.lines = r.lines[1:]
	} else {
		r.lines[0] = r.lines[0][n:]
	}
	return n, nil
}

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

func TestHuhProfileFormPreservesDefaultsAndCorrectsInvalidFields(t *testing.T) {
	input := &promptLineReader{lines: [][]byte{
		[]byte("\n"),
		[]byte("ftp://vault.example.com\n"),
		[]byte("https://vault.example.com\n"),
		[]byte("alice\n"),
		[]byte("\n"),
		[]byte("\n"),
	}}
	var output bytes.Buffer
	form := huhProfileForm{input: input, output: &output, accessible: true}

	got, err := form.Run(context.Background(), ProfileFormRequest{
		Profile: profile.Profile{Name: "team-a", AuthPath: "oidc"}, NameEditable: true,
	})
	if err != nil {
		t.Fatalf("run profile form: %v", err)
	}
	want := profile.Profile{
		Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc",
	}
	if got != want {
		t.Errorf("profile form result = %#v, want %#v", got, want)
	}
	for _, text := range []string{"Name", "Address", "Username", "Auth path", "Namespace", "address scheme must be http or https"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("profile form output = %q, want text %q", output.String(), text)
		}
	}
	for _, forbidden := range []string{"password", "credential", "token"} {
		if strings.Contains(strings.ToLower(output.String()), forbidden) {
			t.Errorf("profile form output exposed secret field %q: %q", forbidden, output.String())
		}
	}
}

func TestHuhProfileFormKeepsUpdateNameReadOnly(t *testing.T) {
	input := &promptLineReader{lines: [][]byte{
		[]byte("https://new.example.com\n"),
		[]byte("\n"),
		[]byte("\n"),
		[]byte("\n"),
	}}
	var output bytes.Buffer
	form := huhProfileForm{input: input, output: &output, accessible: true}
	current := profile.Profile{
		Name: "team-a", Address: "https://vault.example.com", Username: "alice",
		AuthPath: "oidc", Namespace: "engineering",
	}

	got, err := form.Run(context.Background(), ProfileFormRequest{Profile: current})
	if err != nil {
		t.Fatalf("run profile form: %v", err)
	}
	if got.Name != current.Name {
		t.Errorf("profile name = %q, want stable %q", got.Name, current.Name)
	}
	if got.Address != "https://new.example.com" {
		t.Errorf("profile address = %q, want updated address", got.Address)
	}
	if strings.Contains(ansi.Strip(output.String()), "Name ") {
		t.Errorf("update form output contains editable name field: %q", output.String())
	}
}

func TestHuhProfileRemovalConfirmerNamesProfileAndWarnsForActiveRemoval(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		leavesNoActive bool
		wantConfirmed  bool
		wantWarning    bool
	}{
		{name: "accept active removal", input: "y\n", leavesNoActive: true, wantConfirmed: true, wantWarning: true},
		{name: "decline inactive removal", input: "n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			confirmer := huhProfileRemovalConfirmer{
				input: strings.NewReader(tt.input), output: &output, accessible: true,
			}

			confirmed, err := confirmer.Confirm(context.Background(), ProfileRemovalConfirmation{
				Name: "team-a", LeavesNoActiveProfile: tt.leavesNoActive,
			})
			if err != nil {
				t.Fatalf("confirm profile removal: %v", err)
			}
			if confirmed != tt.wantConfirmed {
				t.Errorf("confirmed = %t, want %t", confirmed, tt.wantConfirmed)
			}
			plainOutput := ansi.Strip(output.String())
			if !strings.Contains(plainOutput, `Remove profile "team-a"`) {
				t.Errorf("confirmation output = %q, want profile name", plainOutput)
			}
			hasWarning := strings.Contains(plainOutput, "leave no active profile")
			if hasWarning != tt.wantWarning {
				t.Errorf("confirmation warning = %t, want %t; output=%q", hasWarning, tt.wantWarning, plainOutput)
			}
		})
	}
}
