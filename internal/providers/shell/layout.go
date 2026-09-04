package shell

import "sort"

var layoutSections = []string{"left", "center", "right"}

type widgetPlacement struct {
	ID      string
	Section string
	Index   int
	Entry   any
}

type layoutState struct {
	placements map[string][]widgetPlacement
	valid      bool
}

// reconcileBarLayout applies source-owned widget placement across all three
// bar sections after generic JSON merging. It deliberately leaves generic
// semantics intact outside identifiable, unique widget entries.
func reconcileBarLayout(sourceBaseline, sourceDesired, targetCurrent map[string]any, result MergeResult, options MergeOptions) MergeResult {
	a, b, target := indexLayout(sourceBaseline), indexLayout(sourceDesired), indexLayout(targetCurrent)
	if !a.valid || !b.valid {
		return preserveAmbiguousSourceLayout(targetCurrent, result, options)
	}
	mergedLayout, ok := layoutMap(result.Value)
	if !ok {
		return result
	}
	original := cloneLayout(mergedLayout)
	modified := map[string]bool{}
	resolvedSections := map[string]bool{}
	ids := map[string]bool{}
	for id := range a.placements {
		ids[id] = true
	}
	for id := range b.placements {
		ids[id] = true
	}
	names := make([]string, 0, len(ids))
	for id := range ids {
		names = append(names, id)
	}
	sort.Strings(names)
	for _, id := range names {
		before, desired := singlePlacement(a, id), singlePlacement(b, id)
		if !placementChanged(id, before, desired, a, b) {
			continue
		}
		targetPlacements := target.placements[id]
		if len(targetPlacements) > 1 && !options.Force {
			return preserveAmbiguousTargetLayout(targetCurrent, result)
		}
		if before != nil {
			resolvedSections[before.Section] = true
		}
		if desired != nil {
			resolvedSections[desired.Section] = true
		}
		for _, placement := range targetPlacements {
			resolvedSections[placement.Section] = true
		}
		removeWidget(mergedLayout, id, modified)
		if desired != nil {
			insertWidget(mergedLayout, *desired, b, modified)
		}
	}
	if len(resolvedSections) == 0 {
		return result
	}
	result.Applied = filterLayoutChanges(result.Applied, resolvedSections)
	result.Conflicts = filterLayoutConflicts(result.Conflicts, resolvedSections)
	for _, section := range layoutSections {
		if !modified[section] || nodeEqual(mergeNode{Present: true, Value: original[section]}, mergeNode{Present: true, Value: mergedLayout[section]}) {
			continue
		}
		result.Applied = append(result.Applied, MergeChange{
			Path:   []string{"bar", "layout", section},
			Before: original[section],
			After:  cloneValue(mergedLayout[section]),
		})
	}
	return result
}

func widgetID(entry any) (string, bool) {
	switch value := entry.(type) {
	case string:
		return value, value != ""
	case map[string]any:
		id, ok := value["id"].(string)
		return id, ok && id != ""
	default:
		return "", false
	}
}

func indexLayout(root map[string]any) layoutState {
	state := layoutState{placements: map[string][]widgetPlacement{}, valid: true}
	layout, ok := layoutMap(root)
	if !ok {
		return state
	}
	for _, section := range layoutSections {
		entries, ok := layout[section].([]any)
		if !ok {
			continue
		}
		for index, entry := range entries {
			id, ok := widgetID(entry)
			if !ok {
				state.valid = false
				continue
			}
			state.placements[id] = append(state.placements[id], widgetPlacement{ID: id, Section: section, Index: index, Entry: entry})
		}
	}
	for _, placements := range state.placements {
		if len(placements) > 1 {
			state.valid = false
		}
	}
	return state
}

func layoutMap(root map[string]any) (map[string]any, bool) {
	bar, ok := root["bar"].(map[string]any)
	if !ok {
		return nil, false
	}
	layout, ok := bar["layout"].(map[string]any)
	return layout, ok
}

func singlePlacement(state layoutState, id string) *widgetPlacement {
	placements := state.placements[id]
	if len(placements) != 1 {
		return nil
	}
	placement := placements[0]
	return &placement
}

