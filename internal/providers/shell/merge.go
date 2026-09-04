package shell

import (
	"encoding/json"
	"fmt"
	"sort"
)

type MergeOptions struct {
	Force bool
}

type MergeChange struct {
	Path   []string
	Before any
	After  any
	Forced bool
}

type MergeConflict struct {
	Path   []string
	Source any
	Target any
}

type MergeResult struct {
	Value     map[string]any
	Applied   []MergeChange
	Conflicts []MergeConflict
}

type mergeNode struct {
	Present bool
	Value   any
}

// Merge applies captured Shell intent (sourceBaseline -> sourceDesired) to the
// current target (targetBaseline -> targetCurrent). It recursively merges only
// objects present on all four sides; arrays and every other value are atomic.
func Merge(sourceBaseline, sourceDesired, targetBaseline, targetCurrent map[string]any, options MergeOptions) (MergeResult, error) {
	if sourceBaseline == nil || sourceDesired == nil || targetBaseline == nil || targetCurrent == nil {
		return MergeResult{}, fmt.Errorf("shell merge documents must be objects")
	}
	result := MergeResult{}
	merged := mergeNodeValue(nil,
		mergeNode{Present: true, Value: sourceBaseline},
		mergeNode{Present: true, Value: sourceDesired},
		mergeNode{Present: true, Value: targetBaseline},
		mergeNode{Present: true, Value: targetCurrent},
		options, &result,
	)
	value, ok := merged.Value.(map[string]any)
	if !merged.Present || !ok {
		return MergeResult{}, fmt.Errorf("shell merge result must be an object")
	}
	result.Value = value
	return result, nil
}

func mergeNodeValue(path []string, a, b, c, t mergeNode, options MergeOptions, result *MergeResult) mergeNode {
	if aObject, ok := a.Value.(map[string]any); ok && a.Present {
		if bObject, ok := b.Value.(map[string]any); ok && b.Present {
			if cObject, ok := c.Value.(map[string]any); ok && c.Present {
				if tObject, ok := t.Value.(map[string]any); ok && t.Present {
					return mergeObjects(path, aObject, bObject, cObject, tObject, options, result)
				}
			}
		}
	}
	if nodeEqual(a, b) {
		return cloneNode(t)
	}
	if nodeEqual(b, t) {
		return cloneNode(t)
	}
	if nodeEqual(c, t) {
		result.Applied = append(result.Applied, MergeChange{
			Path: append([]string(nil), path...), Before: cloneValue(t.Value), After: cloneValue(b.Value),
		})
		return cloneNode(b)
	}
	if options.Force {
		result.Applied = append(result.Applied, MergeChange{
			Path: append([]string(nil), path...), Before: cloneValue(t.Value), After: cloneValue(b.Value), Forced: true,
		})
		return cloneNode(b)
	}
	result.Conflicts = append(result.Conflicts, MergeConflict{
		Path: append([]string(nil), path...), Source: cloneValue(b.Value), Target: cloneValue(t.Value),
	})
	return cloneNode(t)
}

func mergeObjects(path []string, a, b, c, t map[string]any, options MergeOptions, result *MergeResult) mergeNode {
	keys := map[string]bool{}
	for _, object := range []map[string]any{a, b, c, t} {
		for key := range object {
			keys[key] = true
		}
	}
	names := make([]string, 0, len(keys))
	for key := range keys {
		names = append(names, key)
	}
	sort.Strings(names)
	merged := make(map[string]any, len(names))
	for _, key := range names {
		value := mergeNodeValue(append(path, key), nodeAt(a, key), nodeAt(b, key), nodeAt(c, key), nodeAt(t, key), options, result)
		if value.Present {
			merged[key] = value.Value
		}
	}
	return mergeNode{Present: true, Value: merged}
}

func nodeAt(object map[string]any, key string) mergeNode {
	value, ok := object[key]
	return mergeNode{Present: ok, Value: value}
}

func nodeEqual(a, b mergeNode) bool {
	if a.Present != b.Present {
		return false
	}
	if !a.Present {
		return true
	}
	left, err := json.Marshal(a.Value)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b.Value)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

func cloneNode(node mergeNode) mergeNode {
	if !node.Present {
		return mergeNode{}
	}
	return mergeNode{Present: true, Value: cloneValue(node.Value)}
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		copy := make(map[string]any, len(value))
		for key, item := range value {
			copy[key] = cloneValue(item)
		}
		return copy
	case []any:
		copy := make([]any, len(value))
		for i, item := range value {
			copy[i] = cloneValue(item)
		}
		return copy
	default:
		return value
	}
}
