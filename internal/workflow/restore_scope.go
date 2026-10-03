package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// RestoreScope selects the categories one Restore run plans. Only names a
// single category; Defer leaves captured categories out of this run so the
// rest can apply while they are blocked (ADR 0028). Both are explicit,
// per-run narrowing (ADR 0023 §8): neither is persisted, and neither grants
// authority to the categories that remain.
type RestoreScope struct {
	Only  string
	Defer []string
}

// AllRestoreCategories is every captured category with nothing deferred.
var AllRestoreCategories = RestoreScope{}

func onlyRestoreScope(only string) RestoreScope { return RestoreScope{Only: only} }

// selectRestoreScope returns the captured providers the scope selects and
// the sorted, de-duplicated categories it defers.
func selectRestoreScope[T ReadProvider](providers []T, data profile.Data, scope RestoreScope) ([]T, []string, error) {
	if scope.Only != "" {
		if len(scope.Defer) > 0 {
			return nil, nil, fmt.Errorf("deferring categories cannot be combined with restoring only %s", scope.Only)
		}
		provider, ok := ProviderByID(providers, scope.Only)
		if !ok {
			return nil, nil, fmt.Errorf("unknown category %s", scope.Only)
		}
		if !provider.Captured(profile.CloneData(data)) {
			return nil, nil, CaptureRequiredError(provider.ID())
		}
		return []T{provider}, nil, nil
	}
	captured := capturedProviders(providers, data)
	if len(scope.Defer) == 0 {
		return captured, nil, nil
	}
	deferred := map[string]bool{}
	for _, id := range scope.Defer {
		id = strings.TrimSpace(id)
		provider, ok := ProviderByID(providers, id)
		if !ok {
			return nil, nil, fmt.Errorf("cannot defer unknown category %q", id)
		}
		if !provider.Captured(profile.CloneData(data)) {
			return nil, nil, fmt.Errorf("cannot defer %s: it has no captured state to restore", id)
		}
		deferred[id] = true
	}
	selected := make([]T, 0, len(captured))
	for _, provider := range captured {
		if !deferred[provider.ID()] {
			selected = append(selected, provider)
		}
	}
	if len(selected) == 0 {
		return nil, nil, fmt.Errorf("every captured category is deferred; nothing is left to restore")
	}
	ids := make([]string, 0, len(deferred))
	for id := range deferred {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return selected, ids, nil
}
