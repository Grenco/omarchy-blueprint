package packages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type Provider struct {
	Runner           command.Runner
	MiseGlobalConfig string
	// Stat checks sync database presence; nil uses os.Stat.
	Stat func(string) (fs.FileInfo, error)
}

func (p Provider) Detect(ctx context.Context) (profile.Packages, error) {
	official, err := p.query(ctx, "-Qqen")
	if err != nil {
		return profile.Packages{}, fmt.Errorf("detect explicitly installed native packages: %w", err)
	}
	aur, err := p.query(ctx, "-Qqem")
	if err != nil {
		return profile.Packages{}, fmt.Errorf("detect explicitly installed foreign packages: %w", err)
	}
	installed, err := p.query(ctx, "-Qq")
	if err != nil {
		return profile.Packages{}, fmt.Errorf("detect installed packages: %w", err)
	}
	packages := profile.Packages{Official: official, AUR: aur, Installed: installed}
	preinstalls, err := omarchy.DetectPreinstalls(ctx, p.Runner)
	if err != nil {
		return profile.Packages{}, fmt.Errorf("detect Omarchy preinstalls: %w", err)
	}
	packages.Preinstalls = profile.Preinstalls{Managed: true, RemovedAll: preinstalls.RemovedAll, Items: preinstalls.Items}
	packages = CanonicalizePreinstallOwnership(packages, preinstalls.Items)
	packages.SemanticInstalled, packages.SemanticRemoved = p.detectSemanticState(ctx)
	if p.MiseGlobalConfig != "" {
		mise, err := ReadMiseTools(p.MiseGlobalConfig)
		if err != nil {
			return profile.Packages{}, fmt.Errorf("detect global mise packages: %w", err)
		}
		if err := ValidateMiseSecrets(mise); err != nil {
			return profile.Packages{}, err
		}
		packages.Mise = mise
		packages.MiseInstalled = p.detectMiseInstalled(ctx)
	}
	return classify(packages), nil
}

func (p Provider) detectSemanticState(ctx context.Context) (map[string]bool, map[string]bool) {
	installed, removed := map[string]bool{}, map[string]bool{}
	for _, id := range []string{"tailscale"} {
		recipe, ok := omarchy.SemanticRecipe(id)
		if !ok {
			continue
		}
		installed[id] = p.recipeChecksPass(ctx, recipe.Verify)
		removed[id] = p.recipeChecksPass(ctx, recipe.RemoveVerify)
	}
	return installed, removed
}

func (p Provider) recipeChecksPass(ctx context.Context, checks [][]string) bool {
	if p.Runner == nil || len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		if len(check) == 0 {
			continue
		}
		if _, err := p.Runner.Run(ctx, check[0], check[1:]...); err != nil {
			return false
		}
	}
	return true
}

func (p Provider) detectMiseInstalled(ctx context.Context) map[string]bool {
	out, err := p.Runner.Run(ctx, "mise", "ls", "--json")
	if err != nil {
		return nil
	}
	var entries map[string][]struct {
		Installed bool `json:"installed"`
	}
	if json.Unmarshal([]byte(out), &entries) != nil {
		return nil
	}
	installed := make(map[string]bool, len(entries))
	for id, versions := range entries {
		for _, version := range versions {
			installed[id] = installed[id] || version.Installed
		}
	}
	return installed
}

func (p Provider) query(ctx context.Context, arg string) ([]string, error) {
	out, err := p.Runner.Run(ctx, "pacman", arg)
	if err != nil {
		var runErr *command.RunError
		if errors.As(err, &runErr) && runErr.ExitCode == 1 && strings.TrimSpace(out) == "" {
			return []string{}, nil
		}
		return nil, err
	}
	return lines(out), nil
}

func (p Provider) Check(ctx context.Context, saved profile.Packages) error {
	if err := ValidateMiseSecrets(saved.Mise); err != nil {
		return err
	}
	if _, err := p.Detect(ctx); err != nil {
		return err
	}
	if len(saved.Mise) > 0 {
		if _, err := p.Runner.Run(ctx, "mise", "--version"); err != nil {
			return fmt.Errorf("mise is required to restore mise packages: %w", err)
		}
	}
	return nil
}

