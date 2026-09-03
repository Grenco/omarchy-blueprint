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

type Provider struct{ Runner command.Runner }

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
	return classify(profile.Packages{Official: official, AUR: aur, Installed: installed}), nil
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

func Diff(saved, current profile.Packages) []model.Change {
	saved, current = classify(saved), classify(current)
	current = ApplyExclusions(current, saved.Excluded)
	savedNames := packageNames(saved)
	currentNames := packageNames(current)
	var out []model.Change
	out = append(out, diffKind("official", saved.Official, current.Official, savedNames, currentNames)...)
	out = append(out, diffKind("aur", saved.AUR, current.AUR, savedNames, currentNames)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Name < out[j].Name
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func Plan(saved, current profile.Packages, schema int, from, to string) model.RestorePlan {
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
	return plan
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
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
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
		} else {
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
		} else {
			result.AUR = append(result.AUR, name)
		}
		changed = append(changed, canonical)
	}
	result.Official, result.AUR = lines(strings.Join(result.Official, "\n")), lines(strings.Join(result.AUR, "\n"))
	return result, changed, nil
}

func resolveRef(packages profile.Packages, ref string, excludedOnly bool) (string, error) {
	kind, name, ok := splitRef(ref)
	if !ok || (kind != "package" && kind != "official" && kind != "aur") {
		return "", fmt.Errorf("invalid package reference %q; use package:<name>, official:<name>, or aur:<name>", ref)
	}
	if name == "" || strings.ContainsAny(name, " \t\n:") {
		return "", fmt.Errorf("invalid package name in %q", ref)
	}
	candidates := []string{}
	for _, candidate := range []string{"official:" + name, "aur:" + name} {
		candidateKind, _, _ := splitRef(candidate)
		managed := (candidateKind == "official" && contains(packages.Official, name)) || (candidateKind == "aur" && contains(packages.AUR, name))
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
	return "", fmt.Errorf("package %s is ambiguous; use official:%s or aur:%s", name, name, name)
}

func ValidateExclusions(packages profile.Packages) error {
	for _, ref := range packages.Excluded {
		kind, name, ok := splitRef(ref)
		if !ok || (kind != "official" && kind != "aur") || name == "" || strings.ContainsAny(name, " \t\n:") {
			return fmt.Errorf("invalid excluded package reference %q; use official:<name> or aur:<name>", ref)
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
	return packages
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
