package cli

import (
	"strconv"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"vlt/internal/profile"
)

type presentationRole uint8

const (
	presentationPlain presentationRole = iota
	presentationHeading
	presentationLabel
	presentationSelected
	presentationMuted
	presentationSuccess
	presentationError
)

type presentationStyles struct {
	plain    lipgloss.Style
	heading  lipgloss.Style
	label    lipgloss.Style
	selected lipgloss.Style
	muted    lipgloss.Style
	success  lipgloss.Style
	error    lipgloss.Style
}

type presentation struct {
	styles presentationStyles
}

type presentationCell struct {
	value string
	role  presentationRole
}

type presentationDetail struct {
	label string
	value string
	role  presentationRole
}

func newPresentation(terminal Terminal) presentation {
	styles := presentationStyles{
		plain:    lipgloss.NewStyle(),
		heading:  lipgloss.NewStyle(),
		label:    lipgloss.NewStyle(),
		selected: lipgloss.NewStyle(),
		muted:    lipgloss.NewStyle(),
		success:  lipgloss.NewStyle(),
		error:    lipgloss.NewStyle(),
	}
	if terminal != nil && terminal.ColorEnabled() {
		styles.heading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Cyan)
		styles.label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Cyan)
		styles.selected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Green)
		styles.muted = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		styles.success = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Green)
		styles.error = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Red)
	}
	return presentation{styles: styles}
}

func (p presentation) renderTable(rows [][]presentationCell) string {
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	if columns == 0 {
		return ""
	}

	widths := make([]int, columns)
	for _, row := range rows {
		for column, cell := range row {
			widths[column] = max(widths[column], lipgloss.Width(cell.value))
		}
	}

	var output strings.Builder
	for _, row := range rows {
		for column, cell := range row {
			output.WriteString(p.render(cell.role, cell.value))
			if column < len(row)-1 {
				output.WriteString(strings.Repeat(" ", widths[column]-lipgloss.Width(cell.value)+2))
			}
		}
		output.WriteByte('\n')
	}
	return output.String()
}

func (p presentation) renderDetails(rows []presentationDetail) string {
	labelWidth := 0
	for _, row := range rows {
		labelWidth = max(labelWidth, lipgloss.Width(row.label)+1)
	}

	var output strings.Builder
	for _, row := range rows {
		label := row.label + ":"
		output.WriteString(p.render(presentationLabel, label))
		output.WriteString(strings.Repeat(" ", labelWidth-lipgloss.Width(label)+1))
		output.WriteString(p.render(row.role, row.value))
		output.WriteByte('\n')
	}
	return output.String()
}

func (p presentation) status(value string) string {
	return p.render(presentationSuccess, value) + "\n"
}

func (p presentation) cancellation(value string) string {
	return p.render(presentationMuted, value) + "\n"
}

func (p presentation) diagnostic(value string) string {
	return p.render(presentationError, value) + "\n"
}

func (p presentation) render(role presentationRole, value string) string {
	return p.style(role).Render(value)
}

func (p presentation) style(role presentationRole) lipgloss.Style {
	switch role {
	case presentationHeading:
		return p.styles.heading
	case presentationLabel:
		return p.styles.label
	case presentationSelected:
		return p.styles.selected
	case presentationMuted:
		return p.styles.muted
	case presentationSuccess:
		return p.styles.success
	case presentationError:
		return p.styles.error
	default:
		return p.styles.plain
	}
}

