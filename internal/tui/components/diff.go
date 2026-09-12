package components

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
)

const sideBySideMinWidth = 100

// DiffViewer renders only the bounded, structured inspection document.
type DiffViewer struct {
	document   inspection.DiffDocument
	width      int
	height     int
	offset     int
	sideBySide bool
}

func NewDiffViewer(document inspection.DiffDocument) DiffViewer {
	return DiffViewer{document: document, height: 20}
}

func (v *DiffViewer) SetSize(width, height int) {
	v.width, v.height = width, height
	if width < sideBySideMinWidth {
		v.sideBySide = false
	}
	v.clampOffset(len(v.lines()))
}

func (v *DiffViewer) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	lines := v.lines()
	switch key.String() {
	case "j", "down":
		v.offset++
	case "k", "up":
		v.offset--
	case "pgdown":
		v.offset += v.pageSize()
	case "pgup":
		v.offset -= v.pageSize()
	case "s":
		if v.width >= sideBySideMinWidth && v.document.Kind == inspection.DiffText {
			v.sideBySide = !v.sideBySide
			v.offset = 0
		}
	case "{":
		v.moveHunk(-1)
	case "}":
		v.moveHunk(1)
	}
	v.clampOffset(len(lines))
	return nil
}

func (v DiffViewer) View() string {
	lines := v.lines()
	if len(lines) == 0 {
		return "No diff available."
	}
	start, end := v.offset, min(len(lines), v.offset+v.pageSize())
	return strings.Join(lines[start:end], "\n")
}

func (v DiffViewer) lines() []string {
	if v.document.Kind != inspection.DiffText {
		return metadataLines(v.document)
	}
	if v.sideBySide && v.width >= sideBySideMinWidth {
		return v.sideBySideLines()
	}
	lines := []string{"--- " + literal(v.document.OldLabel), "+++ " + literal(v.document.NewLabel)}
	for _, hunk := range v.document.Hunks {
		lines = append(lines, fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount))
		for _, line := range hunk.Lines {
			prefix := " "
			if line.Kind == "add" {
				prefix = "+"
			}
			if line.Kind == "remove" {
				prefix = "-"
			}
			lines = appendWrapped(lines, fmt.Sprintf("%s%4s %4s %s", prefix, lineNumber(line.OldLine), lineNumber(line.NewLine), visibleWhitespace(line.Text)), v.width)
		}
	}
	return lines
}

func (v DiffViewer) sideBySideLines() []string {
	column := max(20, (v.width-3)/2)
	lines := []string{diffPad(literal(v.document.OldLabel), column) + " | " + literal(v.document.NewLabel)}
	for _, hunk := range v.document.Hunks {
		lines = append(lines, fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount))
		for _, line := range hunk.Lines {
			old, new := "", ""
			if line.Kind != "add" {
				old = fmt.Sprintf("%4s %s", lineNumber(line.OldLine), visibleWhitespace(line.Text))
			}
			if line.Kind != "remove" {
				new = fmt.Sprintf("%4s %s", lineNumber(line.NewLine), visibleWhitespace(line.Text))
			}
			lines = append(lines, diffPad(old, column)+" | "+new)
		}
	}
	return lines
}

func metadataLines(document inspection.DiffDocument) []string {
	lines := []string{literal(string(document.Kind)) + " diff"}
	if document.Title != "" {
		lines = append(lines, literal(document.Title))
	}
	for _, fact := range document.Metadata {
		lines = append(lines, literal(fact.Key)+": "+literal(fact.Value))
	}
	return lines
}

func (v *DiffViewer) moveHunk(delta int) {
	starts := []int{}
	position := 2
	for _, hunk := range v.document.Hunks {
		starts, position = append(starts, position), position+1+len(hunk.Lines)
	}
	if len(starts) == 0 {
		return
	}
	target := 0
	for i, start := range starts {
		if start <= v.offset {
			target = i
		}
	}
	target = min(len(starts)-1, max(0, target+delta))
	v.offset = starts[target]
}

func (v *DiffViewer) clampOffset(lineCount int) {
	v.offset = min(max(0, v.offset), max(0, lineCount-v.pageSize()))
}
func (v DiffViewer) pageSize() int { return max(1, v.height) }
func lineNumber(number int) string {
	if number == 0 {
		return ""
	}
	return fmt.Sprint(number)
}
func diffPad(value string, width int) string {
	if len(value) >= width {
		return value[:width]
	}
	return value + strings.Repeat(" ", width-len(value))
}
func appendWrapped(lines []string, value string, width int) []string {
	if width <= 0 {
		return append(lines, value)
	}
	for len(value) > width {
		lines, value = append(lines, value[:width]), "      "+value[width:]
	}
	return append(lines, value)
}
func visibleWhitespace(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return '→'
		}
		if unicode.IsSpace(r) {
			return '·'
		}
		return r
	}, literal(value))
}
func literal(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return '?'
		}
		return r
	}, value)
}
