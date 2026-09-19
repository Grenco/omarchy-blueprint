package packages

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type Provider struct {
	Runner           command.Runner
	MiseGlobalConfig string
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
	if p.MiseGlobalConfig != "" {
		mise, err := ReadMiseTools(p.MiseGlobalConfig)
		if err != nil {
			return profile.Packages{}, fmt.Errorf("detect global mise packages: %w", err)
		}
		if err := ValidateMiseSecrets(mise); err != nil {
			return profile.Packages{}, err
		}
		packages.Mise = mise
	}
	return classify(packages), nil
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
	saved, current = classify(saved), classify(current)
	savedNames := packageNames(saved)
	currentNames := packageNames(current)
	var out []model.Change
	out = append(out, diffKind("official", saved.Official, current.Official, savedNames, currentNames)...)
	out = append(out, diffKind("aur", saved.AUR, current.AUR, savedNames, currentNames)...)
	out = append(out, diffMise(saved.Mise, current.Mise)...)
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

func (p Provider) Plan(saved, current profile.Packages, schema int, from, to string) (model.RestorePlan, error) {
	if err := ValidateMiseSecrets(saved.Mise); err != nil {
		return model.RestorePlan{}, err
	}
	saved, physicalCurrent := classify(saved), classify(current)
	current = physicalCurrent
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	currentNames := packageNames(current)
	var missingOfficial, missingAUR []string
	for _, name := range saved.Official {
		if !currentNames[name] {
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
	if len(missingAUR) > 0 {
		for _, name := range missingAUR {
			plan.Operations = append(plan.Operations, operation("aur", []string{name}, []string{"omarchy", "pkg", "aur", "add", name}))
		}
	}
	for _, name := range saved.MachineSpecific {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: name, Reason: "machine-specific hardware package"})
	}
	savedNames := packageNames(saved)
	for _, name := range current.Official {
		if !savedNames[name] {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: "official:" + name, Reason: "additional package left installed; removal disabled"})
		}
	}
	for _, name := range current.AUR {
		if !savedNames[name] {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: "aur:" + name, Reason: "additional package left installed; removal disabled"})
		}
	}
	if p.MiseGlobalConfig == "" {
		return plan, nil
	}
	additions, conflicts, extras := classifyMiseRestore(saved.Mise, current.Mise)
	for _, id := range conflicts {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: "mise:" + id, Reason: "existing Mise declaration differs; overwrite disabled"})
	}
	for _, id := range extras {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: "mise:" + id, Reason: "additional package left installed; removal disabled"})
	}
	if len(additions) == 0 {
		return plan, nil
	}
	if err := ValidateMiseMutationPath(p.MiseGlobalConfig); err != nil {
		return skipMiseAdditions(plan, additions, err.Error()), nil
	}
	snapshot, err := ReadMiseConfigSnapshot(p.MiseGlobalConfig)
	if err != nil {
		return skipMiseAdditions(plan, additions, err.Error()), nil
	}
	var candidate []byte
	if snapshot.Exists {
		candidate, err = BuildMiseAppendCandidate(snapshot.Bytes, physicalCurrent.Mise, additions)
	} else {
		candidate, err = EncodeMiseTools(additions)
	}
	if err != nil {
		return skipMiseAdditions(plan, additions, err.Error()), nil
	}
	ids := sortedMiseIDs(additions)
	write := model.FileWrite{Generated: true, Content: candidate, Destination: p.MiseGlobalConfig, SourceHash: hashBytes(candidate), Backup: snapshot.Exists, RejectSymlinkParents: true}
	if snapshot.Exists {
		write.ExpectedHash = snapshot.Hash
	} else {
		write.ExpectedMissing = true
	}
	plan.Operations = append(plan.Operations,
		model.Operation{ID: "packages.mise.configure", Provider: "packages", Action: "configure", Resource: "mise:global-tools", Items: ids, File: &write, Risk: model.RiskMedium, Reversible: snapshot.Exists},
		model.Operation{ID: "packages.mise.install", Provider: "packages", Action: "install", Resource: "mise:" + strings.Join(ids, ","), Items: ids, Command: append([]string{"mise", "-C", "/", "install"}, ids...), DependsOn: []string{"packages.mise.configure"}, Risk: miseInstallRisk(additions)},
	)
	return plan, nil
}

func Verify(saved, current profile.Packages) model.VerificationResult {
	saved, current = classify(saved), classify(current)
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
		if !ok || !EqualMiseTool(saved.Mise[id], actual) {
			missing = append(missing, "mise:"+id)
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
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

func operation(kind string, names, argv []string) model.Operation {
	id := "packages.install." + kind
	if kind == "aur" && len(names) == 1 {
		id += "." + names[0]
	}
	return model.Operation{ID: id, Provider: "packages", Action: "install", Resource: kind + ":" + strings.Join(names, ","), Items: names, Command: argv, Risk: model.RiskLow, Reversible: false}
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
	result := profile.Packages{Installed: current.Installed, MachineSpecific: current.MachineSpecific}
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
