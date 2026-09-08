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
	if err := ValidateExclusions(saved); err != nil {
		return err
	}
	if err := ValidateMiseSecrets(saved.Mise); err != nil {
		return err
	}
	if _, err := p.Detect(ctx); err != nil {
		return err
	}
	if len(ApplyExclusions(saved, saved.Excluded).Mise) > 0 {
		if _, err := p.Runner.Run(ctx, "mise", "--version"); err != nil {
			return fmt.Errorf("mise is required to restore mise packages: %w", err)
		}
	}
	return nil
}

func Diff(saved, current profile.Packages) []model.Change {
	saved, current = classify(saved), classify(current)
	current = ApplyExclusions(current, saved.Excluded)
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
	if err := ValidateExclusions(saved); err != nil {
		return model.RestorePlan{}, err
	}
	if err := ValidateMiseSecrets(saved.Mise); err != nil {
		return model.RestorePlan{}, err
	}
	saved, current = classify(saved), classify(current)
	current = ApplyExclusions(current, saved.Excluded)
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
	for _, name := range saved.Excluded {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: name, Reason: "excluded by profile"})
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
		candidate, err = BuildMiseAppendCandidate(snapshot.Bytes, current.Mise, additions)
	} else {
		candidate, err = EncodeMiseTools(additions)
	}
	if err != nil {
		return skipMiseAdditions(plan, additions, err.Error()), nil
	}
	ids := sortedMiseIDs(additions)
	write := model.FileWrite{Generated: true, Content: candidate, Destination: p.MiseGlobalConfig, SourceHash: hashBytes(candidate), Backup: snapshot.Exists}
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
	current = ApplyExclusions(current, saved.Excluded)
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

func ApplyExclusions(packages profile.Packages, excluded []string) profile.Packages {
	packages.Excluded = lines(strings.Join(excluded, "\n"))
	for _, ref := range packages.Excluded {
		kind, name, ok := splitRef(ref)
		if !ok {
			continue
		}
		switch kind {
		case "official":
			packages.Official = remove(packages.Official, name)
		case "aur":
			packages.AUR = remove(packages.AUR, name)
		case "mise":
			packages.Mise = cloneMiseWithout(packages.Mise, name)
		}
		packages.Installed = remove(packages.Installed, name)
	}
	return packages
}

func Exclude(packages profile.Packages, refs []string) (profile.Packages, []string, error) {
	result := clone(packages)
	var changed []string
	for _, ref := range refs {
		canonical, err := resolveRef(result, ref, false)
		if err != nil {
			return packages, nil, err
		}
		if contains(result.Excluded, canonical) {
			continue
		}
		kind, name, _ := splitRef(canonical)
		if kind == "official" {
			result.Official = remove(result.Official, name)
		} else if kind == "aur" {
			result.AUR = remove(result.AUR, name)
		}
		result.Excluded = append(result.Excluded, canonical)
		changed = append(changed, canonical)
	}
	result.Excluded = lines(strings.Join(result.Excluded, "\n"))
	return result, changed, nil
}

func Include(packages profile.Packages, refs []string) (profile.Packages, []string, error) {
	result := clone(packages)
	var changed []string
	for _, ref := range refs {
		canonical, err := resolveRef(result, ref, true)
		if err != nil {
			return packages, nil, err
		}
		if !contains(result.Excluded, canonical) {
			continue
		}
		kind, name, _ := splitRef(canonical)
		result.Excluded = remove(result.Excluded, canonical)
		if kind == "official" {
			result.Official = append(result.Official, name)
		} else if kind == "aur" {
			result.AUR = append(result.AUR, name)
		}
		changed = append(changed, canonical)
	}
	result.Official, result.AUR = lines(strings.Join(result.Official, "\n")), lines(strings.Join(result.AUR, "\n"))
	return result, changed, nil
}