func (p presentation) huhTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		styles := huh.ThemeBase(isDark)
		styles.Group.Title = p.styles.heading
		styles.Group.Description = p.styles.muted

		styles.Focused.Title = p.styles.heading
		styles.Focused.NoteTitle = p.styles.heading
		styles.Focused.Description = p.styles.muted
		styles.Focused.ErrorIndicator = p.styles.error.SetString(" *")
		styles.Focused.ErrorMessage = p.styles.error
		styles.Focused.SelectSelector = p.styles.selected.SetString("> ")
		styles.Focused.Option = p.styles.plain
		styles.Focused.NextIndicator = p.styles.selected.SetString(">")
		styles.Focused.PrevIndicator = p.styles.selected.SetString("<")
		styles.Focused.MultiSelectSelector = p.styles.selected.SetString("> ")
		styles.Focused.SelectedOption = p.styles.selected
		styles.Focused.SelectedPrefix = p.styles.selected.SetString("[*] ")
		styles.Focused.UnselectedOption = p.styles.plain
		styles.Focused.UnselectedPrefix = p.styles.muted.SetString("[ ] ")
		styles.Focused.FocusedButton = p.styles.selected.Padding(0, 2).MarginRight(1)
		styles.Focused.BlurredButton = p.styles.muted.Padding(0, 2).MarginRight(1)
		styles.Focused.TextInput.Cursor = p.styles.selected
		styles.Focused.TextInput.Placeholder = p.styles.muted
		styles.Focused.TextInput.Prompt = p.styles.label
		styles.Focused.TextInput.Text = p.styles.plain

		styles.Blurred = styles.Focused
		styles.Blurred.Base = styles.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
		styles.Blurred.Card = styles.Blurred.Base
		styles.Blurred.Title = p.styles.muted
		styles.Blurred.NoteTitle = p.styles.muted
		styles.Blurred.SelectSelector = p.styles.muted.SetString("  ")
		styles.Blurred.MultiSelectSelector = p.styles.muted.SetString("  ")
		styles.Blurred.NextIndicator = lipgloss.NewStyle()
		styles.Blurred.PrevIndicator = lipgloss.NewStyle()
		styles.Blurred.TextInput.Prompt = p.styles.muted

		styles.Help.Ellipsis = p.styles.muted
		styles.Help.ShortKey = p.styles.muted
		styles.Help.ShortDesc = p.styles.muted
		styles.Help.ShortSeparator = p.styles.muted
		styles.Help.FullKey = p.styles.muted
		styles.Help.FullDesc = p.styles.muted
		styles.Help.FullSeparator = p.styles.muted
		return styles
	})
}

func profileListOutput(profiles []profile.Profile, activeName string, terminal Terminal) string {
	rows := [][]presentationCell{{
		{value: "#", role: presentationHeading},
		{value: "ACTIVE", role: presentationHeading},
		{value: "NAME", role: presentationHeading},
		{value: "ADDRESS", role: presentationHeading},
		{value: "NAMESPACE", role: presentationHeading},
	}}
	for index, candidate := range profile.NewService(profiles).List() {
		active := candidate.Name == activeName
		marker := ""
		markerRole := presentationPlain
		if active {
			marker = "*"
			markerRole = presentationSelected
		}
		namespace := candidate.Namespace
		if namespace == "" {
			namespace = "-"
		}
		rows = append(rows, []presentationCell{
			{value: strconv.Itoa(index + 1)},
			{value: marker, role: markerRole},
			{value: candidate.Name},
			{value: candidate.Address},
			{value: namespace},
		})
	}
	return newPresentation(terminal).renderTable(rows)
}

func profileShowOutput(candidate profile.Profile, active bool, terminal Terminal) string {
	namespace := candidate.Namespace
	if namespace == "" {
		namespace = "-"
	}
	activeValue := "no"
	activeRole := presentationPlain
	if active {
		activeValue = "yes"
		activeRole = presentationSelected
	}
	rows := []presentationDetail{
		{label: "Name", value: candidate.Name},
		{label: "Address", value: candidate.Address},
		{label: "Username", value: candidate.Username},
		{label: "Auth path", value: candidate.AuthPath},
		{label: "Namespace", value: namespace},
		{label: "Active", value: activeValue, role: activeRole},
	}
	return newPresentation(terminal).renderDetails(rows)
}