func placementChanged(id string, left, right *widgetPlacement, before, desired layoutState) bool {
	if left == nil || right == nil {
		return left != right
	}
	if left.Section != right.Section {
		return true
	}
	for otherID, placements := range before.placements {
		if otherID == id || len(placements) != 1 || placements[0].Section != left.Section {
			continue
		}
		otherDesired := singlePlacement(desired, otherID)
		if otherDesired == nil || otherDesired.Section != right.Section {
			continue
		}
		if (left.Index < placements[0].Index) != (right.Index < otherDesired.Index) {
			return true
		}
	}
	return false
}

func removeWidget(layout map[string]any, id string, modified map[string]bool) {
	for _, section := range layoutSections {
		entries, ok := layout[section].([]any)
		if !ok {
			continue
		}
		kept := entries[:0]
		for _, entry := range entries {
			entryID, ok := widgetID(entry)
			if ok && entryID == id {
				modified[section] = true
				continue
			}
			kept = append(kept, entry)
		}
		layout[section] = kept
	}
}

func insertWidget(layout map[string]any, desired widgetPlacement, source layoutState, modified map[string]bool) {
	entries, _ := layout[desired.Section].([]any)
	entry := cloneValue(desired.Entry)
	index := sourceAnchorIndex(entries, desired, source)
	entries = append(entries, nil)
	copy(entries[index+1:], entries[index:])
	entries[index] = entry
	layout[desired.Section] = entries
	modified[desired.Section] = true
}

func sourceAnchorIndex(entries []any, desired widgetPlacement, source layoutState) int {
	section := make([]widgetPlacement, 0)
	for _, placements := range source.placements {
		if len(placements) == 1 && placements[0].Section == desired.Section {
			section = append(section, placements[0])
		}
	}
	sort.Slice(section, func(i, j int) bool { return section[i].Index < section[j].Index })
	for i := len(section) - 1; i >= 0; i-- {
		if section[i].Index >= desired.Index {
			continue
		}
		if index := widgetIndex(entries, section[i].ID); index >= 0 {
			return index + 1
		}
	}
	for _, placement := range section {
		if placement.Index <= desired.Index {
			continue
		}
		if index := widgetIndex(entries, placement.ID); index >= 0 {
			return index
		}
	}
	if desired.Index > len(entries) {
		return len(entries)
	}
	return desired.Index
}

func widgetIndex(entries []any, id string) int {
	for index, entry := range entries {
		if entryID, ok := widgetID(entry); ok && entryID == id {
			return index
		}
	}
	return -1
}

func cloneLayout(layout map[string]any) map[string]any {
	copy := map[string]any{}
	for _, section := range layoutSections {
		copy[section] = cloneValue(layout[section])
	}
	return copy
}

func isLayoutPath(path []string, sections map[string]bool) bool {
	return len(path) == 3 && path[0] == "bar" && path[1] == "layout" && sections[path[2]]
}

func filterLayoutChanges(changes []MergeChange, sections map[string]bool) []MergeChange {
	kept := changes[:0]
	for _, change := range changes {
		if !isLayoutPath(change.Path, sections) {
			kept = append(kept, change)
		}
	}
	return kept
}

func filterLayoutConflicts(conflicts []MergeConflict, sections map[string]bool) []MergeConflict {
	kept := conflicts[:0]
	for _, conflict := range conflicts {
		if !isLayoutPath(conflict.Path, sections) {
			kept = append(kept, conflict)
		}
	}
	return kept
}

func preserveAmbiguousSourceLayout(target map[string]any, result MergeResult, options MergeOptions) MergeResult {
	if options.Force {
		return result
	}
	return preserveAmbiguousTargetLayout(target, result)
}

func preserveAmbiguousTargetLayout(target map[string]any, result MergeResult) MergeResult {
	merged, ok := layoutMap(result.Value)
	targetLayout, targetOK := layoutMap(target)
	if !ok || !targetOK {
		return result
	}
	for _, section := range layoutSections {
		merged[section] = cloneValue(targetLayout[section])
	}
	sections := map[string]bool{"left": true, "center": true, "right": true}
	result.Applied = filterLayoutChanges(result.Applied, sections)
	result.Conflicts = filterLayoutConflicts(result.Conflicts, sections)
	result.Conflicts = append(result.Conflicts, MergeConflict{Path: []string{"bar", "layout"}, Source: nil, Target: nil})
	return result
}
