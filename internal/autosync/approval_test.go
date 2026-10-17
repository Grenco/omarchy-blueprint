package autosync

import (
	"math/rand"
	"reflect"
	"testing"
)

func hasExpansion(c Change, kind ExpansionKind, category string) bool {
	for _, e := range c.Expansions {
		if e.Kind == kind && e.Category == category {
			return true
		}
	}
	return false
}

func findExpansion(c Change, kind ExpansionKind) (Expansion, bool) {
	for _, e := range c.Expansions {
		if e.Kind == kind {
			return e, true
		}
	}
	return Expansion{}, false
}

// Finding 3: widening Hooks run-code from one operation type to all of them
// must be named, with the newly covered operation types. Theme activation
// is among them because a theme-set hook needs Hooks run-code.
func TestDescribeNamesRunCodeFamilyExpansion(t *testing.T) {
	prev := activeGrant("/profile", PresetHandsOff, "hooks")
	prev.RunCode = map[string]RunCodeScope{"hooks": {Families: []string{"hooks.delete"}}}
	next := prev
	next.RunCode = map[string]RunCodeScope{"hooks": {}}
	e, ok := findExpansion(Describe(prev, next), ExpandRunCode)
	if !ok || !e.AllFamilies || !e.RunsCode || !reflect.DeepEqual(e.Families, []string{"hooks.write", "themes.activate"}) {
		t.Fatalf("run-code expansion not described exactly: %+v", Describe(prev, next))
	}
	if Narrows(prev, next) {
		t.Fatal("run-code expansion treated as narrowing")
	}
}

