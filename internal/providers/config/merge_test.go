package config

import (
	"bytes"
	"strings"
	"testing"
)

func TestIsMergeableText(t *testing.T) {
	atLimit := bytes.Repeat([]byte("a"), maxMergeableTextSize)
	for _, test := range []struct {
		name    string
		content []byte
		want    bool
	}{
		{"valid at limit", atLimit, true},
		{"over limit", append(atLimit, 'a'), false},
		{"nul", []byte("one\x00two"), false},
		{"invalid utf8", []byte{0xff}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsMergeableText(test.content); got != test.want {
				t.Fatalf("IsMergeableText() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMergeText3CleanNonOverlappingEdits(t *testing.T) {
	for _, test := range []struct {
		name, base, desired, current, want string
	}{
		{"edit and insertion", "alpha\nbeta\n", "alpha\nbeta-user\n", "upstream\nalpha\nbeta\n", "upstream\nalpha\nbeta-user\n"},
		{"deletion and insertion", "alpha\nbeta\ngamma\n", "alpha\ngamma\n", "alpha\nbeta\nupstream\ngamma\n", "alpha\nupstream\ngamma\n"},
		{"independent additions", "alpha\nbeta\n", "alpha\nuser\nbeta\n", "alpha\nbeta\nupstream\n", "alpha\nuser\nbeta\nupstream\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := MergeText3([]byte(test.base), []byte(test.desired), []byte(test.current))
			if err != nil {
				t.Fatal(err)
			}
			if got.Conflicts || string(got.Content) != test.want {
				t.Fatalf("MergeText3() = %#v, want content %q without conflict", got, test.want)
			}
		})
	}
}

func TestMergeText3OverlapConflicts(t *testing.T) {
	got, err := MergeText3([]byte("value=old\n"), []byte("value=user\n"), []byte("value=upstream\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Conflicts {
		t.Fatalf("MergeText3() = %#v, want conflict", got)
	}
}

func TestMergeText3FastPaths(t *testing.T) {
	for _, test := range []struct{ base, desired, current, want string }{
		{"base\n", "base\n", "current\n", "current\n"},
		{"base\n", "desired\n", "base\n", "desired\n"},
		{"base\n", "same\n", "same\n", "same\n"},
	} {
		got, err := MergeText3([]byte(test.base), []byte(test.desired), []byte(test.current))
		if err != nil || got.Conflicts || string(got.Content) != test.want {
			t.Fatalf("MergeText3() = %#v, %v; want %q without conflict", got, err, test.want)
		}
	}
}

func TestMergeText3NormalizesCRLFDeterministically(t *testing.T) {
	got, err := MergeText3([]byte("alpha\r\nbeta\r\n"), []byte("alpha\r\nbeta-user\r\n"), []byte("upstream\nalpha\nbeta\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Conflicts || string(got.Content) != "upstream\r\nalpha\r\nbeta-user\r\n" {
		t.Fatalf("MergeText3() = %#v", got)
	}
	if !IsMergeableText([]byte(strings.ReplaceAll(string(got.Content), "\r\n", "\n"))) {
		t.Fatal("merged CRLF text was classified as non-mergeable")
	}
}