func Diff(saved, current profile.Packages) []model.Change {
	if saved.Preinstalls.Managed {
		saved = CanonicalizePreinstallOwnership(saved, current.Preinstalls.Items)
		current = CanonicalizePreinstallOwnership(current, current.Preinstalls.Items)
	}
	saved, current = classify(saved), classify(current)
	savedNames := packageNames(saved)
	currentNames := packageNames(current)
	var out []model.Change
	out = append(out, diffKind("official", saved.Official, current.Official, savedNames, currentNames)...)
	out = append(out, diffKind("aur", saved.AUR, current.AUR, savedNames, currentNames)...)
	out = append(out, diffMise(saved.Mise, current.Mise)...)
	out = append(out, diffPreinstalls(saved.Preinstalls, current.Preinstalls)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Name < out[j].Name
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func Plan(saved, current profile.Packages, schema int, from, to string) model.RestorePlan {
	plan, _ := (Provider{}).Plan(saved, current, schema, from, to)
	return plan
}

type PlanOptions struct{ Exact bool }

func (p Provider) Plan(saved, current profile.Packages, schema int, from, to string, options ...PlanOptions) (model.RestorePlan, error) {
	var opts PlanOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if err := ValidateMiseSecrets(saved.Mise); err != nil {
		return model.RestorePlan{}, err
	}
	saved, physicalCurrent := classify(saved), classify(current)
	if schema >= 13 {
		saved = CanonicalizePreinstallOwnership(saved, physicalCurrent.Preinstalls.Items)
		physicalCurrent = CanonicalizePreinstallOwnership(physicalCurrent, physicalCurrent.Preinstalls.Items)
	}
	current = physicalCurrent
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	preinstallOps, preinstallSkipped := planPreinstalls(saved.Preinstalls, current.Preinstalls)
	plan.Operations = append(plan.Operations, preinstallOps...)
	plan.Skipped = append(plan.Skipped, preinstallSkipped...)
	currentNames := packageNames(current)
	var missingOfficial, missingAUR []string
	var semanticInstalls []model.Operation
	for _, name := range saved.Official {
		if recipe, ok := omarchy.SemanticRecipe(name); ok {
			semanticSatisfied := current.SemanticInstalled == nil || current.SemanticInstalled[name]
			if !currentNames[name] || !semanticSatisfied {
				semanticInstalls = append(semanticInstalls, semanticInstallOperation(recipe))
			}
		} else if !currentNames[name] {
			missingOfficial = append(missingOfficial, name)
		}
	}
	for _, name := range saved.AUR {
		if !currentNames[name] {
			missingAUR = append(missingAUR, name)
		}
	}
	if len(missingOfficial) > 0 {
		plan.Operations = append(plan.Operations, operation("official", missingOfficial, append([]string{"omarchy", "pkg", "add"}, missingOfficial...)))
	}
	plan.Operations = append(plan.Operations, semanticInstalls...)
	if len(missingAUR) > 0 {
		for _, name := range missingAUR {
			plan.Operations = append(plan.Operations, operation("aur", []string{name}, []string{"omarchy", "pkg", "aur", "add", name}))
		}
	}
	for _, name := range saved.MachineSpecific {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: name, Reason: "machine-specific hardware package"})
	}
	savedNames := packageNames(saved)
	absent := absenceRefSet(saved.Absent)
	for _, name := range current.Official {
		if !savedNames[name] && !(opts.Exact && absent["official:"+name]) {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: "official:" + name, Reason: "additional package left installed; removal disabled"})
		}
	}
	for _, name := range current.AUR {
		if !savedNames[name] && !(opts.Exact && absent["aur:"+name]) {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: "aur:" + name, Reason: "additional package left installed; removal disabled"})
		}
	}
	if opts.Exact {
		plan = planExactArchRemovals(plan, saved, physicalCurrent)
	}
	if p.MiseGlobalConfig == "" {
		if opts.Exact {
			removals, skipped := actionableMiseRemovals(saved.Absent, physicalCurrent.Mise)
			plan.Skipped = append(plan.Skipped, skipped...)
			plan = skipMiseIDs(plan, removals, "Mise global config is unavailable; removal skipped")
		}
		return plan, nil
	}
	additions, conflicts, extras := classifyMiseRestore(saved.Mise, current.Mise)
	installOnly := profile.MiseTools{}
	if current.MiseInstalled != nil {
		for id, tool := range saved.Mise {
			if !current.MiseInstalled[id] {
				if _, declarationAdded := additions[id]; !declarationAdded {
					installOnly[id] = tool
				}
			}
		}
	}
	for _, id := range conflicts {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: "mise:" + id, Reason: "existing Mise declaration differs; overwrite disabled"})
	}
	for _, id := range extras {
		if opts.Exact && absent["mise:"+id] {
			continue
		}
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: "mise:" + id, Reason: "additional package left installed; removal disabled"})
	}
	var removals []string
	if opts.Exact {
		var skipped []model.Skipped
		removals, skipped = actionableMiseRemovals(saved.Absent, physicalCurrent.Mise)
		plan.Skipped = append(plan.Skipped, skipped...)
	}
	if err := ValidateMiseMutationPath(p.MiseGlobalConfig); err != nil {
		plan = skipMiseAdditions(plan, additions, err.Error())
		plan = skipMiseIDs(plan, removals, err.Error())
		return plan, nil
	}
	snapshot, err := ReadMiseConfigSnapshot(p.MiseGlobalConfig)
	if err != nil {
		plan = skipMiseAdditions(plan, additions, err.Error())
		plan = skipMiseIDs(plan, removals, err.Error())
		return plan, nil
	}
	if len(additions) == 0 && len(removals) == 0 && len(installOnly) == 0 {
		return plan, nil
	}
	if len(additions) == 0 && len(removals) == 0 {
		ids := sortedMiseIDs(installOnly)
		plan.Operations = append(plan.Operations, model.Operation{ID: "packages.mise.install", Provider: "packages", Action: "install", Resource: "mise:" + strings.Join(ids, ","), Items: ids, Command: append([]string{"mise", "-C", "/", "install"}, ids...), Risk: miseInstallRisk(installOnly)})
		return plan, nil
	}
	candidate, candidateErr := buildMiseMutationCandidate(snapshot, physicalCurrent.Mise, additions, removals)
	if candidateErr != nil {
		plan = skipMiseAdditions(plan, additions, candidateErr.Error())
		plan = skipMiseIDs(plan, removals, candidateErr.Error())
		return plan, nil
	}
	var removalIDs []string
	var removalOps []model.Operation
	guardID := ""
	if len(removals) > 0 {
		guardID = "packages.mise.guard"
		// Keep the declaration in place until every uninstall succeeds, but
		// prove it still has the approved content immediately before the
		// first destructive command. Positional parameters avoid embedding an
		// untrusted path or hash into shell source.
		plan.Operations = append(plan.Operations, model.Operation{ID: guardID, Provider: "packages", Action: "verify", Resource: "mise:global-tools", Command: []string{"sh", "-c", `test "$(sha256sum -- "$1" | cut -d ' ' -f 1)" = "$2"`, "sh", p.MiseGlobalConfig, snapshot.Hash}, Risk: model.RiskLow})
	}
	for _, id := range removals {
		opID := "packages.mise.remove." + id
		removalOps = append(removalOps, model.Operation{ID: opID, Provider: "packages", Action: "remove", Resource: "mise:" + id, Items: []string{id}, Command: []string{"mise", "-C", "/", "uninstall", "--all", id}, DependsOn: []string{guardID}, Risk: model.RiskHigh})
		removalIDs = append(removalIDs, opID)
	}
	mutationIDs := append(append([]string(nil), removals...), sortedMiseIDs(additions)...)
	write := model.FileWrite{Generated: true, Content: candidate, Destination: p.MiseGlobalConfig, SourceHash: hashBytes(candidate), Backup: snapshot.Exists, RejectSymlinkParents: true}
	if snapshot.Exists {
		write.ExpectedHash = snapshot.Hash
	} else {
		write.ExpectedMissing = true
	}
	risk := model.RiskMedium
	if len(removals) > 0 {
		risk = model.RiskHigh
	}
	// Remove tools before committing the declaration change so a failed
	// uninstall leaves it actionable for a later retry. The preceding guard
	// blocks stale authority before any destructive command can start.
	plan.Operations = append(plan.Operations, removalOps...)
	plan.Operations = append(plan.Operations, model.Operation{ID: "packages.mise.configure", Provider: "packages", Action: "configure", Resource: "mise:global-tools", Items: mutationIDs, File: &write, DependsOn: removalIDs, Risk: risk, Reversible: snapshot.Exists})
	if len(additions) > 0 {
		for id, tool := range installOnly {
			additions[id] = tool
		}
		ids := sortedMiseIDs(additions)
		plan.Operations = append(plan.Operations, model.Operation{ID: "packages.mise.install", Provider: "packages", Action: "install", Resource: "mise:" + strings.Join(ids, ","), Items: ids, Command: append([]string{"mise", "-C", "/", "install"}, ids...), DependsOn: []string{"packages.mise.configure"}, Risk: miseInstallRisk(additions)})
	} else if len(installOnly) > 0 {
		ids := sortedMiseIDs(installOnly)
		plan.Operations = append(plan.Operations, model.Operation{ID: "packages.mise.install", Provider: "packages", Action: "install", Resource: "mise:" + strings.Join(ids, ","), Items: ids, Command: append([]string{"mise", "-C", "/", "install"}, ids...), DependsOn: []string{"packages.mise.configure"}, Risk: miseInstallRisk(installOnly)})
	}
	return plan, nil
}

