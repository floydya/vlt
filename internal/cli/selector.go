package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

var (
	ErrSharedSelectorCanceled     = errors.New("selection canceled")
	ErrSharedSelectorEmpty        = errors.New("no selection choices")
	ErrSharedSelectorInvalidItems = errors.New("invalid selection choices")
)

const sharedSelectorMaxVisible = 8

type SharedSelectorItem struct {
	ID         string
	Label      string
	SearchText string
}

type SharedSelector interface {
	Select(context.Context, string, []SharedSelectorItem, string) (string, error)
}

type sharedSelector struct {
	input        io.Reader
	output       io.Writer
	presentation presentation
}

type sharedSelectorModel struct {
	title        string
	items        []SharedSelectorItem
	visible      []SharedSelectorItem
	query        string
	cursor       int
	selectedID   string
	canceled     bool
	presentation presentation
}

func NewSharedSelector(input io.Reader, output io.Writer, terminal Terminal) SharedSelector {
	return sharedSelector{
		input:        input,
		output:       output,
		presentation: newPresentation(terminal),
	}
}

func (s sharedSelector) Select(
	ctx context.Context,
	title string,
	items []SharedSelectorItem,
	preselectedID string,
) (string, error) {
	model, err := newSharedSelectorModel(title, items, preselectedID, s.presentation)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		return "", errors.New("shared selector: context is not configured")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.input == nil {
		return "", errors.New("shared selector: input is not configured")
	}
	if s.output == nil {
		return "", errors.New("shared selector: output is not configured")
	}

	final, err := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithInput(s.input),
		tea.WithOutput(s.output),
	).Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if errors.Is(err, tea.ErrInterrupted) {
			return "", ErrSharedSelectorCanceled
		}
		return "", fmt.Errorf("shared selector: run: %w", err)
	}
	result, ok := final.(sharedSelectorModel)
	if !ok {
		return "", errors.New("shared selector: unexpected final model")
	}
	if result.canceled {
		return "", ErrSharedSelectorCanceled
	}
	if result.selectedID == "" {
		return "", errors.New("shared selector: no selection returned")
	}
	return result.selectedID, nil
}

func newSharedSelectorModel(
	title string,
	items []SharedSelectorItem,
	preselectedID string,
	presentation presentation,
) (sharedSelectorModel, error) {
	if err := validateSharedSelectorItems(items); err != nil {
		return sharedSelectorModel{}, err
	}
	cloned := append([]SharedSelectorItem(nil), items...)
	model := sharedSelectorModel{
		title:        title,
		items:        cloned,
		visible:      append([]SharedSelectorItem(nil), cloned...),
		presentation: presentation,
	}
	for index, item := range model.visible {
		if item.ID == preselectedID {
			model.cursor = index
			break
		}
	}
	return model, nil
}

func validateSharedSelectorItems(items []SharedSelectorItem) error {
	if len(items) == 0 {
		return ErrSharedSelectorEmpty
	}
	identities := make(map[string]struct{}, len(items))
	for index, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			return fmt.Errorf("%w: item %d has an empty identity", ErrSharedSelectorInvalidItems, index+1)
		}
		if strings.TrimSpace(item.Label) == "" {
			return fmt.Errorf("%w: item %d has an empty label", ErrSharedSelectorInvalidItems, index+1)
		}
		if _, exists := identities[item.ID]; exists {
			return fmt.Errorf("%w: identity %q is duplicated", ErrSharedSelectorInvalidItems, item.ID)
		}
		identities[item.ID] = struct{}{}
	}
	return nil
}

func filterSharedSelectorItems(query string, items []SharedSelectorItem) []SharedSelectorItem {
	if query == "" {
		return append([]SharedSelectorItem(nil), items...)
	}
	targets := make([]string, len(items))
	for index, item := range items {
		target := item.SearchText
		if target == "" {
			target = item.Label
		}
		targets[index] = strings.ToLower(target)
	}
	ranks := list.DefaultFilter(strings.ToLower(query), targets)
	filtered := make([]SharedSelectorItem, 0, len(ranks))
	for _, rank := range ranks {
		if rank.Index >= 0 && rank.Index < len(items) {
			filtered = append(filtered, items[rank.Index])
		}
	}
	return filtered
}

func (m sharedSelectorModel) Init() tea.Cmd {
	return nil
}

func (m sharedSelectorModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.InterruptMsg:
		m.canceled = true
		return m, tea.Quit
	case tea.PasteMsg:
		m.appendQuery(message.Content)
		return m, nil
	case tea.KeyPressMsg:
		key := message.Key()
		switch {
		case key.Code == tea.KeyEsc || message.String() == "ctrl+c":
			m.canceled = true
			return m, tea.Quit
		case key.Code == tea.KeyEnter || key.Code == tea.KeyReturn || key.Code == tea.KeyKpEnter:
			if len(m.visible) == 0 {
				return m, nil
			}
			m.selectedID = m.visible[m.cursor].ID
			return m, tea.Quit
		case key.Code == tea.KeyUp:
			if len(m.visible) > 0 {
				m.cursor = (m.cursor - 1 + len(m.visible)) % len(m.visible)
			}
		case key.Code == tea.KeyDown:
			if len(m.visible) > 0 {
				m.cursor = (m.cursor + 1) % len(m.visible)
			}
		case key.Code == tea.KeyBackspace:
			m.removeQueryRune()
		case key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper) == 0:
			m.appendQuery(key.Text)
		}
	}
	return m, nil
}

func (m sharedSelectorModel) View() tea.View {
	var output strings.Builder
	output.WriteString(m.presentation.render(presentationHeading, m.title))
	output.WriteByte('\n')
	output.WriteString(m.presentation.render(presentationLabel, "Search:"))
	output.WriteByte(' ')
	output.WriteString(m.query)
	output.WriteByte('\n')

	if len(m.visible) == 0 {
		output.WriteString(m.presentation.render(presentationMuted, "  No matches."))
		output.WriteByte('\n')
	} else {
		start := 0
		if m.cursor >= sharedSelectorMaxVisible {
			start = m.cursor - sharedSelectorMaxVisible + 1
		}
		end := min(len(m.visible), start+sharedSelectorMaxVisible)
		for index := start; index < end; index++ {
			marker := "  "
			role := presentationPlain
			if index == m.cursor {
				marker = "> "
				role = presentationSelected
			}
			output.WriteString(m.presentation.render(role, marker))
			output.WriteString(m.presentation.render(role, m.visible[index].Label))
			output.WriteByte('\n')
		}
	}
	output.WriteString(m.presentation.render(presentationMuted, "up/down move  enter select  esc cancel"))

	return tea.NewView(output.String())
}

func (m *sharedSelectorModel) appendQuery(value string) {
	var filtered strings.Builder
	for _, candidate := range value {
		if !unicode.IsControl(candidate) {
			filtered.WriteRune(candidate)
		}
	}
	if filtered.Len() == 0 {
		return
	}
	m.query += filtered.String()
	m.applyFilter()
}

func (m *sharedSelectorModel) removeQueryRune() {
	query := []rune(m.query)
	if len(query) == 0 {
		return
	}
	m.query = string(query[:len(query)-1])
	m.applyFilter()
}

func (m *sharedSelectorModel) applyFilter() {
	m.visible = filterSharedSelectorItems(m.query, m.items)
	m.cursor = 0
}
