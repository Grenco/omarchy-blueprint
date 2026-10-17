package autosync

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func TestRegistryMatchesTheAudit(t *testing.T) {
	r := DefaultRegistry()
	if r.Version != CapabilityVersion {
		t.Fatal("registry version")
	}
	byFamily := map[string][]Entry{}
	for _, e := range r.Entries {
		if !knownCategory(e.Category()) {
			t.Fatalf("%s: unknown category", e.Family)
		}
		if e.Delivered {
			t.Fatalf("%s delivered in Phase 11 PR A", e.Family)
		}
		if len(e.Activations) != 1 {
			t.Fatalf("%s: entry must declare exactly its activation, got %v", e.Family, e.Activations)
		}
		if unverifiedFamilies[e.Family] && !hasCondition([]Entry{e}, CondOutcomeVerified) {
			t.Fatalf("%s: unverifiable family without an outcome check", e.Family)
		}
		switch e.Increment {
		case "I1", "I2", "I3", "I4":
		default:
			t.Fatalf("%s: increment %q", e.Family, e.Increment)
		}
		switch e.Class {
		case ClassConservative:
			// Conservative is strictly additive and never runs code.
			if len(e.Changes) == 0 {
				t.Fatalf("%s: Conservative entry must restrict changes", e.Family)
			}
			for _, c := range e.Changes {
				if c != model.EffectAdd {
					t.Fatalf("%s: Conservative entry allows %s", e.Family, c)
				}
			}
			fallthrough
		case ClassBalanced:
			if len(e.RunCode) > 0 || e.Privileged {
				t.Fatalf("%s: %s entry needs code or privilege", e.Family, e.Class)
			}
			for _, p := range e.Payloads {
				if p == model.PayloadExecLater || p == model.PayloadExecNow {
					t.Fatalf("%s: %s entry admits code", e.Family, e.Class)
				}
			}
			if len(e.Payloads) == 0 && e.Family != "plugins.disable" {
				t.Fatalf("%s: %s entry must restrict payloads", e.Family, e.Class)
			}
		case ClassHandsOff:
			if len(e.RunCode) == 0 {
				t.Fatalf("%s: Hands-off entry without run-code authorisation", e.Family)
			}
		default:
			t.Fatalf("%s: class %v", e.Family, e.Class)
		}
		byFamily[e.Family] = append(byFamily[e.Family], e)
	}
	// Specific audit rules.
	for _, family := range []string{"packages.install-official", "packages.install-aur", "packages.install-recipe", "packages.preinstall-add"} {
		for _, e := range byFamily[family] {
			if e.Class != ClassHandsOff || !e.Privileged || e.Increment != "I4" {
				t.Fatalf("%s must be Hands-off privileged install in I4: %+v", family, e)
			}
		}
	}
	for _, e := range byFamily["packages.mise-install"] {
		if e.Privileged {
			t.Fatal("mise needs no privileged authority")
		}
	}
	if !hasCondition(byFamily["packages.install-aur"], CondAURMechanism) || hasCondition(byFamily["packages.install-aur"], CondSignedRepoHelper) {
		t.Fatal("AUR must use its own mechanism, not the signed-repository helper")
	}
	for _, e := range byFamily["themes.activate"] {
		if e.Class == ClassBalanced && !hasCondition([]Entry{e}, CondNoThemeHook) {
			t.Fatal("Balanced theme activation must prove no hook")
		}
		if e.Class == ClassHandsOff && !(contains(e.RunCode, "themes") && contains(e.RunCode, "hooks")) {
			t.Fatal("Hands-off theme activation must need Themes and Hooks run-code")
		}
	}
	for _, e := range byFamily["services.write-unit"] {
		if e.Class == ClassBalanced && !hasCondition([]Entry{e}, CondUnitInertness) {
			t.Fatal("Balanced unit definitions need the unit-inertness proof")
		}
	}
	for family, entries := range byFamily {
		if strings.Contains(family, "remove") || strings.HasSuffix(family, ".delete") {
			for _, e := range entries {
				if !hasCondition([]Entry{e}, CondProvenance) || e.Increment != "I4" {
					t.Fatalf("%s: removals need provenance and belong to I4", family)
				}
			}
		}
	}
	// T3 forms have no entry.
	for _, family := range []string{"config.force-write", "config.force-delete", "packages.preinstalls"} {
		if len(byFamily[family]) > 0 {
			t.Fatalf("%s is never unattended", family)
		}
	}
}

func hasCondition(entries []Entry, c Condition) bool {
	for _, e := range entries {
		for _, have := range e.Conditions {
			if have == c {
				return true
			}
		}
	}
	return false
}

// Every family literal a provider emits is registered, except the forms the
// audit classifies as never unattended (T3).
func TestProviderFamiliesAreRegistered(t *testing.T) {
	never := map[string]bool{"config.force-write": true, "config.force-delete": true, "packages.preinstalls": true}
	families := providerFamilies(t)
	if len(families) < 50 {
		t.Fatalf("found only %d provider families: %v", len(families), families)
	}
	r := DefaultRegistry()
	for _, family := range families {
		if !r.known(family) && !never[family] {
			t.Fatalf("provider family %s has no registry entry or T3 classification", family)
		}
	}
	for _, e := range r.Entries {
		if !contains(families, e.Family) {
			t.Fatalf("registry family %s is never emitted by a provider", e.Family)
		}
	}
}

