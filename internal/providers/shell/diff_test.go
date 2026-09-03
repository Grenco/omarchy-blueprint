package shell

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func diffState(t *testing.T, raw string) State {
	t.Helper()
	doc, err := ParseDocument([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return State{
		Status:     StatusCustomized,
		Version:    doc.Version,
		Hash:       doc.Hash,
		UserExists: true,
		Current:    doc,
	}
}

func TestDiffDefaultToDefaultNoDrift(t *testing.T) {
	p := Provider{}
	changes, err := p.Diff(profile.Shell{}, State{Status: StatusDefault, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestDiffDefaultToCustomizationIsAdditiveDrift(t *testing.T) {
	p := Provider{}
	changes, err := p.Diff(profile.Shell{Version: 1}, diffState(t, customizedShellJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Type != model.ChangeAdd || !strings.Contains(changes[0].Summary, "customization present") {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestDiffCustomizedToDefaultIsRemoval(t *testing.T) {
	p := Provider{}
	saved := profile.Shell{Version: 1, Hash: "desired", BaselineHash: "base"}
	changes, err := p.Diff(saved, State{Status: StatusDefault, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Type != model.ChangeRemove || !strings.Contains(changes[0].Summary, "removed") {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestDiffIgnoresWhitespaceAndKeyOrder(t *testing.T) {
	p := Provider{}
	// Desired is the compacted customized JSON; current is a reindented but
	// semantically identical document (produced by re-marshaling).
	compact, err := ParseDocument([]byte(customizedShellJSON))
	if err != nil {
		t.Fatal(err)
	}
	saved := profile.Shell{Version: 1, Hash: compact.Hash}
	current := State{Status: StatusCustomized, Version: 1, Hash: compact.Hash, UserExists: true, Current: compact}
	changes, err := p.Diff(saved, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestDiffSummarizesKnownShellFields(t *testing.T) {
	f := newShellFixture(t)
	f.writeUser(defaultShellJSON)
	p := Provider{BaselinePath: f.baseline, UserPath: f.user, ProfileDir: f.profile}
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.Capture(state)
	if err != nil {
		t.Fatal(err)
	}
	// The machine now has the customized document instead.
	f.writeUser(customizedShellJSON)
	after, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	changes, err := p.Diff(saved, after)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, c := range changes {
		names[c.Name] = c.Summary
	}
	for _, name := range []string{"bar.position", "bar.transparent", "idle.lock", "bar.layout.right", "plugins"} {
		if _, ok := names[name]; !ok {
			t.Fatalf("known field %q missing from changes %#v", name, changes)
		}
	}
}

func TestDiffFallsBackForUnknownFieldChange(t *testing.T) {
	p := Provider{}
	a, err := ParseDocument([]byte(`{"version":1,"future":{"keep":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseDocument([]byte(`{"version":1,"future":{"keep":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	changes, err := p.Diff(profile.Shell{Version: 1, Hash: a.Hash}, State{Status: StatusCustomized, Version: 1, Hash: b.Hash, UserExists: true, Current: b})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Name != "shell" || !strings.Contains(changes[0].Summary, "shell configuration differs") {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestVerifyRequiresDesiredCustomizationButIgnoresExtraCustomization(t *testing.T) {
	if result := Verify(profile.Shell{Version: 1}, State{Status: StatusCustomized, Version: 1}); !result.OK {
		t.Fatalf("no desired state must verify OK: %#v", result)
	}
	saved := profile.Shell{Version: 1, Hash: "desired"}
	if result := Verify(saved, State{Status: StatusCustomized, Version: 1, Hash: "desired"}); !result.OK {
		t.Fatalf("matching desired state must verify OK: %#v", result)
	}
	if result := Verify(saved, State{Status: StatusDefault, Version: 1}); result.OK || !reflect.DeepEqual(result.Missing, []string{"shell:config"}) {
		t.Fatalf("missing desired customization must fail: %#v", result)
	}
	if result := Verify(saved, State{Status: StatusCustomized, Version: 2, Hash: "desired"}); result.OK {
		t.Fatal("version mismatch must fail verification")
	}
	if result := Verify(saved, State{Status: StatusCustomized, Version: 1, Hash: "other"}); result.OK {
		t.Fatal("hash mismatch must fail verification")
	}
}