// Every kind of expansion has an explicit, exact representation.
func TestEveryExpansionIsExplicit(t *testing.T) {
	base := func() Grant {
		g := activeGrant("/profile", PresetCustom)
		g.Categories = map[string]CategoryScope{
			"themes": {Targets: []string{"theme:a"}, DenyTargets: []string{"theme:x"}, DenyFamilies: []string{"themes.remove"}, AllowFamilies: []string{"themes.copy-local"}, MaxClass: ClassConservative},
			"hooks":  {AllTargets: true, MaxClass: ClassHandsOff},
		}
		g.RunCode = map[string]RunCodeScope{"hooks": {Families: []string{"hooks.write"}}}
		return approved(g)
	}
	type check func(t *testing.T, c Change)
	cases := map[string]struct {
		mutate func(Grant) Grant
		check  check
	}{
		"category": {func(g Grant) Grant {
			g.Categories["defaults"] = CategoryScope{AllTargets: true, MaxClass: ClassBalanced}
			return g
		}, func(t *testing.T, c Change) {
			e, ok := findExpansion(c, ExpandCategory)
			if !ok || e.Category != "defaults" || !e.AllTargets || !e.AllFamilies || e.After != ClassBalanced || len(e.Families) == 0 {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
		"ceiling": {func(g Grant) Grant {
			s := g.Categories["themes"]
			s.MaxClass = ClassBalanced
			g.Categories["themes"] = s
			return g
		}, func(t *testing.T, c Change) {
			e, ok := findExpansion(c, ExpandCeiling)
			if !ok || e.Before != ClassConservative || e.After != ClassBalanced {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
		"one-target": {func(g Grant) Grant {
			s := g.Categories["themes"]
			s.Targets = []string{"theme:a", "theme:b"}
			g.Categories["themes"] = s
			return g
		}, func(t *testing.T, c Change) {
			e, ok := findExpansion(c, ExpandTargets)
			if !ok || e.AllTargets || !reflect.DeepEqual(e.Targets, []string{"theme:b"}) {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
		"all-targets": {func(g Grant) Grant {
			s := g.Categories["themes"]
			s.AllTargets, s.Targets = true, nil
			g.Categories["themes"] = s
			return g
		}, func(t *testing.T, c Change) {
			if e, ok := findExpansion(c, ExpandAllTargets); !ok || !e.AllTargets {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
		"target-exception": {func(g Grant) Grant {
			s := g.Categories["themes"]
			s.DenyTargets = nil
			g.Categories["themes"] = s
			return g
		}, func(t *testing.T, c Change) {
			if e, ok := findExpansion(c, ExpandTargetException); !ok || !reflect.DeepEqual(e.Targets, []string{"theme:x"}) {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
		"family-exception": {func(g Grant) Grant {
			s := g.Categories["themes"]
			s.DenyFamilies = nil
			g.Categories["themes"] = s
			return g
		}, func(t *testing.T, c Change) {
			if e, ok := findExpansion(c, ExpandFamilyException); !ok || !reflect.DeepEqual(e.Families, []string{"themes.remove"}) {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
		"families": {func(g Grant) Grant {
			s := g.Categories["themes"]
			s.AllowFamilies = nil
			g.Categories["themes"] = s
			return g
		}, func(t *testing.T, c Change) {
			e, ok := findExpansion(c, ExpandFamilies)
			if !ok || !e.AllFamilies || contains(e.Families, "themes.copy-local") || !contains(e.Families, "themes.activate") {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
		"run-code-category": {func(g Grant) Grant { g.RunCode["themes"] = RunCodeScope{}; return g }, func(t *testing.T, c Change) {
			e, ok := findExpansion(c, ExpandRunCode)
			if !ok || e.Category != "themes" || !e.RunsCode || !contains(e.Families, "themes.activate") {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
		"privileged": {func(g Grant) Grant { g.PrivilegedInstall = true; return g }, func(t *testing.T, c Change) {
			if e, ok := findExpansion(c, ExpandPrivilegedInstall); !ok || !e.RunsCode {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
		"restore-link": {func(g Grant) Grant { g.RestoreLink = LinkSeparate; return g }, func(t *testing.T, c Change) {
			if _, ok := findExpansion(c, ExpandRestoreLink); !ok {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
		"identity": {func(g Grant) Grant { g.Identity.Machine = "desktop"; return g }, func(t *testing.T, c Change) {
			if _, ok := findExpansion(c, ExpandIdentity); !ok {
				t.Fatalf("%+v", c.Expansions)
			}
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			prev := base()
			next := base()
			next = tc.mutate(next)
			c := Describe(prev, next)
			tc.check(t, c)
			if Narrows(prev, next) {
				t.Fatal("expansion treated as narrowing")
			}
			if !reflect.DeepEqual(c.Before, scopeOf(prev)) || !reflect.DeepEqual(c.After, scopeOf(next)) {
				t.Fatal("before/after scope missing")
			}
		})
	}
	// Removing scope is never described as an expansion.
	prev := base()
	next := base()
	delete(next.Categories, "hooks")
	next.RunCode = nil
	if c := Describe(prev, next); len(c.Expansions) != 0 || !Narrows(prev, next) {
		t.Fatalf("narrowing described as expansion: %+v", c.Expansions)
	}
}

// If a change lets any operation become eligible, the confirmation data
// names at least one expansion: nothing a person approves is hidden.
func TestAnyEffectiveWideningIsDescribed(t *testing.T) {
	rng := rand.New(rand.NewSource(29))
	ops := []OperationInput{themeCopy("a"), themeCopy("b"), defaultSet("browser"), hookWrite("h")}
	random := func() Grant {
		g := activeGrant("/profile", []Preset{PresetConservative, PresetBalanced, PresetHandsOff, PresetCustom}[rng.Intn(4)])
		g.RestoreLink = []RestoreLink{LinkLinked, LinkCopyOnce, LinkSeparate}[rng.Intn(3)]
		g.Categories = map[string]CategoryScope{}
		for _, c := range []string{"themes", "defaults", "hooks"} {
			if rng.Intn(3) == 0 {
				continue
			}
			s := CategoryScope{AllTargets: rng.Intn(2) == 0}
			if !s.AllTargets {
				s.Targets = []string{"theme:a", "theme:b", "default:browser", "hook:h"}[:rng.Intn(5)]
			}
			if rng.Intn(3) == 0 {
				s.DenyTargets = []string{"theme:b"}
			}
			if rng.Intn(4) == 0 {
				s.DenyFamilies = []string{"defaults.set"}
			}
			if g.Preset == PresetCustom {
				s.MaxClass = Class(rng.Intn(4))
			}
			g.Categories[c] = s
		}
		if (g.Preset == PresetHandsOff || g.Preset == PresetCustom) && rng.Intn(2) == 0 {
			g.RunCode = map[string]RunCodeScope{"hooks": {}}
			if rng.Intn(2) == 0 {
				g.RunCode["hooks"] = RunCodeScope{Families: []string{"hooks.delete"}}
			}
		}
		return approved(g)
	}
	ev := evidenceFor{CondInstalledApp: true}
	widenings := 0
	for i := 0; i < 3000; i++ {
		prev, next := random(), random()
		before := decide(t, prev, ev, ops...)
		after := decide(t, next, ev, ops...)
		widened := false
		for n := range ops {
			widened = widened || (after.Decisions[n].Eligible && !before.Decisions[n].Eligible)
		}
		if !widened {
			continue
		}
		widenings++
		if c := Describe(prev, next); len(c.Expansions) == 0 {
			t.Fatalf("iteration %d: widening with empty confirmation data\nprev %+v\nnext %+v", i, prev, next)
		}
	}
	if widenings < 100 {
		t.Fatalf("property exercised only %d widenings", widenings)
	}
}