// providerFamilies collects family strings from provider effect code:
// literal "category.family" values and helper prefixes joined with the
// literal arguments passed to them.
func providerFamilies(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "providers")
	literal := regexp.MustCompile(`^(themes|defaults|config|shell|hooks|plugins|packages|services|resources)\.[a-z-]+$`)
	prefixed := map[string]string{
		"themes": "themes.", "plugins": "plugins.", "config": "config.", "packages": "packages.", "resources": "resources.",
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		pkg := filepath.Base(filepath.Dir(path))
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.KeyValueExpr:
				if key, ok := n.Key.(*ast.Ident); ok && key.Name == "Family" {
					if lit, ok := n.Value.(*ast.BasicLit); ok {
						if s, err := strconv.Unquote(lit.Value); err == nil && literal.MatchString(s) {
							seen[s] = true
						}
					}
				}
			case *ast.CallExpr:
				name := ""
				if id, ok := n.Fun.(*ast.Ident); ok {
					name = id.Name
				}
				switch name {
				case "effect", "writeEffect", "deleteEffect", "packagesEffect", "resourceEffect":
					if prefix, ok := prefixed[pkg]; ok && len(n.Args) > 0 {
						if lit, ok := n.Args[0].(*ast.BasicLit); ok {
							if s, err := strconv.Unquote(lit.Value); err == nil {
								seen[prefix+s] = true
							}
						}
					}
				case "serviceCommand":
					if lit, ok := n.Args[0].(*ast.BasicLit); ok {
						if s, err := strconv.Unquote(lit.Value); err == nil {
							seen["services."+s] = true
						}
					}
				}
			case *ast.AssignStmt:
				// family := "services.write-unit" and tuple assignments.
				for i, lhs := range n.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name == "family" && i < len(n.Rhs) {
						if lit, ok := n.Rhs[i].(*ast.BasicLit); ok {
							if s, err := strconv.Unquote(lit.Value); err == nil && literal.MatchString(s) {
								seen[s] = true
							}
						}
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Families assembled from runtime values in packages/effects.go.
	for _, kind := range []string{"official", "aur", "recipe"} {
		seen["packages.remove-"+kind] = true
	}
	seen["services.enable"], seen["services.disable"] = true, true
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Every Restore operation a provider constructs carries a descriptor: the
// literal sets Effect, is wrapped by withEffect, or its function assigns
// .Effect before returning it. Helpers that build an undescribed literal
// (themes.operation, plugins.op) must only ever be called inside withEffect.
func TestProviderOperationsAreDescribed(t *testing.T) {
	root := filepath.Join("..", "providers")
	checked := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		helpers := map[string]bool{}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			assignsEffect := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if as, ok := n.(*ast.AssignStmt); ok {
					for _, lhs := range as.Lhs {
						if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Effect" {
							assignsEffect = true
						}
					}
				}
				return true
			})
			wrapped := map[*ast.CompositeLit]bool{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "withEffect" && len(call.Args) > 0 {
						if lit, ok := call.Args[0].(*ast.CompositeLit); ok {
							wrapped[lit] = true
						}
					}
				}
				return true
			})
			elements := map[*ast.CompositeLit]bool{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if outer, ok := n.(*ast.CompositeLit); ok {
					if array, ok := outer.Type.(*ast.ArrayType); ok && isOperationType(array.Elt) {
						for _, elt := range outer.Elts {
							if inner, ok := elt.(*ast.CompositeLit); ok && inner.Type == nil {
								elements[inner] = true
							}
						}
					}
				}
				return true
			})
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || len(lit.Elts) == 0 || !(isOperationType(lit.Type) || elements[lit]) {
					return true
				}
				checked++
				for _, elt := range lit.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Effect" {
							return true
						}
					}
				}
				if wrapped[lit] || assignsEffect {
					return true
				}
				if returnsLiteral(fn, lit) {
					helpers[fn.Name.Name] = true
					return true
				}
				t.Errorf("%s: %s builds a model.Operation without an effect descriptor", path, fn.Name.Name)
				return true
			})
		}
		// Every call to an undescribed helper must be wrapped.
		pkgDir := filepath.Dir(path)
		for helper := range helpers {
			checkHelperCalls(t, pkgDir, helper)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 40 {
		t.Fatalf("checked only %d operation literals", checked)
	}
}

func isOperationType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "model" && sel.Sel.Name == "Operation"
}

func returnsLiteral(fn *ast.FuncDecl, lit *ast.CompositeLit) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if ret, ok := n.(*ast.ReturnStmt); ok {
			for _, r := range ret.Results {
				if r == lit {
					found = true
				}
			}
		}
		return true
	})
	return found
}

func checkHelperCalls(t *testing.T, dir, helper string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		wrapped := map[*ast.CallExpr]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "withEffect" && len(call.Args) > 0 {
					if inner, ok := call.Args[0].(*ast.CallExpr); ok {
						wrapped[inner] = true
					}
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == helper && !wrapped[call] {
				t.Errorf("%s: call to %s is not wrapped by withEffect", path, helper)
			}
			return true
		})
	}
}
