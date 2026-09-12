package components

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/charmbracelet/x/ansi"
)

const sideBySideMinWidth = 100

// DiffViewer renders only the bounded, structured inspection document.
type DiffViewer struct {
	document   inspection.DiffDocument
	width      int
	height     int
	offset     int
	sideBySide bool
	whitespace bool
	styles     Styles
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

// SetStyles supplies optional semantic add/remove styling.
func (v *DiffViewer) SetStyles(styles Styles) { v.styles = styles }

func (v *DiffViewer) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		v.offset++
	case "k", "up":
		v.offset--
	case "pgdown":
		v.offset += v.pageSize()
	case "pgup":
		v.offset -= v.pageSize()
	case "home":
		v.offset = 0
	case "end":
		v.offset = len(v.lines())
	case "w":
		v.whitespace = !v.whitespace
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
	v.clampOffset(len(v.lines()))
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
		lines := metadataLines(v.document)
		for i := range lines {
			lines[i] = truncate(lines[i], v.width)
		}
		return lines
	}
	if v.sideBySide && v.width >= sideBySideMinWidth {
		return v.sideBySideLines()
	}
	lines := []string{truncate("--- "+literal(v.document.OldLabel), v.width), truncate("+++ "+literal(v.document.NewLabel), v.width)}
	for _, hunk := range v.document.Hunks {
		lines = append(lines, truncate(fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount), v.width))
		for _, line := range hunk.Lines {
			prefix := " "
			if line.Kind == "add" {
				prefix = "+"
			}
			if line.Kind == "remove" {
				prefix = "-"
			}
			value := fmt.Sprintf("%s%4s %4s %s", prefix, lineNumber(line.OldLine), lineNumber(line.NewLine), v.text(line.Text))
			for _, wrapped := range appendWrapped(nil, value, v.width) {
				lines = append(lines, v.styleLine(line.Kind, wrapped))
			}
		}
	}
	return lines
}

func (v DiffViewer) sideBySideLines() []string {
	column := max(20, (v.width-3)/2)
	lines := []string{diffPad(literal(v.document.OldLabel), column) + " | " + diffPad(literal(v.document.NewLabel), column)}
	for _, hunk := range v.document.Hunks {
		lines = append(lines, truncate(fmt.Sprintf("@@ -%d,%d +%d,%d @@", hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount), v.width))
		for _, line := range hunk.Lines {
			old, new := "", ""
			if line.Kind != "add" {
				old = fmt.Sprintf("%4s %s", lineNumber(line.OldLine), v.text(line.Text))
			}
			if line.Kind != "remove" {
				new = fmt.Sprintf("%4s %s", lineNumber(line.NewLine), v.text(line.Text))
			}
			lines = append(lines, v.styleLine(line.Kind, diffPad(old, column)+" | "+diffPad(new, column)))
		}
	}
	for i := range lines {
		lines[i] = truncate(lines[i], v.width)
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
	for position, line := range v.lines() {
		if strings.HasPrefix(line, "@@ ") {
			starts = append(starts, position)
		}
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
	value = truncate(value, width)
	if lipgloss.Width(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-lipgloss.Width(value))
}
func appendWrapped(lines []string, value string, width int) []string {
	if width <= 0 {
		return append(lines, value)
	}
	continuation := "           "
	for first := true; lipgloss.Width(value) > width; first = false {
		prefix, available := "", width
		if !first && width > lipgloss.Width(continuation) {
			prefix, available = continuation, width-lipgloss.Width(continuation)
		}
		part := ansi.Cut(value, 0, available)
		lines, value = append(lines, prefix+part), strings.TrimPrefix(value, part)
	}
	if len(lines) > 0 && width > lipgloss.Width(continuation) {
		value = continuation + value
	}
	return append(lines, truncate(value, width))
}

func truncate(value string, width int) string {
	if width <= 0 {
		return value
	}
	return ansi.Truncate(value, width, "")
}

func (v DiffViewer) text(value string) string {
	if v.whitespace {
		return visibleWhitespace(value)
	}
	return literal(value)
}

func (v DiffViewer) styleLine(kind, value string) string {
	switch kind {
	case "add":
		return v.styles.Added(value)
	case "remove":
		return v.styles.Removed(value)
	default:
		return value
	}
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
		if (r < 0x20 && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return '?'
		}
		return r
	}, value)
}
