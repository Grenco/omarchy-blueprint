package config

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	maxMergeableTextSize = 4 << 20
	maxDiffCells         = 8_000_000
)

// MergeResult is the result of applying the desired user's delta to the
// current baseline. Content is nil when Conflicts is true; callers must not
// write conflict markers into live configuration files.
type MergeResult struct {
	Content   []byte
	Conflicts bool
}

// IsMergeableText reports whether content is valid UTF-8 text without NUL
// bytes and no larger than 4 MiB. The 4 MiB limit is inclusive.
func IsMergeableText(content []byte) bool {
	return len(content) <= maxMergeableTextSize && !bytes.Contains(content, []byte{0}) && utf8.Valid(content)
}

// MergeText3 applies the base-to-desired line edits to currentBaseline.
// Internally, CRLF is normalized to LF. Output uses desired's first observed
// newline convention, falling back to currentBaseline and then base, so a
// captured user's CRLF convention wins whenever it is unambiguous.
func MergeText3(base, desired, currentBaseline []byte) (MergeResult, error) {
	if !IsMergeableText(base) || !IsMergeableText(desired) || !IsMergeableText(currentBaseline) {
		return MergeResult{}, errors.New("three-way merge requires mergeable text inputs")
	}
	if bytes.Equal(base, desired) {
		return MergeResult{Content: append([]byte(nil), currentBaseline...)}, nil
	}
	if bytes.Equal(base, currentBaseline) || bytes.Equal(desired, currentBaseline) {
		return MergeResult{Content: append([]byte(nil), desired...)}, nil
	}

	baseLines := splitMergeLines(base)
	desiredLines := splitMergeLines(desired)
	currentLines := splitMergeLines(currentBaseline)
	desiredEdits, err := lineEdits(baseLines, desiredLines)
	if err != nil {
		return MergeResult{}, err
	}
	currentEdits, err := lineEdits(baseLines, currentLines)
	if err != nil {
		return MergeResult{}, err
	}

	merged, conflict := mergeLineEdits(baseLines, desiredEdits, currentEdits)
	if conflict {
		return MergeResult{Conflicts: true}, nil
	}
	return MergeResult{Content: joinMergeLines(merged, newlineConvention(desired, currentBaseline, base))}, nil
}

type lineEdit struct {
	start, end int
	lines      []string
}

func splitMergeLines(content []byte) []string {
	return strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
}

func joinMergeLines(lines []string, newline string) []byte {
	return []byte(strings.Join(lines, newline))
}

func newlineConvention(contents ...[]byte) string {
	for _, content := range contents {
		for i := 0; i < len(content); i++ {
			if content[i] == '\n' {
				if i > 0 && content[i-1] == '\r' {
					return "\r\n"
				}
				return "\n"
			}
		}
	}
	return "\n"
}

func lineEdits(base, next []string) ([]lineEdit, error) {
	prefix := 0
	for prefix < len(base) && prefix < len(next) && base[prefix] == next[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(base)-prefix && suffix < len(next)-prefix && base[len(base)-1-suffix] == next[len(next)-1-suffix] {
		suffix++
	}
	a, b := base[prefix:len(base)-suffix], next[prefix:len(next)-suffix]
	if len(a) == 0 || len(b) == 0 {
		if len(a) == len(b) {
			return nil, nil
		}
		return []lineEdit{{start: prefix, end: prefix + len(a), lines: append([]string(nil), b...)}}, nil
	}
	if len(a) > maxDiffCells/len(b) {
		if noSharedLines(a, b) {
			return []lineEdit{{start: prefix, end: prefix + len(a), lines: append([]string(nil), b...)}}, nil
		}
		return nil, errors.New("three-way merge diff is too complex")
	}

	width := len(b) + 1
	directions := make([]byte, (len(a)+1)*width)
	previous, current := make([]int, width), make([]int, width)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			at := i*width + j
			if a[i] == b[j] {
				current[j] = previous[j+1] + 1
				directions[at] = 1
			} else if previous[j] >= current[j+1] {
				current[j] = previous[j]
				directions[at] = 2
			} else {
				current[j] = current[j+1]
				directions[at] = 3
			}
		}
		previous, current = current, previous
		clear(current)
	}

	var edits []lineEdit
	i, j := 0, 0
	var edit *lineEdit
	flush := func() {
		if edit != nil {
			edits = append(edits, *edit)
			edit = nil
		}
	}
	for i < len(a) || j < len(b) {
		if i < len(a) && j < len(b) && directions[i*width+j] == 1 {
			flush()
			i++
			j++
			continue
		}
		if edit == nil {
			edit = &lineEdit{start: prefix + i, end: prefix + i}
		}
		if j == len(b) || (i < len(a) && directions[i*width+j] == 2) {
			i++
			edit.end = prefix + i
		} else {
			edit.lines = append(edit.lines, b[j])
			j++
		}
	}
	flush()
	return edits, nil
}

func noSharedLines(a, b []string) bool {
	seen := make(map[string]struct{}, len(a))
	for _, line := range a {
		seen[line] = struct{}{}
	}
	for _, line := range b {
		if _, ok := seen[line]; ok {
			return false
		}
	}
	return true
}

func mergeLineEdits(base []string, desired, current []lineEdit) ([]string, bool) {
	merged := make([]string, 0, len(base))
	position, di, ci := 0, 0, 0
	for di < len(desired) || ci < len(current) {
		dnext, cnext := len(base), len(base)
		if di < len(desired) {
			dnext = desired[di].start
		}
		if ci < len(current) {
			cnext = current[ci].start
		}
		next := min(dnext, cnext)
		merged = append(merged, base[position:next]...)
		if dnext == cnext {
			d, c := desired[di], current[ci]
			if d.end != c.end || !sameLines(d.lines, c.lines) {
				return nil, true
			}
			merged = append(merged, d.lines...)
			position = d.end
			di++
			ci++
			continue
		}
		if dnext < cnext {
			d := desired[di]
			if cnext < d.end {
				return nil, true
			}
			merged = append(merged, d.lines...)
			position = d.end
			di++
			continue
		}
		c := current[ci]
		if dnext < c.end {
			return nil, true
		}
		merged = append(merged, c.lines...)
		position = c.end
		ci++
	}
	return append(merged, base[position:]...), false
}

func sameLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