type VerifyOptions struct {
	Exact  bool
	Schema int
}

func Verify(saved, current profile.Packages, options ...VerifyOptions) model.VerificationResult {
	var opts VerifyOptions
	if len(options) > 0 {
		opts = options[0]
	}
	saved, current = classify(saved), classify(current)
	if opts.Schema >= 13 || saved.Preinstalls.Managed {
		saved = CanonicalizePreinstallOwnership(saved, current.Preinstalls.Items)
		current = CanonicalizePreinstallOwnership(current, current.Preinstalls.Items)
	}
	var missing []string
	currentNames := packageNames(current)
	for _, name := range saved.Official {
		if !currentNames[name] {
			missing = append(missing, "official:"+name)
		}
	}
	for _, name := range saved.AUR {
		if !currentNames[name] {
			missing = append(missing, "aur:"+name)
		}
	}
	for _, id := range sortedMiseIDs(saved.Mise) {
		actual, ok := current.Mise[id]
		if !ok || !EqualMiseTool(saved.Mise[id], actual) || (current.MiseInstalled != nil && !current.MiseInstalled[id]) {
			missing = append(missing, "mise:"+id)
		}
	}
	if saved.Preinstalls.Managed && saved.Preinstalls.RemovedAll != current.Preinstalls.RemovedAll {
		missing = append(missing, "preinstalls")
	}
	for _, id := range sortedBoolKeys(saved.Preinstalls.Items) {
		if current.Preinstalls.Items[id] != saved.Preinstalls.Items[id] {
			missing = append(missing, "preinstall:"+id)
		}
	}
	if opts.Exact {
		for _, absence := range saved.Absent {
			kind, id, ok := splitRef(absence.Ref)
			if !ok || machineSpecific(id) {
				continue
			}
			switch kind {
			case "official":
				if set(current.Official)[id] {
					missing = append(missing, absence.Ref)
				}
			case "aur":
				if set(current.AUR)[id] {
					missing = append(missing, absence.Ref)
				}
			case "mise":
				if actual, present := current.Mise[id]; present && EqualMiseTool(actual, absence.Mise) {
					missing = append(missing, absence.Ref)
				}
			}
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}

// Verify checks package state and the non-secret postconditions owned by
// semantic Omarchy recipes. Package presence alone is not proof that a
// service installer completed its integration work.
func (p Provider) Verify(ctx context.Context, saved, current profile.Packages, options ...VerifyOptions) model.VerificationResult {
	result := Verify(saved, current, options...)
	currentNames := packageNames(current)
	missing := set(result.Missing)
	for _, id := range saved.Official {
		recipe, semantic := omarchy.SemanticRecipe(id)
		if !semantic || !currentNames[id] {
			continue
		}
		for _, check := range recipe.Verify {
			if len(check) == 0 {
				continue
			}
			if _, err := p.Runner.Run(ctx, check[0], check[1:]...); err != nil {
				resource := "official:" + id
				if !missing[resource] {
					result.Missing = append(result.Missing, resource)
					missing[resource] = true
				}
				break
			}
		}
	}
	if len(options) > 0 && options[0].Exact {
		for _, absence := range saved.Absent {
			kind, id, ok := splitRef(absence.Ref)
			if !ok || kind != "official" || currentNames[id] {
				continue
			}
			recipe, semantic := omarchy.SemanticRecipe(id)
			if !semantic {
				continue
			}
			for _, check := range recipe.RemoveVerify {
				if _, err := p.Runner.Run(ctx, check[0], check[1:]...); err != nil {
					if !missing[absence.Ref] {
						result.Missing = append(result.Missing, absence.Ref)
						missing[absence.Ref] = true
					}
					break
				}
			}
		}
	}
	sort.Strings(result.Missing)
	result.OK = len(result.Missing) == 0
	return result
}

func diffMise(saved, current profile.MiseTools) []model.Change {
	var changes []model.Change
	for _, id := range sortedMiseIDs(current) {
		desired, ok := saved[id]
		if !ok {
			changes = append(changes, model.Change{Type: model.ChangeAdd, Provider: "packages", Kind: "mise", Name: id, Summary: "+ mise package " + id + " (" + SummarizeMiseTool(current[id]) + ")"})
		} else if !EqualMiseTool(desired, current[id]) {
			changes = append(changes, model.Change{Type: model.ChangeModify, Provider: "packages", Kind: "mise", Name: id, Summary: "~ mise package " + id + ": " + SummarizeMiseTool(desired) + " -> " + SummarizeMiseTool(current[id])})
		}
	}
	for _, id := range sortedMiseIDs(saved) {
		if _, ok := current[id]; !ok {
			changes = append(changes, model.Change{Type: model.ChangeRemove, Provider: "packages", Kind: "mise", Name: id, Summary: "- mise package " + id + " (" + SummarizeMiseTool(saved[id]) + ")"})
		}
	}
	return changes
}

func diffPreinstalls(saved, current profile.Preinstalls) []model.Change {
	if !saved.Managed && len(saved.Items) == 0 {
		return nil
	}
	var changes []model.Change
	if saved.Managed && saved.RemovedAll != current.RemovedAll {
		changeType, summary := model.ChangeAdd, "+ Omarchy preinstalls enabled"
		if current.RemovedAll {
			changeType, summary = model.ChangeRemove, "- Omarchy preinstalls removed"
		}
		changes = append(changes, model.Change{Type: changeType, Provider: "packages", Kind: "preinstalls", Name: "preinstalls", Summary: summary})
	}
	for _, id := range sortedBoolKeys(saved.Items) {
		actual, known := current.Items[id]
		if known && actual == saved.Items[id] {
			continue
		}
		changeType, prefix := model.ChangeAdd, "+ "
		if !saved.Items[id] {
			changeType, prefix = model.ChangeRemove, "- "
		}
		changes = append(changes, model.Change{Type: changeType, Provider: "packages", Kind: "preinstall", Name: id, Summary: prefix + "Omarchy preinstall " + id})
	}
	return changes
}

func planPreinstalls(saved, current profile.Preinstalls) ([]model.Operation, []model.Skipped) {
	if !saved.Managed && len(saved.Items) == 0 {
		return nil, nil
	}
	var operations []model.Operation
	var groupDependency []string
	simulated := make(map[string]bool, len(current.Items))
	for id, present := range current.Items {
		simulated[id] = present
	}
	if saved.Managed && saved.RemovedAll != current.RemovedAll {
		if saved.RemovedAll {
			// Omarchy's native remove-all also sweeps every Omarchy-style webapp
			// and TUI launcher, outside Blueprint's tracked preinstall scope.
			// Do not automate that broader destructive action.
			return nil, []model.Skipped{{Provider: "packages", Resource: "preinstalls", Reason: "Omarchy remove-all can delete unmanaged webapps/TUIs; run it manually after review"}}
		}
		action, commandName, id := "install", "omarchy-install-preinstalls", "packages.preinstalls.install"
		markerCheck := `test ! -f "$HOME/.local/state/omarchy/preinstalls-removed"`
		operations = append(operations, model.Operation{
			ID:       id,
			Provider: "packages",
			Action:   action,
			Resource: "preinstalls",
			// Omarchy's gum confirmation returns success even when declined.
			// Check the authoritative marker in the same operation so dependent
			// child changes cannot run after a cancelled group transition.
			Command:     []string{"sh", "-c", commandName + " && " + markerCheck},
			Risk:        model.RiskHigh,
			Interactive: true,
			Notice:      "Omarchy's preinstall flow requests terminal confirmation before changing the managed application set.",
		})
		groupDependency = []string{id}
		for item := range simulated {
			simulated[item] = !saved.RemovedAll
		}
	}
	var skipped []model.Skipped
	for _, item := range sortedBoolKeys(saved.Items) {
		want := saved.Items[item]
		actual, supported := simulated[item]
		if !supported {
			skipped = append(skipped, model.Skipped{Provider: "packages", Resource: "preinstall:" + item, Reason: "not present in the installed Omarchy preinstall catalogue"})
			continue
		}
		if actual == want {
			continue
		}
		action, commandName, risk := "install", "omarchy-pkg-add", model.RiskLow
		if !want {
			action, commandName, risk = "remove", "omarchy-pkg-drop", model.RiskHigh
		}
		operations = append(operations, model.Operation{
			ID:         "packages.preinstall." + action + "." + item,
			Provider:   "packages",
			Action:     action,
			Resource:   "preinstall:" + item,
			Items:      []string{item},
			Command:    []string{commandName, item},
			DependsOn:  append([]string(nil), groupDependency...),
			Risk:       risk,
			Reversible: false,
		})
	}
	return operations, skipped
}

func sortedBoolKeys(items map[string]bool) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func classifyMiseRestore(saved, current profile.MiseTools) (profile.MiseTools, []string, []string) {
	additions := profile.MiseTools{}
	var conflicts, extras []string
	for _, id := range sortedMiseIDs(saved) {
		actual, ok := current[id]
		if !ok {
			additions[id] = saved[id]
		} else if !EqualMiseTool(saved[id], actual) {
			conflicts = append(conflicts, id)
		}
	}
	for _, id := range sortedMiseIDs(current) {
		if _, ok := saved[id]; !ok {
			extras = append(extras, id)
		}
	}
	return additions, conflicts, extras
}

func skipMiseAdditions(plan model.RestorePlan, additions profile.MiseTools, reason string) model.RestorePlan {
	for _, id := range sortedMiseIDs(additions) {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: "mise:" + id, Reason: reason})
	}
	return plan
}

