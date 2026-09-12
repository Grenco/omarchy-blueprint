package components

import "testing"

func TestTableEnsureAndViewport(t *testing.T) {
	table := Table{}
	table.Ensure(3, 5, 2)
	if table.Offset != 2 {
		t.Fatalf("offset=%d", table.Offset)
	}
	if got := table.View([]string{"a", "b", "c", "d", "e"}, 2); got != "c\nd" {
		t.Fatalf("view=%q", got)
	}
}
