package components

import "strings"

// Table is a bounded row viewport. Its owner retains selection semantics.
type Table struct{ Offset int }

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
