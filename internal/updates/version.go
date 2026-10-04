// Package updates tells a person when a newer Blueprint release exists. It is
// passive: it never downloads, installs or runs anything, and its state is
// machine-local, separate from profiles, Restore and Sync.
package updates

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a semantic version (https://semver.org). Build metadata is
// accepted but ignored, as semver precedence requires.
type Version struct {
	Major, Minor, Patch uint64
	Prerelease          []string
}

// ParseVersion parses MAJOR.MINOR.PATCH[-PRERELEASE][+BUILD], with an
// optional leading "v".
func ParseVersion(s string) (Version, error) {
	text := strings.TrimPrefix(strings.TrimSpace(s), "v")
	if build := strings.IndexByte(text, '+'); build >= 0 {
		if !validIdentifiers(text[build+1:], false) {
			return Version{}, fmt.Errorf("invalid version %q: bad build metadata", s)
		}
		text = text[:build]
	}
	var prerelease []string
	if dash := strings.IndexByte(text, '-'); dash >= 0 {
		if !validIdentifiers(text[dash+1:], true) {
			return Version{}, fmt.Errorf("invalid version %q: bad prerelease", s)
		}
		prerelease = strings.Split(text[dash+1:], ".")
		text = text[:dash]
	}
	core := strings.Split(text, ".")
	if len(core) != 3 {
		return Version{}, fmt.Errorf("invalid version %q: want MAJOR.MINOR.PATCH", s)
	}
	var numbers [3]uint64
	for i, part := range core {
		if !numeric(part) || len(part) > 1 && part[0] == '0' {
			return Version{}, fmt.Errorf("invalid version %q", s)
		}
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("invalid version %q: %w", s, err)
		}
		numbers[i] = n
	}
	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2], Prerelease: prerelease}, nil
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Prerelease) > 0 {
		s += "-" + strings.Join(v.Prerelease, ".")
	}
	return s
}

// Compare returns -1, 0 or 1 as v has lower, equal or higher precedence
// than other.
func (v Version) Compare(other Version) int {
	for _, pair := range [][2]uint64{{v.Major, other.Major}, {v.Minor, other.Minor}, {v.Patch, other.Patch}} {
		if pair[0] != pair[1] {
			return cmpUint(pair[0], pair[1])
		}
	}
	// A prerelease has lower precedence than its release.
	switch {
	case len(v.Prerelease) == 0 && len(other.Prerelease) == 0:
		return 0
	case len(v.Prerelease) == 0:
		return 1
	case len(other.Prerelease) == 0:
		return -1
	}
	for i := 0; i < len(v.Prerelease) && i < len(other.Prerelease); i++ {
		a, b := v.Prerelease[i], other.Prerelease[i]
		if a == b {
			continue
		}
		aNum, bNum := numeric(a), numeric(b)
		switch {
		case aNum && bNum:
			x, _ := strconv.ParseUint(a, 10, 64)
			y, _ := strconv.ParseUint(b, 10, 64)
			return cmpUint(x, y)
		case aNum:
			return -1 // numeric identifiers sort before alphanumeric ones
		case bNum:
			return 1
		case a < b:
			return -1
		default:
			return 1
		}
	}
	return cmpUint(uint64(len(v.Prerelease)), uint64(len(other.Prerelease)))
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func numeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// validIdentifiers checks dot-separated [0-9A-Za-z-] identifiers; numeric
// prerelease identifiers may not have leading zeros.
func validIdentifiers(s string, prerelease bool) bool {
	if s == "" {
		return false
	}
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		for _, r := range id {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
				return false
			}
		}
		if prerelease && numeric(id) && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	return true
}
