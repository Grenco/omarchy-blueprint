package packages

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/graeme/omarchy-blueprint/internal/command"
	"github.com/graeme/omarchy-blueprint/internal/model"
	"github.com/graeme/omarchy-blueprint/internal/profile"
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
	return classify(profile.Packages{Official: official, AUR: aur}), nil
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
	var out []model.Change
	out = append(out, diffKind("official", saved.Official, current.Official)...)
	out = append(out, diffKind("aur", saved.AUR, current.AUR)...)
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
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	currentOfficial, currentAUR := set(current.Official), set(current.AUR)
	var missingOfficial, missingAUR []string
	for _, name := range saved.Official {
		if !currentOfficial[name] {
			missingOfficial = append(missingOfficial, name)
		}
	}
	for _, name := range saved.AUR {
		if !currentAUR[name] {
			missingAUR = append(missingAUR, name)
		}
	}
	if len(missingOfficial) > 0 {
		plan.Operations = append(plan.Operations, operation("official", missingOfficial, append([]string{"omarchy", "pkg", "add"}, missingOfficial...)))
	}
	if len(missingAUR) > 0 {
		plan.Operations = append(plan.Operations, operation("aur", missingAUR, append([]string{"omarchy", "pkg", "aur", "add"}, missingAUR...)))
	}
	for _, name := range saved.MachineSpecific {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "packages", Resource: name, Reason: "machine-specific hardware package"})
	}
	return plan
}

func Verify(saved, current profile.Packages) model.VerificationResult {
	saved, current = classify(saved), classify(current)
	var missing []string
	co, ca := set(current.Official), set(current.AUR)
	for _, name := range saved.Official {
		if !co[name] {
			missing = append(missing, "official:"+name)
		}
	}
	for _, name := range saved.AUR {
		if !ca[name] {
			missing = append(missing, "aur:"+name)
		}
	}
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}

func diffKind(kind string, saved, current []string) []model.Change {
	s, c := set(saved), set(current)
	var out []model.Change
	for _, name := range current {
		if !s[name] {
			out = append(out, model.Change{Type: model.ChangeAdd, Provider: "packages", Kind: kind, Name: name, Summary: "+ " + kind + " package " + name})
		}
	}
	for _, name := range saved {
		if !c[name] {
			out = append(out, model.Change{Type: model.ChangeRemove, Provider: "packages", Kind: kind, Name: name, Summary: "- " + kind + " package " + name})
		}
	}
	return out
}

func operation(kind string, names, argv []string) model.Operation {
	return model.Operation{ID: "packages.install." + kind, Provider: "packages", Action: "install", Resource: kind + ":" + strings.Join(names, ","), Items: names, Command: argv, Risk: model.RiskLow, Reversible: false}
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