func absenceRefSet(absences []profile.PackageAbsence) map[string]bool {
	refs := make(map[string]bool, len(absences))
	for _, absence := range absences {
		refs[absence.Ref] = true
	}
	return refs
}

func planExactArchRemovals(plan model.RestorePlan, saved, current profile.Packages) model.RestorePlan {
	official, aur := set(current.Official), set(current.AUR)
	for _, absence := range saved.Absent {
		kind, id, ok := splitRef(absence.Ref)
		if !ok || id == "" {
			continue
		}
		if machineSpecific(id) {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: absence.Ref, Reason: "hardware/machine-specific package is protected from removal"})
			continue
		}
		switch kind {
		case "official", "aur":
			present := official[id]
			if kind == "aur" {
				present = aur[id]
			}
			_, semantic := omarchy.SemanticRecipe(id)
			semanticResidue := semantic && current.SemanticRemoved != nil && !current.SemanticRemoved[id]
			if !present && !semanticResidue {
				continue
			}
			commandLine := []string{"omarchy", "pkg", "drop", id}
			operationID := "packages.remove." + kind + "." + id
			interactive, notice := false, ""
			if kind == "official" {
				if recipe, found := omarchy.SemanticRecipe(id); found {
					commandLine = append([]string(nil), recipe.Remove...)
					operationID = "packages.remove.semantic." + id
					interactive, notice = recipe.Interactive, recipe.RemoveNotice
				}
			}
			plan.Operations = append(plan.Operations, model.Operation{ID: operationID, Provider: "packages", Action: "remove", Resource: absence.Ref, Items: []string{id}, Command: commandLine, Risk: model.RiskHigh, Interactive: interactive, Notice: notice})
		}
	}
	return plan
}