func resolveRef(packages profile.Packages, ref string, excludedOnly bool) (string, error) {
	kind, name, ok := splitRef(ref)
	if !ok || (kind != "package" && kind != "official" && kind != "aur" && kind != "mise") {
		return "", fmt.Errorf("invalid package reference %q; use package:<name>, official:<name>, aur:<name>, or mise:<name>", ref)
	}
	if name == "" || ((kind == "official" || kind == "aur") && strings.ContainsAny(name, " \t\n:")) || (kind == "mise" && !validMiseRefName(name)) {
		return "", fmt.Errorf("invalid package name in %q", ref)
	}
	candidates := []string{}
	for _, candidate := range []string{"official:" + name, "aur:" + name, "mise:" + name} {
		candidateKind, _, _ := splitRef(candidate)
		_, miseManaged := packages.Mise[name]
		managed := (candidateKind == "official" && contains(packages.Official, name)) || (candidateKind == "aur" && contains(packages.AUR, name)) || (candidateKind == "mise" && miseManaged)
		if contains(packages.Excluded, candidate) || (!excludedOnly && managed) {
			candidates = append(candidates, candidate)
		}
	}
	if kind != "package" {
		canonical := kind + ":" + name
		for _, candidate := range candidates {
			if candidate == canonical {
				return canonical, nil
			}
		}
		return "", fmt.Errorf("package %s is not %s in this profile", name, kind)
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("package %s is not managed by this profile", name)
	}
	return "", fmt.Errorf("package %s is ambiguous; use official:%s, aur:%s, or mise:%s", name, name, name, name)
}

func ValidateExclusions(packages profile.Packages) error {
	for _, ref := range packages.Excluded {
		kind, name, ok := splitRef(ref)
		if !ok || (kind != "official" && kind != "aur" && kind != "mise") || name == "" || ((kind == "official" || kind == "aur") && strings.ContainsAny(name, " \t\n:")) || (kind == "mise" && !validMiseRefName(name)) {
			return fmt.Errorf("invalid excluded package reference %q", ref)
		}
		if kind == "mise" {
			if _, ok := packages.Mise[name]; !ok {
				return fmt.Errorf("excluded mise package %s has no stored declaration", name)
			}
			continue
		}
		if (kind == "official" && contains(packages.Official, name)) || (kind == "aur" && contains(packages.AUR, name)) {
			return fmt.Errorf("package %s is both managed and excluded", ref)
		}
	}
	return nil
}

func clone(packages profile.Packages) profile.Packages {
	packages.Official = append([]string{}, packages.Official...)
	packages.AUR = append([]string{}, packages.AUR...)
	packages.MachineSpecific = append([]string{}, packages.MachineSpecific...)
	packages.Excluded = append([]string{}, packages.Excluded...)
	packages.Installed = append([]string{}, packages.Installed...)
	packages.Mise = cloneMise(packages.Mise)
	return packages
}

func validMiseRefName(name string) bool {
	if strings.TrimSpace(name) != name || name == "" {
		return false
	}
	for _, r := range name {
		if r <= 0x1f || r == 0x7f || r == ' ' || r == '\t' || r == '\n' {
			return false
		}
	}
	return true
}

func cloneMise(tools profile.MiseTools) profile.MiseTools {
	result := make(profile.MiseTools, len(tools))
	for id, tool := range tools {
		normalized, err := NormalizeMiseTool(id, map[string]any(tool))
		if err == nil {
			result[id] = normalized
		}
	}
	return result
}

func cloneMiseWithout(tools profile.MiseTools, excluded string) profile.MiseTools {
	result := cloneMise(tools)
	delete(result, excluded)
	return result
}

func PreserveExcludedMise(current, previous profile.Packages) profile.Packages {
	if current.Mise == nil {
		current.Mise = profile.MiseTools{}
	}
	for _, ref := range previous.Excluded {
		kind, id, ok := splitRef(ref)
		if !ok || kind != "mise" {
			continue
		}
		if tool, ok := previous.Mise[id]; ok {
			current.Mise[id] = cloneMise(profile.MiseTools{id: tool})[id]
		}
	}
	return current
}

func splitRef(ref string) (string, string, bool) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), ":")
	return kind, name, ok
}
func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
func remove(items []string, target string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item != target {
			out = append(out, item)
		}
	}
	return out
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
