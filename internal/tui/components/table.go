package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Table is a bounded row viewport. Its owner retains selection semantics.
type Table struct{ Offset int }

type Column struct {
	Title    string
	Width    int
	MinWidth int
}

type Row struct {
	Cells    []string
	Selected bool
}

func (t *Table) Move(delta, count, height int) {
	t.Offset = max(0, min(t.Offset+delta, max(0, count-max(1, height))))
}

func (t *Table) Ensure(index, count, height int) {
	if height <= 0 || count == 0 {
		t.Offset = 0
		return
	}
	if index < t.Offset {
		t.Offset = index
	}
	if index >= t.Offset+height {
		t.Offset = index - height + 1
	}
	t.Move(0, count, height)
}

func (t *Table) View(rows []string, height int) string {
	if height <= 0 || len(rows) == 0 {
		return ""
	}
	t.Move(0, len(rows), height)
	return strings.Join(rows[t.Offset:min(len(rows), t.Offset+height)], "\n")
}

// Render lays out actual cells, retaining the header while the data viewport
// scrolls. Columns that cannot meet their minimum width are hidden from right.
func (t *Table) Render(columns []Column, rows []Row, width, height int, styles Styles) string {
	if width <= 0 || height <= 0 || len(columns) == 0 {
		return ""
	}
	columns, widths := tableColumns(columns, width)
	if len(columns) == 0 {
		return ""
	}
	dataHeight := max(0, height-1)
	t.Move(0, len(rows), dataHeight)
	lines := []string{tableLine(columnTitles(columns), widths)}
	for _, row := range rows[t.Offset:min(len(rows), t.Offset+dataHeight)] {
		if row.Selected && !styles.Palette.ColorEnabled && len(row.Cells) > 0 {
			row.Cells = append([]string(nil), row.Cells...)
			row.Cells[0] = Icons.Selected + " " + row.Cells[0]
		}
		line := tableLine(row.Cells, widths)
		if row.Selected {
			line = styles.Selection(line, true)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func tableColumns(columns []Column, width int) ([]Column, []int) {
	visible := append([]Column(nil), columns...)
	for len(visible) > 1 && tableMinimum(visible) > width {
		visible = visible[:len(visible)-1]
	}
	if tableMinimum(visible) > width {
		return nil, nil
	}
	widths := make([]int, len(visible))
	remaining := width - (len(visible) - 1)
	for i, column := range visible {
		minimum := max(1, column.MinWidth, lipgloss.Width(column.Title))
		widths[i] = minimum
		remaining -= minimum
	}
	for remaining > 0 {
		for i, column := range visible {
			if remaining == 0 {
				break
			}
			if column.Width == 0 || widths[i] < column.Width {
				widths[i]++
				remaining--
			}
		}
	}
	return visible, widths
}

func tableMinimum(columns []Column) int {
	minimum := len(columns) - 1
	for _, column := range columns {
		minimum += max(1, column.MinWidth, lipgloss.Width(column.Title))
	}
	return minimum
}
func columnTitles(columns []Column) []string {
	titles := make([]string, len(columns))
	for i, column := range columns {
		titles[i] = column.Title
	}
	return titles
}
func tableLine(cells []string, widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		value := ""
		if i < len(cells) {
			value = cells[i]
		}
		parts[i] = lipgloss.NewStyle().Width(width).MaxWidth(width).Render(value)
	}
	return strings.Join(parts, " ")
}