func actionableMiseRemovals(absences []profile.PackageAbsence, current profile.MiseTools) ([]string, []model.Skipped) {
	var ids []string
	var skipped []model.Skipped
	for _, absence := range absences {
		kind, id, ok := splitRef(absence.Ref)
		if !ok || kind != "mise" {
			continue
		}
		actual, present := current[id]
		if !present {
			continue
		}
		if absence.Mise == nil || !EqualMiseTool(actual, absence.Mise) {
			skipped = append(skipped, model.Skipped{Provider: "packages", Resource: absence.Ref, Reason: "current Mise declaration no longer matches the removed profile entry; removal skipped"})
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, skipped
}

func buildMiseMutationCandidate(snapshot MiseConfigSnapshot, current, additions profile.MiseTools, removals []string) ([]byte, error) {
	if !snapshot.Exists {
		if len(removals) > 0 {
			return nil, errors.New("Mise global config is unavailable; removal skipped")
		}
		return EncodeMiseTools(additions)
	}
	candidate := append([]byte(nil), snapshot.Bytes...)
	remaining := make(profile.MiseTools, len(current))
	for id, tool := range current {
		remaining[id] = tool
	}
	if len(removals) > 0 {
		var err error
		candidate, err = BuildMiseRemovalCandidate(candidate, current, removals)
		if err != nil {
			return nil, err
		}
		for _, id := range removals {
			delete(remaining, id)
		}
	}
	if len(additions) > 0 {
		return BuildMiseAppendCandidate(candidate, remaining, additions)
	}
	return candidate, nil
}

func skipMiseIDs(plan model.RestorePlan, ids []string, reason string) model.RestorePlan {
	for _, id := range ids {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: "mise:" + id, Reason: reason})
	}
	return plan
}

