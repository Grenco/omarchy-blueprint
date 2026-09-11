// Package inspection provides safe, presentation-independent inspection data.
package inspection

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	"github.com/Grenco/omarchy-blueprint/internal/sensitive"
	"github.com/sergi/go-diff/diffmatchpatch"
)

const (
	MaxPreviewBytes     = 2 << 20
	MaxPreviewLines     = 20_000
	MaxPreviewLineBytes = 16 << 10
)

type DiffKind string

const (
	DiffText        DiffKind = "text"
	DiffBinary      DiffKind = "binary"
	DiffSensitive   DiffKind = "sensitive"
	DiffTooLarge    DiffKind = "too-large"
	DiffMetadata    DiffKind = "metadata"
	DiffUnavailable DiffKind = "unavailable"
)

type DiffFact struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type DiffLine struct {
	Kind    string `json:"kind"`
	OldLine int    `json:"old_line,omitempty"`
	NewLine int    `json:"new_line,omitempty"`
	Text    string `json:"text"`
}

type DiffHunk struct {
	OldStart int        `json:"old_start"`
	OldCount int        `json:"old_count"`
	NewStart int        `json:"new_start"`
	NewCount int        `json:"new_count"`
	Lines    []DiffLine `json:"lines"`
}

type DiffDocument struct {
	Kind     DiffKind   `json:"kind"`
	Title    string     `json:"title,omitempty"`
	OldLabel string     `json:"old_label,omitempty"`
	NewLabel string     `json:"new_label,omitempty"`
	Hunks    []DiffHunk `json:"hunks,omitempty"`
	Metadata []DiffFact `json:"metadata,omitempty"`
}

// BuildTextDiff creates a bounded, terminal-safe structured diff. Callers must
// classify filesystem objects before supplying their contents.
func BuildTextDiff(oldLabel string, old []byte, newLabel string, new []byte) DiffDocument {
	doc := DiffDocument{OldLabel: oldLabel, NewLabel: newLabel}
	if bytes.IndexByte(old, 0) >= 0 || bytes.IndexByte(new, 0) >= 0 {
		doc.Kind = DiffBinary
		return doc
	}
	if len(old) > MaxPreviewBytes || len(new) > MaxPreviewBytes {
		doc.Kind = DiffTooLarge
		doc.Metadata = []DiffFact{{Key: "limit", Value: fmt.Sprintf("content exceeds %d bytes", MaxPreviewBytes)}}
		return doc
	}
	if result, _ := sensitive.ScanReader(bytes.NewReader(old), MaxPreviewBytes); result.Sensitive {
		doc.Kind = DiffSensitive
		doc.Metadata = []DiffFact{{Key: "reason", Value: result.Reason}}
		return doc
	}
	if result, _ := sensitive.ScanReader(bytes.NewReader(new), MaxPreviewBytes); result.Sensitive {
		doc.Kind = DiffSensitive
		doc.Metadata = []DiffFact{{Key: "reason", Value: result.Reason}}
		return doc
	}
	if logicalLines(old) > MaxPreviewLines || logicalLines(new) > MaxPreviewLines {
		doc.Kind = DiffTooLarge
		doc.Metadata = []DiffFact{{Key: "limit", Value: fmt.Sprintf("content exceeds %d lines", MaxPreviewLines)}}
		return doc
	}
	if bytes.Equal(old, new) {
		doc.Kind = DiffMetadata
		doc.Metadata = []DiffFact{{Key: "content", Value: "unchanged"}}
		return doc
	}

	dmp := diffmatchpatch.New()
	oldChars, newChars, lines := dmp.DiffLinesToChars(string(old), string(new))
	diffs := dmp.DiffMain(oldChars, newChars, false)
	diffs = dmp.DiffCharsToLines(diffs, lines)
	hunk := DiffHunk{OldStart: 1, NewStart: 1}
	oldLine, newLine := 1, 1
	truncated := false
	for _, part := range diffs {
		for _, text := range splitLines(part.Text) {
			line := DiffLine{Text: sanitizeLine(text)}
			if len(line.Text) > MaxPreviewLineBytes {
				line.Text = line.Text[:MaxPreviewLineBytes]
				truncated = true
			}
			switch part.Type {
			case diffmatchpatch.DiffEqual:
				line.Kind, line.OldLine, line.NewLine = "context", oldLine, newLine
				oldLine, newLine = oldLine+1, newLine+1
			case diffmatchpatch.DiffDelete:
				line.Kind, line.OldLine = "remove", oldLine
				oldLine++
			case diffmatchpatch.DiffInsert:
				line.Kind, line.NewLine = "add", newLine
				newLine++
			}
			hunk.Lines = append(hunk.Lines, line)
		}
	}
	hunk.OldCount, hunk.NewCount = oldLine-1, newLine-1
	doc.Kind, doc.Hunks = DiffText, []DiffHunk{hunk}
	if truncated {
		doc.Metadata = []DiffFact{{Key: "line-truncated", Value: fmt.Sprintf("lines capped at %d bytes", MaxPreviewLineBytes)}}
	}
	return doc
}

func logicalLines(content []byte) int { return len(splitLines(string(content))) }

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}

func sanitizeLine(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || (r >= 0x20 && r != 0x7f && !((r >= 0x80 && r <= 0x9f) || unicode.IsControl(r))) {
			return r
		}
		return '?'
	}, text)
}
