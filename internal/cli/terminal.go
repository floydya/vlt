package cli

import (
	"io"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
)

type Terminal interface {
	InputIsTerminal() bool
	DisplayIsTerminal() bool
	PromptsEnabled() bool
	ColorEnabled() bool
}

type terminalCapabilities struct {
	inputTerminal   bool
	displayTerminal bool
	colorEnabled    bool
}

type terminalDetector func(uintptr) bool
type colorProfileDetector func(io.Writer, []string) colorprofile.Profile

func NewTerminal(input io.Reader, display io.Writer, environment []string) Terminal {
	return newTerminal(input, display, environment, term.IsTerminal, colorprofile.Detect)
}

func newTerminal(
	input io.Reader,
	display io.Writer,
	environment []string,
	detectTerminal terminalDetector,
	detectColorProfile colorProfileDetector,
) Terminal {
	inputTerminal := streamIsTerminal(input, detectTerminal)
	displayTerminal := streamIsTerminal(display, detectTerminal)
	colorEnabled := displayTerminal && !hasNonEmptyEnvironment(environment, "NO_COLOR") &&
		detectColorProfile(display, environment) > colorprofile.ASCII

	return terminalCapabilities{
		inputTerminal:   inputTerminal,
		displayTerminal: displayTerminal,
		colorEnabled:    colorEnabled,
	}
}

func (t terminalCapabilities) InputIsTerminal() bool {
	return t.inputTerminal
}

func (t terminalCapabilities) DisplayIsTerminal() bool {
	return t.displayTerminal
}

func (t terminalCapabilities) PromptsEnabled() bool {
	return t.inputTerminal && t.displayTerminal
}

func (t terminalCapabilities) ColorEnabled() bool {
	return t.colorEnabled
}

func streamIsTerminal(stream any, detect terminalDetector) bool {
	file, ok := stream.(interface{ Fd() uintptr })
	return ok && detect(file.Fd())
}

func hasNonEmptyEnvironment(environment []string, name string) bool {
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found && key == name && value != "" {
			return true
		}
	}
	return false
}