func miseInstallRisk(tools profile.MiseTools) model.Risk {
	if MiseToolsHavePostinstall(tools) {
		return model.RiskHigh
	}
	return model.RiskLow
}

func diffKind(kind string, saved, current []string, savedNames, currentNames map[string]bool) []model.Change {
	var out []model.Change
	for _, name := range current {
		if !savedNames[name] {
			out = append(out, model.Change{Type: model.ChangeAdd, Provider: "packages", Kind: kind, Name: name, Summary: "+ " + kind + " package " + name})
		}
	}
	for _, name := range saved {
		if !currentNames[name] {
			out = append(out, model.Change{Type: model.ChangeRemove, Provider: "packages", Kind: kind, Name: name, Summary: "- " + kind + " package " + name})
		}
	}
	return out
}

func packageNames(packages profile.Packages) map[string]bool {
	names := set(packages.Official)
	for _, name := range packages.AUR {
		names[name] = true
	}
	for _, name := range packages.Installed {
		names[name] = true
	}
	return names
}

// CanonicalizePreinstallOwnership gives every package in Omarchy's installed
// preinstall catalogue one policy identity: preinstall:<id>. Installed is
// deliberately retained as physical-presence scratch state, while portable
// generic identities and tombstones are migrated to child intent so legacy
// profiles keep their meaning without retaining a second authority path.
func CanonicalizePreinstallOwnership(packages profile.Packages, catalogue map[string]bool) profile.Packages {
	if len(catalogue) == 0 {
		return packages
	}
	items := make(map[string]bool, len(packages.Preinstalls.Items))
	for id, present := range packages.Preinstalls.Items {
		items[id] = present
	}
	keepNames := func(names []string) []string {
		kept := make([]string, 0, len(names))
		for _, name := range names {
			if _, preinstall := catalogue[name]; !preinstall {
				kept = append(kept, name)
			} else if _, explicit := items[name]; !explicit {
				items[name] = true
			}
		}
		return kept
	}
	packages.Official = keepNames(packages.Official)
	packages.AUR = keepNames(packages.AUR)
	absent := make([]profile.PackageAbsence, 0, len(packages.Absent))
	for _, item := range packages.Absent {
		kind, id, ok := splitRef(item.Ref)
		if ok && (kind == "official" || kind == "aur") {
			if _, preinstall := catalogue[id]; preinstall {
				if _, explicit := items[id]; !explicit {
					items[id] = false
				}
				continue
			}
		}
		absent = append(absent, item)
	}
	packages.Absent = absent
	packages.Preinstalls.Items = items
	return packages
}

