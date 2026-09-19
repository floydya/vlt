package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"charm.land/huh/v2"
)

type ProfileSelector interface {
	Select(context.Context, []string, string) (string, error)
}

type huhProfileSelector struct {
	input      io.Reader
	output     io.Writer
	accessible bool
}

func NewHuhProfileSelector(input io.Reader, output io.Writer) ProfileSelector {
	return huhProfileSelector{input: input, output: output}
}

func (s huhProfileSelector) Select(ctx context.Context, names []string, active string) (string, error) {
	if len(names) == 0 {
		return "", errors.New("select profile: no profile choices")
	}
	if s.input == nil {
		return "", errors.New("select profile: input is not configured")
	}
	if s.output == nil {
		return "", errors.New("select profile: output is not configured")
	}

	selected := names[0]
	options := make([]huh.Option[string], 0, len(names))
	for _, name := range names {
		label := name
		isActive := name == active
		if isActive {
			label += " (active)"
			selected = name
		}
		options = append(options, huh.NewOption(label, name).Selected(isActive))
	}

	field := huh.NewSelect[string]().
		Title("Select a profile").
		Options(options...).
		Value(&selected)
	form := huh.NewForm(huh.NewGroup(field)).
		WithInput(s.input).
		WithOutput(s.output).
		WithAccessible(s.accessible)
	if err := form.RunWithContext(ctx); err != nil {
		return "", fmt.Errorf("select profile: %w", err)
	}
	return selected, nil
}
