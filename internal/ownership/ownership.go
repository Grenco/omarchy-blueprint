package ownership

import (
	"path/filepath"
	"sort"
)

type Claim struct {
	Provider         string
	Path             string
	Recursive        bool
	DelegateSymlinks bool
}

type Index struct{ Claims []Claim }

func (i Index) TrackConflict(path string) []Claim {
	return i.conflicts(path, false)
}

func (i Index) LinkConflict(path string) []Claim {
	return i.conflicts(path, true)
}

func (i Index) conflicts(path string, link bool) []Claim {
	var conflicts []Claim
	for _, claim := range i.Claims {
		if link && claim.DelegateSymlinks {
			continue
		}
		if overlapsClaim(path, claim) {
			conflicts = append(conflicts, claim)
		}
	}
	sort.Slice(conflicts, func(a, b int) bool {
		if conflicts[a].Provider == conflicts[b].Provider {
			return conflicts[a].Path < conflicts[b].Path
		}
		return conflicts[a].Provider < conflicts[b].Provider
	})
	return conflicts
}

func overlapsClaim(path string, claim Claim) bool {
	path, claimed := filepath.Clean(path), filepath.Clean(claim.Path)
	return path == claimed || contains(path, claimed) || (claim.Recursive && contains(claimed, path))
}

func contains(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && len(relative) >= 3 && relative[:3] != ".."+string(filepath.Separator)
}