func operation(kind string, names, argv []string) model.Operation {
	id := "packages.install." + kind
	if kind == "aur" && len(names) == 1 {
		id += "." + names[0]
	}
	return model.Operation{ID: id, Provider: "packages", Action: "install", Resource: kind + ":" + strings.Join(names, ","), Items: names, Command: argv, Risk: model.RiskLow, Reversible: false}
}

func semanticInstallOperation(recipe omarchy.AppRecipe) model.Operation {
	return model.Operation{
		ID:          "packages.install.semantic." + recipe.ID,
		Provider:    "packages",
		Action:      "install",
		Resource:    "official:" + recipe.ID,
		Items:       []string{recipe.ID},
		Command:     append([]string(nil), recipe.Install...),
		Risk:        model.RiskHigh,
		Reversible:  false,
		Interactive: recipe.Interactive,
		Notice:      recipe.InstallNotice,
	}
}

func classify(packages profile.Packages) profile.Packages {
	machine := append([]string{}, packages.MachineSpecific...)
	filter := func(kind string, items []string) []string {
		portable := make([]string, 0, len(items))
		for _, name := range items {
			if machineSpecific(name) {
				machine = append(machine, kind+":"+name)
			} else {
				portable = append(portable, name)
			}
		}
		return portable
	}
	packages.Official = filter("official", packages.Official)
	packages.AUR = filter("aur", packages.AUR)
	packages.MachineSpecific = lines(strings.Join(machine, "\n"))
	var installed []string
	for _, name := range packages.Installed {
		if !machineSpecific(name) {
			installed = append(installed, name)
		}
	}
	packages.Installed = lines(strings.Join(installed, "\n"))
	return packages
}

func machineSpecific(name string) bool {
	if name == "amd-ucode" || name == "intel-ucode" || name == "fprintd" || name == "libfprint" || strings.HasPrefix(name, "libfprint-") {
		return true
	}
	for _, prefix := range []string{"nvidia", "lib32-nvidia", "opencl-nvidia", "lib32-opencl-nvidia"} {
		if name == prefix || strings.HasPrefix(name, prefix+"-") {
			return true
		}
	}
	return false
}

