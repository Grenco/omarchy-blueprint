package shell

import (
	"reflect"
	"testing"
)

func TestMergeAtomicDecisionTable(t *testing.T) {
	cases := []struct {
		name                  string
		a, b, c, target, want any
		force                 bool
		applied, conflicts    int
		forced                bool
	}{
		{"unchanged", 1, 1, 1, 1, 1, false, 0, 0, false},
		{"target-only", 1, 1, 1, 2, 2, false, 0, 0, false},
		{"source-only", 1, 2, 1, 1, 2, false, 1, 0, false},
		{"satisfied", 1, 2, 1, 2, 2, false, 0, 0, false},
		{"conflict", 1, 2, 1, 3, 3, false, 0, 1, false},
		{"forced conflict", 1, 2, 1, 3, 2, true, 1, 0, true},
		{"baseline evolution", 1, 2, 9, 9, 2, false, 1, 0, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result, err := Merge(map[string]any{"value": test.a}, map[string]any{"value": test.b}, map[string]any{"value": test.c}, map[string]any{"value": test.target}, MergeOptions{Force: test.force})
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Value["value"]; !reflect.DeepEqual(got, test.want) || len(result.Applied) != test.applied || len(result.Conflicts) != test.conflicts {
				t.Fatalf("result = %#v", result)
			}
			if test.applied == 1 && result.Applied[0].Forced != test.forced {
				t.Fatalf("forced = %t", result.Applied[0].Forced)
			}
		})
	}
}

func TestMergeIndependentObjectChildrenAndAtomicArrays(t *testing.T) {
	a := map[string]any{"idle": map[string]any{"lock": 300, "screensaver": 150}, "layout": []any{"default"}}
	b := map[string]any{"idle": map[string]any{"lock": 600, "screensaver": 150}, "layout": []any{"source"}}
	c := map[string]any{"idle": map[string]any{"lock": 300, "screensaver": 150}, "layout": []any{"default"}}
	target := map[string]any{"idle": map[string]any{"lock": 300, "screensaver": 200}, "layout": []any{"target"}}
	result, err := Merge(a, b, c, target, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	idle := result.Value["idle"].(map[string]any)
	if idle["lock"] != 600 || idle["screensaver"] != 200 {
		t.Fatalf("idle = %#v", idle)
	}
	if !reflect.DeepEqual(result.Value["layout"], []any{"target"}) || len(result.Conflicts) != 1 || pathName(result.Conflicts[0].Path) != "layout" {
		t.Fatalf("result = %#v", result)
	}
}

func TestMergeAbsentNullAndInputImmutability(t *testing.T) {
	a := map[string]any{"removed": 1}
	b := map[string]any{"null": nil}
	c := map[string]any{"removed": 1}
	target := map[string]any{"removed": 1}
	original := cloneValue(target)
	result, err := Merge(a, b, c, target, MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Value["removed"]; ok {
		t.Fatalf("source deletion was not applied: %#v", result.Value)
	}
	if value, ok := result.Value["null"]; !ok || value != nil {
		t.Fatalf("null key not preserved: %#v", result.Value)
	}
	if !reflect.DeepEqual(target, original) {
		t.Fatalf("target mutated: %#v", target)
	}
}
