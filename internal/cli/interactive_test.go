package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"vlt/internal/profile"
)

type promptLineReader struct {
	lines [][]byte
}

type recordingSharedSelector struct {
	title         string
	items         []SharedSelectorItem
	preselectedID string
	selectedID    string
	err           error
	calls         int
}

func (s *recordingSharedSelector) Select(
	_ context.Context,
	title string,
	items []SharedSelectorItem,
	preselectedID string,
) (string, error) {
	s.calls++
	s.title = title
	s.items = append([]SharedSelectorItem(nil), items...)
	s.preselectedID = preselectedID
	return s.selectedID, s.err
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

func TestInteractiveFormsAndConfirmationsReceiveActiveAccent(t *testing.T) {
	terminal := WithAccent(fixedTerminal{color: true}, "#112233")
	var output bytes.Buffer
	forms := []presentation{
		NewHuhProfileForm(strings.NewReader(""), &output, terminal).(huhProfileForm).presentation,
		NewHuhProfileRemovalConfirmer(strings.NewReader(""), &output, terminal).(huhProfileRemovalConfirmer).presentation,
		NewHuhFavoriteRemovalConfirmer(strings.NewReader(""), &output, terminal).(huhFavoriteRemovalConfirmer).presentation,
	}
	for index, form := range forms {
		got := form.huhTheme().Theme(true).Focused.SelectedOption.Render("Selected")
		if !strings.Contains(got, "38;2;17;34;51m") {
			t.Errorf("interactive form %d selected style = %q, want active accent", index, got)
		}
	}
}

func TestSharedProfileSelectorBuildsDeterministicSearchRowsAndPreselectsActive(t *testing.T) {
	shared := &recordingSharedSelector{selectedID: "team-a"}
	selector := NewSharedProfileSelector(shared)
	candidates := []profile.Profile{
		{
			Name: "team-b", Address: "https://vault.team-b.example", Username: "hidden-b",
			AuthPath: "hidden-auth-b", Namespace: "", AllowInsecure: true,
		},
		{
			Name: "team-a", Address: "https://vault.team-a.example", Username: "hidden-a",
			AuthPath: "hidden-auth-a", Namespace: "engineering",
		},
	}

	selected, err := selector.Select(context.Background(), candidates, "team-b")
	if err != nil {
		t.Fatalf("select profile error = %v", err)
	}
	if selected != "team-a" {
		t.Errorf("selected profile = %q, want stable name team-a", selected)
	}
	if shared.title != "Select a profile" || shared.preselectedID != "team-b" {
		t.Errorf("shared selector request = title %q, preselected %q", shared.title, shared.preselectedID)
	}
	if len(shared.items) != 2 {
		t.Fatalf("shared selector item count = %d, want 2", len(shared.items))
	}
	if shared.items[0].ID != "team-a" || shared.items[1].ID != "team-b" {
		t.Fatalf("shared selector IDs = %q, %q, want deterministic profile order", shared.items[0].ID, shared.items[1].ID)
	}
	for _, text := range []string{"1", "team-a", "https://vault.team-a.example", "engineering"} {
		if !strings.Contains(shared.items[0].Label, text) {
			t.Errorf("team-a label = %q, want %q", shared.items[0].Label, text)
		}
	}
	for _, text := range []string{"2", "*", "team-b", "https://vault.team-b.example", "-", "yes"} {
		if !strings.Contains(shared.items[1].Label, text) {
			t.Errorf("team-b label = %q, want %q", shared.items[1].Label, text)
		}
	}
	for _, item := range shared.items {
		for _, forbidden := range []string{"hidden-a", "hidden-b", "hidden-auth-a", "hidden-auth-b"} {
			if strings.Contains(item.Label+item.SearchText, forbidden) {
				t.Errorf("profile selector item exposes %q: %#v", forbidden, item)
			}
		}
	}
	filtered := filterSharedSelectorItems("ENG", shared.items)
	if len(filtered) != 1 || filtered[0].ID != "team-a" {
		t.Errorf("namespace filter = %#v, want team-a", filtered)
	}
	filtered = filterSharedSelectorItems("TMBEX", shared.items)
	if len(filtered) != 1 || filtered[0].ID != "team-b" {
		t.Errorf("address filter = %#v, want team-b", filtered)
	}
}

func TestSharedProfileSelectorPropagatesCancellationWithoutSelection(t *testing.T) {
	shared := &recordingSharedSelector{err: ErrSharedSelectorCanceled}
	selector := NewSharedProfileSelector(shared)

	selected, err := selector.Select(context.Background(), []profile.Profile{managementTestProfile("team-a")}, "team-a")
	if !errors.Is(err, ErrSharedSelectorCanceled) {
		t.Fatalf("select profile error = %v, want shared cancellation", err)
	}
	if selected != "" {
		t.Errorf("selected profile = %q, want empty after cancellation", selected)
	}
}

func TestHuhProfileFormPreservesDefaultsAndCorrectsInvalidFields(t *testing.T) {
	input := &promptLineReader{lines: [][]byte{
		[]byte("\n"),
		[]byte("n\n"),
		[]byte("ftp://vault.example.com\n"),
		[]byte("https://vault.example.com\n"),
		[]byte("alice\n"),
		[]byte("\n"),
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
	for _, text := range []string{"Name", "Allow insecure HTTP", "Address", "Username", "Auth path", "Namespace", "Color", "address scheme must be http or https"} {
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
		[]byte("n\n"),
		[]byte("https://new.example.com\n"),
		[]byte("\n"),
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

func TestHuhProfileFormCanEnableAndClearInsecureHTTPOptIn(t *testing.T) {
	for _, tt := range []struct {
		name    string
		current profile.Profile
		input   []string
		want    profile.Profile
	}{
		{
			name: "enable for HTTP",
			current: profile.Profile{
				Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc",
			},
			input: []string{"y\n", "http://127.0.0.1:8200\n", "\n", "\n", "\n", "\n"},
			want: profile.Profile{
				Name: "team-a", Address: "http://127.0.0.1:8200", Username: "alice", AuthPath: "oidc", AllowInsecure: true,
			},
		},
		{
			name: "clear while moving to HTTPS",
			current: profile.Profile{
				Name: "team-a", Address: "http://127.0.0.1:8200", Username: "alice", AuthPath: "oidc", AllowInsecure: true,
			},
			input: []string{"n\n", "https://vault.example.com\n", "\n", "\n", "\n", "\n"},
			want: profile.Profile{
				Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			lines := make([][]byte, len(tt.input))
			for index, line := range tt.input {
				lines[index] = []byte(line)
			}
			form := huhProfileForm{input: &promptLineReader{lines: lines}, output: &output, accessible: true}

			got, err := form.Run(context.Background(), ProfileFormRequest{Profile: tt.current})
			if err != nil {
				t.Fatalf("run profile form: %v", err)
			}
			if got != tt.want {
				t.Fatalf("profile form result = %#v, want %#v", got, tt.want)
			}
			if !strings.Contains(ansi.Strip(output.String()), "Allow insecure HTTP") {
				t.Fatalf("profile form output = %q, want insecure transport control", output.String())
			}
		})
	}
}

func TestHuhProfileFormValidatesAndEditsColor(t *testing.T) {
	for _, tt := range []struct {
		name         string
		currentColor string
		colorInput   []string
		wantColor    string
		wantError    bool
	}{
		{name: "set after invalid input", colorInput: []string{"red\n", "#a1B2c3\n"}, wantColor: "#a1B2c3", wantError: true},
		{name: "keep current", currentColor: "#A1B2C3", colorInput: []string{"\n"}, wantColor: "#A1B2C3"},
		{name: "clear current", currentColor: "#A1B2C3", colorInput: []string{"-\n"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current := profile.Profile{Name: "team-a", Address: "https://vault.example.com", Username: "alice", AuthPath: "oidc", Color: tt.currentColor}
			lines := [][]byte{[]byte("n\n"), []byte("\n"), []byte("\n"), []byte("\n"), []byte("\n")}
			for _, line := range tt.colorInput {
				lines = append(lines, []byte(line))
			}
			var output bytes.Buffer
			form := huhProfileForm{input: &promptLineReader{lines: lines}, output: &output, accessible: true}
			got, err := form.Run(context.Background(), ProfileFormRequest{Profile: current})
			if err != nil {
				t.Fatalf("run profile form: %v", err)
			}
			if got.Color != tt.wantColor {
				t.Fatalf("profile color = %q, want %q", got.Color, tt.wantColor)
			}
			if !strings.Contains(output.String(), "Color") {
				t.Fatalf("form output = %q, want color field", output.String())
			}
			if tt.wantError && !strings.Contains(output.String(), "color must be #RRGGBB") {
				t.Fatalf("form output = %q, want inline color error", output.String())
			}
		})
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

func TestHuhProfileRemovalConfirmerReportsLinkedFavoriteCount(t *testing.T) {
	var output bytes.Buffer
	confirmer := huhProfileRemovalConfirmer{
		input: strings.NewReader("n\n"), output: &output, accessible: true,
	}

	confirmed, err := confirmer.Confirm(context.Background(), ProfileRemovalConfirmation{
		Name: "team-a", LinkedFavorites: 2,
	})
	if err != nil {
		t.Fatalf("confirm profile removal error = %v", err)
	}
	if confirmed {
		t.Fatal("confirmed = true, want declined")
	}
	for _, text := range []string{"team-a", "2 linked favorites"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("confirmation output = %q, want %q", output.String(), text)
		}
	}
}