// Merge computes the new desired Packages state from the previous desired
// state (including Absent tombstones), the current live (classified,
// portable) detection, and the resolved Capture decision for each canonical
// ref ("official:<name>", "aur:<name>", "mise:<id>"). enabled must already
// account for provider safety and policy resolution; Merge itself only
// implements the Capture merge transition table:
//
//	unknown + present + enabled  -> present
//	present + absent  + enabled  -> tombstone
//	absent  + present + enabled  -> present
//	*       + *        disabled  -> preserve previous present/absent
//	unknown + present  disabled  -> remain unmanaged
//
// MachineSpecific/Installed are carried from current unchanged: hardware
// packages are never portable targets (not-portable-by-default), and
// Installed is a live-detection scratch field, not desired state.
// MachineSpecific is pure runtime inspection metadata, discovered fresh from
// current every call, never persisted portable state. There is no legacy
// Excluded field to carry forward: a manually excluded ref now has no
// desired state at all (stripped from Official/AUR/Mise by
// Session.SetPackageExcluded at the moment of exclusion) plus an explicit
// Capture Disabled + Restore Disabled policy rule, so Merge's own "disabled
// preserves existing desired state" rule already keeps it unmanaged.
func Merge(previous, current profile.Packages, enabled func(ref string) bool) profile.Packages {
	if current.Preinstalls.Managed {
		previous = CanonicalizePreinstallOwnership(previous, current.Preinstalls.Items)
		current = CanonicalizePreinstallOwnership(current, current.Preinstalls.Items)
	}
	result := profile.Packages{Installed: current.Installed, MachineSpecific: current.MachineSpecific}
	result.Preinstalls = mergePreinstalls(previous.Preinstalls, current.Preinstalls, enabled)
	prevAbsent, prevAbsentMise := absenceIndex(previous.Absent)
	var absences []profile.PackageAbsence
	result.Official, absences = mergeNames("official", set(previous.Official), set(current.Official), prevAbsent, enabled, absences)
	result.AUR, absences = mergeNames("aur", set(previous.AUR), set(current.AUR), prevAbsent, enabled, absences)
	result.Mise, absences = mergeMise(previous.Mise, current.Mise, prevAbsent, prevAbsentMise, enabled, absences)
	sort.Strings(result.Official)
	sort.Strings(result.AUR)
	sort.Slice(absences, func(i, j int) bool { return absences[i].Ref < absences[j].Ref })
	result.Absent = absences
	return result
}

func mergePreinstalls(previous, current profile.Preinstalls, enabled func(ref string) bool) profile.Preinstalls {
	result := profile.Preinstalls{Managed: previous.Managed, RemovedAll: previous.RemovedAll, Items: map[string]bool{}}
	if enabled("preinstalls") {
		result.Managed = current.Managed
		result.RemovedAll = current.RemovedAll
	}
	items := map[string]bool{}
	for id := range previous.Items {
		items[id] = true
	}
	for id := range current.Items {
		items[id] = true
	}
	for id := range items {
		if enabled("preinstall:" + id) {
			if present, known := current.Items[id]; known {
				result.Items[id] = present
			}
		} else if present, known := previous.Items[id]; known {
			result.Items[id] = present
		}
	}
	return result
}

// transitionResult is the Capture merge outcome for one target: whether it
// belongs in the new desired-present set, the new desired-absent (tombstone)
// set, or neither (still unmanaged).
type transitionResult int

const (
	transitionNone transitionResult = iota
	transitionPresent
	transitionAbsent
)

// transition implements the Capture merge invariant table generically.
// wasPresent/wasAbsent describe the previous desired state (both false means
// "unknown": never captured); isPresent is the current live state; enabled
// is the resolved Capture decision for this target.
func transition(wasPresent, wasAbsent, isPresent, enabled bool) transitionResult {
	if !enabled {
		switch {
		case wasPresent:
			return transitionPresent
		case wasAbsent:
			return transitionAbsent
		default:
			return transitionNone
		}
	}
	if isPresent {
		return transitionPresent
	}
	if wasPresent || wasAbsent {
		return transitionAbsent
	}
	return transitionNone
}

func mergeNames(kind string, prevPresent, curPresent map[string]bool, prevAbsent map[string]bool, enabled func(ref string) bool, absences []profile.PackageAbsence) ([]string, []profile.PackageAbsence) {
	names := map[string]bool{}
	for name := range prevPresent {
		names[name] = true
	}
	for name := range curPresent {
		names[name] = true
	}
	for ref := range prevAbsent {
		if refKind, name, ok := splitRef(ref); ok && refKind == kind {
			names[name] = true
		}
	}
	var result []string
	for name := range names {
		ref := kind + ":" + name
		switch transition(prevPresent[name], prevAbsent[ref], curPresent[name], enabled(ref)) {
		case transitionPresent:
			result = append(result, name)
		case transitionAbsent:
			absences = append(absences, profile.PackageAbsence{Ref: ref})
		}
	}
	return result, absences
}

func absenceIndex(absences []profile.PackageAbsence) (map[string]bool, profile.MiseTools) {
	refs := make(map[string]bool, len(absences))
	mise := profile.MiseTools{}
	for _, absence := range absences {
		refs[absence.Ref] = true
		if kind, id, ok := splitRef(absence.Ref); ok && kind == "mise" && absence.Mise != nil {
			mise[id] = absence.Mise
		}
	}
	return refs, mise
}

func splitRef(ref string) (string, string, bool) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), ":")
	return kind, name, ok
}

func set(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item] = true
	}
	return m
}

func lines(out string) []string {
	seen := map[string]bool{}
	var result []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !seen[line] {
			seen[line] = true
			result = append(result, line)
		}
	}
	sort.Strings(result)
	return result
}
