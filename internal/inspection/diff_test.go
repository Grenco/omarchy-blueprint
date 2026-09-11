package inspection

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildTextDiffSafetyAndShape(t *testing.T) {
	doc := BuildTextDiff("old", []byte("one\ntwo\nthree\n"), "new", []byte("one\nchanged\nthree\n"))
	if doc.Kind != DiffText || len(doc.Hunks) != 1 || len(doc.Hunks[0].Lines) != 4 {
		t.Fatalf("text document = %#v", doc)
	}
	if doc.Hunks[0].Lines[1].Kind != "remove" || doc.Hunks[0].Lines[1].OldLine != 2 || doc.Hunks[0].Lines[2].Kind != "add" || doc.Hunks[0].Lines[2].NewLine != 2 {
		t.Fatalf("changed lines = %#v", doc.Hunks[0].Lines)
	}
	if got := BuildTextDiff("old", nil, "new", []byte("new\n")); got.Kind != DiffText || got.Hunks[0].Lines[0].Kind != "add" {
		t.Fatalf("new document = %#v", got)
	}
	for _, test := range []struct {
		name     string
		old, new []byte
		kind     DiffKind
	}{
		{"binary", nil, []byte{'a', 0}, DiffBinary},
		{"sensitive", nil, []byte("api_token = tokenvaluewithmorethan16chars\n"), DiffSensitive},
		{"bytes", nil, bytes.Repeat([]byte("x"), MaxPreviewBytes+1), DiffTooLarge},
		{"lines", nil, []byte(strings.Repeat("x\n", MaxPreviewLines+1)), DiffTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := BuildTextDiff("old", test.old, "new", test.new); got.Kind != test.kind || len(got.Hunks) != 0 {
				t.Fatalf("document = %#v", got)
			}
		})
	}
}

func TestBuildTextDiffMetadataAndSanitization(t *testing.T) {
	if got := BuildTextDiff("old", []byte("same\n"), "new", []byte("same\n")); got.Kind != DiffMetadata {
		t.Fatalf("same document = %#v", got)
	}
	input := "\x1b]0;bad\a\t" + strings.Repeat("x", MaxPreviewLineBytes+1) + "\n"
	doc := BuildTextDiff("old", nil, "new", []byte(input))
	line := doc.Hunks[0].Lines[0]
	if strings.ContainsRune(line.Text, '\x1b') || !strings.Contains(line.Text, "\t") || len(line.Text) != MaxPreviewLineBytes || len(doc.Metadata) == 0 {
		t.Fatalf("sanitized line = %#v, metadata = %#v", line, doc.Metadata)
	}
}
