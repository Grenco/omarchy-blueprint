package shell

import (
	"reflect"
	"testing"
)

func layoutDocument(left, center, right []any) map[string]any {
	return map[string]any{"version": 1, "bar": map[string]any{"layout": map[string]any{"left": left, "center": center, "right": right}}}
}

func mergeLayout(t *testing.T, a, b, c, target map[string]any, options MergeOptions) MergeResult {
	t.Helper()
	result, err := Merge(a, b, c, target, options)
	if err != nil {
		t.Fatal(err)
	}
	return reconcileBarLayout(a, b, target, result, options)
}

func entries(root map[string]any, section string) []any {
	layout, _ := layoutMap(root)
	items, _ := layout[section].([]any)
	return items
}

func ids(items []any) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		id, ok := widgetID(item)
		if ok {
			result = append(result, id)
		}
	}
	return result
}

func widgetCount(root map[string]any, id string) int {
	count := 0
	for _, section := range layoutSections {
		for _, item := range entries(root, section) {
			if itemID, ok := widgetID(item); ok && itemID == id {
				count++
			}
		}
	}
	return count
}

func TestLayoutReconcilesSourceAddedWidgetAcrossSections(t *testing.T) {
	a := layoutDocument(nil, []any{"workspaces", "clock"}, nil)
	b := layoutDocument(nil, []any{"workspaces", "clock", "acme.weather"}, nil)
	c := layoutDocument(nil, []any{"workspaces", "clock"}, nil)
	target := layoutDocument(nil, []any{"workspaces", "clock"}, []any{"desktop-only", "acme.weather"})
	result := mergeLayout(t, a, b, c, target, MergeOptions{})
	if widgetCount(result.Value, "acme.weather") != 1 || !reflect.DeepEqual(ids(entries(result.Value, "center")), []string{"workspaces", "clock", "acme.weather"}) || !reflect.DeepEqual(ids(entries(result.Value, "right")), []string{"desktop-only"}) || len(result.Conflicts) != 0 {
		t.Fatalf("result=%#v", result)
	}
}

func TestLayoutMovesAndRemovesSourceOwnedWidget(t *testing.T) {
	a := layoutDocument(nil, []any{"clock"}, []any{"acme.weather"})
	b := layoutDocument([]any{map[string]any{"id": "clock", "format": "HH:mm:ss"}}, nil, nil)
	c := layoutDocument(nil, []any{"clock"}, []any{"acme.weather"})
	target := layoutDocument([]any{"acme.weather"}, nil, []any{"clock"})
	result := mergeLayout(t, a, b, c, target, MergeOptions{})
	if widgetCount(result.Value, "clock") != 1 || !reflect.DeepEqual(ids(entries(result.Value, "left")), []string{"clock"}) || widgetCount(result.Value, "acme.weather") != 0 {
		t.Fatalf("result=%#v", result.Value)
	}
	clock := entries(result.Value, "left")[0].(map[string]any)
	if clock["format"] != "HH:mm:ss" {
		t.Fatalf("source settings did not move: %#v", clock)
	}
}

func TestLayoutPreservesTargetOnlyMovementAndAnchorsTargetOnlyWidgets(t *testing.T) {
	a := layoutDocument(nil, []any{"A", "B", "C"}, nil)
	b := layoutDocument(nil, []any{"A", "B", "acme.weather", "C"}, nil)
	c := layoutDocument(nil, []any{"A", "B", "C"}, nil)
	target := layoutDocument(nil, []any{"A", "pc-only", "B", "C"}, nil)
	result := mergeLayout(t, a, b, c, target, MergeOptions{})
	if !reflect.DeepEqual(ids(entries(result.Value, "center")), []string{"A", "pc-only", "B", "acme.weather", "C"}) {
		t.Fatalf("result=%#v", result.Value)
	}
}

func TestLayoutPreservesTargetOnlyMovementForSourceUntouchedWidget(t *testing.T) {
	a := layoutDocument(nil, []any{"clock"}, nil)
	b := layoutDocument(nil, []any{"clock"}, nil)
	c := layoutDocument(nil, []any{"clock"}, nil)
	target := layoutDocument(nil, nil, []any{"clock"})
	result := mergeLayout(t, a, b, c, target, MergeOptions{})
	if !reflect.DeepEqual(ids(entries(result.Value, "right")), []string{"clock"}) || len(result.Applied) != 0 || len(result.Conflicts) != 0 {
		t.Fatalf("result=%#v", result)
	}
}

func TestLayoutAmbiguousTargetPreservesNormalAndForceResolves(t *testing.T) {
	a := layoutDocument(nil, nil, nil)
	b := layoutDocument(nil, []any{"acme.weather"}, nil)
	c := layoutDocument(nil, nil, nil)
	target := layoutDocument(nil, []any{"acme.weather"}, []any{"acme.weather"})
	normal := mergeLayout(t, a, b, c, target, MergeOptions{})
	if widgetCount(normal.Value, "acme.weather") != 2 || len(normal.Conflicts) != 1 || pathName(normal.Conflicts[0].Path) != "bar.layout" {
		t.Fatalf("normal=%#v", normal)
	}
	forced := mergeLayout(t, a, b, c, target, MergeOptions{Force: true})
	if widgetCount(forced.Value, "acme.weather") != 1 || !reflect.DeepEqual(ids(entries(forced.Value, "center")), []string{"acme.weather"}) {
		t.Fatalf("forced=%#v", forced)
	}
}

func TestLayoutAmbiguousSourceRetainsConservativeConflict(t *testing.T) {
	a := layoutDocument(nil, nil, []any{"acme.weather", "acme.weather"})
	b := layoutDocument(nil, []any{"acme.weather", "acme.weather"}, nil)
	c := layoutDocument(nil, nil, []any{"acme.weather", "acme.weather"})
	target := layoutDocument(nil, nil, []any{"acme.weather", "acme.weather"})
	result := mergeLayout(t, a, b, c, target, MergeOptions{})
	if widgetCount(result.Value, "acme.weather") != 2 || len(result.Conflicts) != 1 || pathName(result.Conflicts[0].Path) != "bar.layout" {
		t.Fatalf("result=%#v", result)
	}
}
