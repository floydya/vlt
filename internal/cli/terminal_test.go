package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

type terminalTestReader uintptr

func (terminalTestReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r terminalTestReader) Fd() uintptr            { return uintptr(r) }

type terminalTestWriter uintptr

func (terminalTestWriter) Write(value []byte) (int, error) { return len(value), nil }
func (w terminalTestWriter) Fd() uintptr                   { return uintptr(w) }

func TestTerminalReportsCapabilitiesSeparately(t *testing.T) {
	tests := []struct {
		name        string
		inputTTY    bool
		displayTTY  bool
		profile     colorprofile.Profile
		wantPrompts bool
		wantColor   bool
	}{
		{name: "input and color display", inputTTY: true, displayTTY: true, profile: colorprofile.TrueColor, wantPrompts: true, wantColor: true},
		{name: "redirected input", displayTTY: true, profile: colorprofile.ANSI, wantColor: true},
		{name: "redirected display", inputTTY: true, profile: colorprofile.ANSI},
		{name: "display without color support", inputTTY: true, displayTTY: true, profile: colorprofile.ASCII, wantPrompts: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			terminal := newTerminal(
				terminalTestReader(10), terminalTestWriter(20), nil,
				func(fd uintptr) bool {
					switch fd {
					case 10:
						return tt.inputTTY
					case 20:
						return tt.displayTTY
					default:
						return false
					}
				},
				func(io.Writer, []string) colorprofile.Profile { return tt.profile },
			)

			if got := terminal.InputIsTerminal(); got != tt.inputTTY {
				t.Errorf("InputIsTerminal() = %t, want %t", got, tt.inputTTY)
			}
			if got := terminal.DisplayIsTerminal(); got != tt.displayTTY {
				t.Errorf("DisplayIsTerminal() = %t, want %t", got, tt.displayTTY)
			}
			if got := terminal.PromptsEnabled(); got != tt.wantPrompts {
				t.Errorf("PromptsEnabled() = %t, want %t", got, tt.wantPrompts)
			}
			if got := terminal.ColorEnabled(); got != tt.wantColor {
				t.Errorf("ColorEnabled() = %t, want %t", got, tt.wantColor)
			}
		})
	}
}

func TestTerminalDisablesColorForAnyNonEmptyNoColorValue(t *testing.T) {
	for _, value := range []string{"1", "true", "false", "disabled"} {
		t.Run(value, func(t *testing.T) {
			terminal := newTerminal(
				terminalTestReader(10), terminalTestWriter(20), []string{"TERM=xterm-256color", "NO_COLOR=" + value},
				func(uintptr) bool { return true },
				func(io.Writer, []string) colorprofile.Profile { return colorprofile.ANSI256 },
			)

			if terminal.ColorEnabled() {
				t.Fatal("ColorEnabled() = true, want false")
			}
			if !terminal.PromptsEnabled() {
				t.Fatal("PromptsEnabled() = false, want true")
			}
		})
	}
}

func TestTerminalHandlesStreamsWithoutFileDescriptors(t *testing.T) {
	detectorCalls := 0
	terminal := newTerminal(
		strings.NewReader(""), &bytes.Buffer{}, []string{"TERM=xterm-256color"},
		func(uintptr) bool {
			detectorCalls++
			return true
		},
		func(io.Writer, []string) colorprofile.Profile { return colorprofile.TrueColor },
	)

	if terminal.InputIsTerminal() || terminal.DisplayIsTerminal() || terminal.PromptsEnabled() || terminal.ColorEnabled() {
		t.Fatalf("terminal capabilities enabled for non-file streams: %#v", terminal)
	}
	if detectorCalls != 0 {
		t.Errorf("terminal detector calls = %d, want 0", detectorCalls)
	}
}
