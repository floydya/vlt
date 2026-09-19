package cli

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"vlt/internal/profile"
)

type profilePresentation struct {
	color bool
}

type profileTableCell struct {
	value  string
	header bool
	active bool
}

func newProfilePresentation(terminal Terminal) profilePresentation {
	return profilePresentation{color: terminal != nil && terminal.ColorEnabled()}
}

func (p profilePresentation) list(profiles []profile.Profile, activeName string) string {
	rows := [][]profileTableCell{{
		{value: "#", header: true},
		{value: "ACTIVE", header: true},
		{value: "NAME", header: true},
		{value: "ADDRESS", header: true},
		{value: "NAMESPACE", header: true},
	}}
	for index, candidate := range profiles {
		active := candidate.Name == activeName
		marker := ""
		if active {
			marker = "*"
		}
		namespace := candidate.Namespace
		if namespace == "" {
			namespace = "-"
		}
		rows = append(rows, []profileTableCell{
			{value: strconv.Itoa(index + 1)},
			{value: marker, active: active},
			{value: candidate.Name},
			{value: candidate.Address},
			{value: namespace},
		})
	}
	return p.table(rows)
}

func (p profilePresentation) details(candidate profile.Profile, active bool) string {
	namespace := candidate.Namespace
	if namespace == "" {
		namespace = "-"
	}
	activeValue := "no"
	if active {
		activeValue = "yes"
	}
	rows := []struct {
		label  string
		value  string
		active bool
	}{
		{label: "Name:", value: candidate.Name},
		{label: "Address:", value: candidate.Address},
		{label: "Username:", value: candidate.Username},
		{label: "Auth path:", value: candidate.AuthPath},
		{label: "Namespace:", value: namespace},
		{label: "Active:", value: activeValue, active: active},
	}
	labelWidth := 0
	for _, row := range rows {
		labelWidth = max(labelWidth, lipgloss.Width(row.label))
	}
	var output strings.Builder
	for _, row := range rows {
		output.WriteString(p.heading(row.label))
		output.WriteString(strings.Repeat(" ", labelWidth-lipgloss.Width(row.label)+1))
		if row.active {
			output.WriteString(p.selected(row.value))
		} else {
			output.WriteString(row.value)
		}
		output.WriteByte('\n')
	}
	return output.String()
}

func (p profilePresentation) table(rows [][]profileTableCell) string {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for column, cell := range row {
			widths[column] = max(widths[column], lipgloss.Width(cell.value))
		}
	}
	var output strings.Builder
	for _, row := range rows {
		for column, cell := range row {
			value := cell.value
			switch {
			case cell.header:
				value = p.heading(value)
			case cell.active:
				value = p.selected(value)
			}
			output.WriteString(value)
			if column < len(row)-1 {
				output.WriteString(strings.Repeat(" ", widths[column]-lipgloss.Width(cell.value)+2))
			}
		}
		output.WriteByte('\n')
	}
	return output.String()
}

func (p profilePresentation) heading(value string) string {
	if !p.color {
		return value
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Cyan).Render(value)
}

func (p profilePresentation) selected(value string) string {
	if !p.color {
		return value
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Green).Render(value)
}
