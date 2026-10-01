package services

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func (p *Provider) resolveSourceRoots() error {
	if p.ResolveRoots != nil {
		roots, err := p.ResolveRoots()
		if err != nil {
			return err
		}
		p.Roots = roots
	}
	if !filepath.IsAbs(p.Roots.UserConfigDir) {
		return fmt.Errorf("Services needs an absolute persistent user configuration root")
	}
	return nil
}

func observedByName(units []ObservedUnit) (map[string]ObservedUnit, error) {
	result := map[string]ObservedUnit{}
	for _, unit := range units {
		if _, duplicate := result[unit.Name]; duplicate {
			return nil, fmt.Errorf("duplicate inspected Services identity %q", unit.Name)
		}
		result[unit.Name] = unit
	}
	return result, nil
}

func selectedServiceUnits(data profile.Data, rc workflow.RestoreContext) ([]profile.ServiceUnit, []model.Skipped, error) {
	var selected []profile.ServiceUnit
	var skipped []model.Skipped
	for _, unit := range data.Services.Units {
		decision, err := rc.Require(unit.Name)
		if err != nil {
			return nil, nil, err
		}
		if !decision.Restore && !decision.CompatibilityApply {
			skipped = append(skipped, model.Skipped{Provider: "services", Resource: unit.Name, Reason: decision.Reason})
			continue
		}
		if unit.Presence == profile.ServiceAbsent && rc.Options.Convergence != policy.ConvergenceExact {
			skipped = append(skipped, model.Skipped{Provider: "services", Resource: unit.Name, Reason: "Additive leaves explicit Services absence unapplied"})
			continue
		}
		unit.DropIns = append([]profile.ServiceArtifact(nil), unit.DropIns...)
		sort.Slice(unit.DropIns, func(i, j int) bool { return unit.DropIns[i].Path < unit.DropIns[j].Path })
		unit.Instances = append([]profile.ServiceInstance(nil), unit.Instances...)
		sort.Slice(unit.Instances, func(i, j int) bool { return unit.Instances[i].Name < unit.Instances[j].Name })
		for n := range unit.Instances {
			unit.Instances[n].DropIns = append([]profile.ServiceArtifact(nil), unit.Instances[n].DropIns...)
			items := unit.Instances[n].DropIns
			sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
		}
		selected = append(selected, unit)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Name < selected[j].Name })
	return selected, skipped, nil
}

type unitEffects struct {
	files, states []model.Operation
	proposed      map[string][]byte
}

func (p *Provider) Plan(ctx context.Context, data profile.Data, _ omarchy.Info, rc workflow.RestoreContext) (workflow.RestoreFragment, error) {
	fragment := workflow.RestoreFragment{}
	if err := profile.Validate(data); err != nil {
		return fragment, err
	}
	selected, skipped, err := selectedServiceUnits(data, rc)
	if err != nil {
		return fragment, err
	}
	fragment.Skipped = skipped
	assessment := serviceAssessment{}
	if len(selected) == 0 {
		fragment.Compatibility, err = assessment.category(false)
		return fragment, err
	}
	if err := p.resolveSourceRoots(); err != nil {
		return fragment, err
	}
	if p.Systemd == nil {
		return fragment, fmt.Errorf("Services needs systemd inspection")
	}
	units, inspectErr := p.Systemd.InspectUserUnits(ctx)
	if inspectErr != nil {
		if ctx.Err() != nil {
			return fragment, ctx.Err()
		}
		fragment.Requirements = []model.Requirement{{ID: "services.user-manager", Provider: "services", Kind: "user-manager", Reason: "Services needs a usable systemd user login session; open that session and plan again", Remediation: []string{"systemctl", "--user", "status"}}}
		for _, unit := range selected {
			assessment.findings = append(assessment.findings, model.CompatibilityFinding{Target: unit.Name, Code: "services.user-manager.unavailable", Summary: "User service manager cannot be inspected; satisfy the session requirement and replan", State: model.CompatibilityUnknown, Authority: model.CompatibilityBlocked, RequirementID: "services.user-manager"})
		}
		fragment.Compatibility, err = assessment.category(true)
		return fragment, err
	}
	current, err := observedByName(units)
	if err != nil {
		return fragment, err
	}
	assessment.evidence = append(assessment.evidence, model.CompatibilityEvidence{Kind: "services.user-manager", Summary: "Current user manager supplied read-only persistent unit evidence"})
	proposed := map[string][]byte{}
	var fileOps, stateOps []model.Operation
	needsReload := false
	for _, unit := range selected {
		effects, reason, code, blocked, err := p.planUnit(unit, current, rc)
		if err != nil {
			return workflow.RestoreFragment{}, err
		}
		if reason != "" {
			fragment.Skipped = append(fragment.Skipped, model.Skipped{Provider: "services", Resource: unit.Name, Reason: reason})
			assessment.finding(unit.Name, code, reason, blocked)
			continue
		}
		unknownDependency := false
		for _, dependency := range current[unit.Name].RelatedUnits {
			if observed, found := current[dependency]; !found || !observed.TopologyKnown {
				unknownDependency = true
			}
		}
		if unknownDependency {
			if rc.Options.Convergence == policy.ConvergenceExact && hasRemoval(effects.files) {
				assessment.finding(unit.Name, "services.exact.dependency", "Exact removal withheld because dependency evidence is incomplete", false)
				fragment.Skipped = append(fragment.Skipped, model.Skipped{Provider: "services", Resource: unit.Name, Reason: "Exact removal withheld because dependency evidence is incomplete"})
				continue
			}
			assessment.findings = appendIfServiceFinding(assessment.findings, model.CompatibilityFinding{Target: unit.Name, Code: "services.dependency.unestablished", Summary: "External dependency evidence is incomplete; no activation authority is granted", State: model.CompatibilityUnknown, Authority: model.CompatibilityUnchanged})
		}
		fileOps = append(fileOps, effects.files...)
		stateOps = append(stateOps, effects.states...)
		for name, bytes := range effects.proposed {
			if previous, exists := proposed[name]; exists && string(previous) != string(bytes) {
				return fragment, fmt.Errorf("conflicting proposed Services artifact %q", name)
			}
			proposed[name] = bytes
		}
		assessment.evidence = append(assessment.evidence, model.CompatibilityEvidence{Kind: "services.persistent-intent", Summary: "Selected unit representation and guarded user-level effects are established: " + unit.Name})
		if unit.Presence == profile.ServicePresent && current[unit.Name].LoadState != "" && current[unit.Name].LoadState != "loaded" && unit.StartIntent != profile.ServiceStartMasked && len(effects.proposed) > 0 {
			needsReload = true
		}
		if unit.LinkedSource != "" && current[unit.Name].Name != "" && current[unit.Name].FragmentPath != unit.LinkedSource {
			needsReload = true
		}
		if current[unit.Name].ObservedActive && len(effects.files) > 0 && unit.Presence == profile.ServicePresent {
			fragment.Skipped = append(fragment.Skipped, model.Skipped{Provider: "services", Resource: unit.Name, Reason: "Already running; Blueprint will not restart or reload this process. Persistent changes apply on its next normal start."})
		}
	}
	if len(proposed) > 0 {
		if err := p.validateProposedSet(ctx, proposed); err != nil {
			if ctx.Err() != nil {
				return fragment, ctx.Err()
			}
			var unavailable *ValidationUnavailableError
			if errors.As(err, &unavailable) {
				fragment.Requirements = append(fragment.Requirements, model.Requirement{ID: "services.verifier", Provider: "services", Kind: "unit-verification", Reason: "Authoritative systemd unit verification is unavailable; restore the supported systemd tooling externally and plan again", Remediation: []string{"systemd-analyze", "--version"}})
				for _, unit := range selected {
					assessment.findings = append(assessment.findings, model.CompatibilityFinding{Target: unit.Name, Code: "services.verifier.unavailable", Summary: "Systemd parser evidence is unavailable; no persistent work can apply", State: model.CompatibilityUnknown, Authority: model.CompatibilityBlocked, RequirementID: "services.verifier"})
				}
				fragment.Compatibility, err = assessment.category(true)
				return fragment, err
			}
			for _, unit := range selected {
				assessment.finding(unit.Name, "services.unit.invalid", "Systemd could not validate the proposed effective user-service set", true)
			}
			fragment.Compatibility, err = assessment.category(true)
			return fragment, err
		}
		assessment.evidence = append(assessment.evidence, model.CompatibilityEvidence{Kind: "services.systemd-verification", Summary: "Systemd accepted the isolated proposed effective unit set without generators or target writes"})
		assessment.evidence = append(assessment.evidence, model.CompatibilityEvidence{Kind: "services.proposed-set-identity", Summary: proposedSetIdentity(proposed)})
	}
	fragment.Operations = fileOps
	var reloadID string
	if len(fileOps) > 0 || needsReload {
		reloadID = "services.daemon-reload"
		reload := serviceCommand("daemon-reload", "", "")
		reload.ID = reloadID
		for _, file := range fileOps {
			reload.DependsOn = append(reload.DependsOn, file.ID)
		}
		fragment.Operations = append(fragment.Operations, reload)
	}
	for n := range stateOps {
		if reloadID != "" {
			stateOps[n].DependsOn = append(stateOps[n].DependsOn, reloadID)
		}
	}
	fragment.Operations = append(fragment.Operations, stateOps...)
	if len(stateOps) > 0 {
		refresh := serviceCommand("daemon-reload", "", "")
		refresh.ID = "services.state-daemon-reload"
		for _, state := range stateOps {
			refresh.DependsOn = append(refresh.DependsOn, state.ID)
		}
		fragment.Operations = append(fragment.Operations, refresh)
	}
	fragment.Compatibility, err = assessment.category(true)
	return fragment, err
}

func appendIfServiceFinding(findings []model.CompatibilityFinding, item model.CompatibilityFinding) []model.CompatibilityFinding {
	for _, prior := range findings {
		if prior.Target == item.Target && prior.Code == item.Code {
			return findings
		}
	}
	return append(findings, item)
}

func proposedSetIdentity(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		sum := sha256.Sum256(files[name])
		fmt.Fprintf(hash, "%s\x00%x\n", name, sum)
	}
	return fmt.Sprintf("Proposed effective unit-set content identity: %x", hash.Sum(nil))
}
func hasRemoval(ops []model.Operation) bool {
	for _, op := range ops {
		if op.Delete != nil {
			return true
		}
	}
	return false
}
func serviceCommand(verb, name, target string) model.Operation {
	id := "services." + verb
	if name != "" {
		sum := sha256.Sum256([]byte(name))
		id += fmt.Sprintf(".%x", sum[:12])
	}
	command := []string{"systemctl", "--user", verb}
	if name != "" {
		command = append(command, "--no-reload", "--", name)
	}
	return model.Operation{ID: id, Provider: "services", Action: verb, Resource: target, Command: command, Risk: model.RiskMedium, Reversible: false}
}

func (p Provider) desiredServiceFile(relative, hash, mode string) (capturedFile, error) {
	file, err := readServiceFile(filepath.Join(p.ProfileDir, "services", filepath.FromSlash(relative)))
	if err != nil {
		return capturedFile{}, err
	}
	if hash == "" || file.hash != hash {
		return capturedFile{}, fmt.Errorf("Services snapshot %q does not match recorded provenance", relative)
	}
	file.mode = mode
	return file, nil
}

func (p Provider) planUnit(saved profile.ServiceUnit, current map[string]ObservedUnit, rc workflow.RestoreContext) (unitEffects, string, string, bool, error) {
	effects := unitEffects{proposed: map[string][]byte{}}
	actual, found := current[saved.Name]
	if found && !actual.Template && actual.LoadState == "not-found" && actual.FragmentPath == "" {
		actual = ObservedUnit{}
		found = false
	}
	force, exact := rc.Options.Conflicts == policy.ConflictForce, rc.Options.Convergence == policy.ConvergenceExact
	failure := func(code, reason string, blocked bool) (unitEffects, string, string, bool, error) {
		return unitEffects{}, reason, code, blocked, nil
	}
	if found && (actual.Generated || actual.Transient || actual.Runtime && !actual.Persistent) {
		return failure("services.source.runtime", "Runtime/generated user units cannot receive persistent Services effects", true)
	}
	if found && !actual.TopologyKnown {
		return failure("services.topology.unestablished", "Effective unit source and drop-in topology are unknown; all affected work is withheld", false)
	}
	if saved.Management == profile.ServiceManagementDefinition && found && actual.FragmentPath != filepath.Join(p.Roots.UserConfigDir, saved.Name) {
		return failure("services.owner.external", "Effective definition is external or linked; Force cannot acquire its ownership", true)
	}
	if saved.Management == profile.ServiceManagementCustomization && saved.LinkedSource == "" && !found && (hasPresentDropIn(saved.DropIns) || saved.StartIntent != profile.ServiceStartMasked && saved.StartIntent != profile.ServiceStartNotManaged) {
		return failure("services.base.unavailable", "Required external base is unavailable; Blueprint will not copy or invent it", true)
	}
	if saved.Management == profile.ServiceManagementCustomization && saved.LinkedSource == "" && saved.Presence == profile.ServicePresent && (hasPresentDropIn(saved.DropIns) || saved.StartIntent == profile.ServiceStartEnabled || saved.StartIntent == profile.ServiceStartDisabled || saved.StartIntent == profile.ServiceStartIndirect) && (actual.FragmentPath == "" || actual.FragmentPath == "/dev/null") {
		return failure("services.base.unavailable", "Required external base source is hidden or unavailable; mutation is withheld", true)
	}
	if saved.Presence == profile.ServiceAbsent && !exact {
		return effects, "", "", false, nil
	}
	if saved.Management == profile.ServiceManagementDefinition && saved.StartIntent == profile.ServiceStartMasked {
		return failure("services.mask.definition", "Masking a definition in the user configuration path would replace authored content; mutation withheld", false)
	}
	if found && hasUnrepresentableDropIn(actual, p.Roots) {
		return failure("services.drop-in.unestablished", "Effective shared or alias drop-in scope cannot be reconstructed safely", false)
	}
	if saved.LinkedSource != "" {
		if !filepath.IsAbs(saved.LinkedSource) {
			return failure("services.link.unavailable", "Linked definition source must resolve to an absolute existing external path", true)
		}
		file, err := readServiceFile(saved.LinkedSource)
		if err != nil {
			return failure("services.link.unavailable", "Linked external source is absent, sensitive, or cannot be read safely", true)
		}
		path := filepath.Join(p.Roots.UserConfigDir, saved.Name)
		if reason := unsafeServiceParents(path, p.Roots); reason != "" {
			return failure("services.link.unestablished", reason, false)
		}
		target, err := os.Readlink(path)
		if err != nil {
			if _, existsErr := os.Lstat(path); !errors.Is(existsErr, os.ErrNotExist) {
				return failure("services.link.conflict", "Existing definition or link differs; Force cannot acquire external linked content", true)
			}
			sum := sha256.Sum256([]byte(saved.Name))
			mode, err := strconv.ParseUint(file.mode, 8, 32)
			if err != nil {
				return effects, "", "", false, err
			}
			effects.files = append(effects.files, model.Operation{ID: fmt.Sprintf("services.link.%x", sum[:12]), Provider: "services", Resource: saved.Name, Action: "link", Symlink: &model.SymlinkWrite{Destination: path, Target: saved.LinkedSource, ExpectedMissing: true, ExpectedTarget: regularFilePrecondition(file, uint32(mode)), RejectSymlinkParents: true}, Risk: model.RiskMedium})
		} else if target != saved.LinkedSource {
			return failure("services.link.conflict", "Existing linked-source relationship conflicts with selected intent", true)
		}
		effects.proposed[saved.Name] = file.data
	}
	if saved.Management == profile.ServiceManagementDefinition {
		file := capturedFile{hash: saved.DefinitionHash}
		if saved.Presence == profile.ServicePresent {
			var err error
			file, err = p.desiredServiceFile(saved.Definition, saved.DefinitionHash, "")
			if err != nil {
				return effects, "", "", false, err
			}
			effects.proposed[saved.Name] = file.data
		}
		op, reason, err := p.planArtifact(saved.Name, saved.Definition, filepath.Join(p.Roots.UserConfigDir, saved.Name), file, force, saved.Presence == profile.ServiceAbsent)
		if err != nil {
			return effects, "", "", false, err
		}
		if reason != "" {
			return failure("services.artifact.withheld", reason, false)
		}
		if op.ID != "" {
			effects.files = append(effects.files, op)
		}
	} else if saved.LinkedSource == "" && found && actual.FragmentPath != "" && actual.FragmentPath != "/dev/null" && (hasPresentDropIn(saved.DropIns) || saved.StartIntent != profile.ServiceStartMasked && saved.StartIntent != profile.ServiceStartNotManaged) {
		base, err := readServiceFile(actual.FragmentPath)
		if err != nil {
			return failure("services.base.unestablished", "External base cannot be read safely for proposed-set validation", true)
		}
		effects.proposed[saved.Name] = base.data
	}
	// Retain the external/current effective drop-ins for validation, including
	// package-owned overlays, then replace only the exact managed names.
	if _, hasBase := effects.proposed[saved.Name]; hasBase {
		for _, path := range actual.DropInPaths {
			if filepath.Base(filepath.Dir(path)) != saved.Name+".d" {
				return failure("services.drop-in.unestablished", "Broader effective drop-in topology cannot be isolated reliably", false)
			}
			file, err := readServiceFile(path)
			if err != nil {
				return failure("services.drop-in.unestablished", "Effective drop-in cannot be read safely", false)
			}
			effects.proposed[saved.Name+".d/"+filepath.Base(path)] = file.data
		}
	}
	for _, item := range saved.DropIns {
		if item.Presence == profile.ServiceAbsent && !exact {
			continue
		}
		file := capturedFile{hash: item.Hash, mode: item.Mode}
		if item.Presence == profile.ServicePresent {
			var err error
			file, err = p.desiredServiceFile(item.Path, item.Hash, item.Mode)
			if err != nil {
				return effects, "", "", false, err
			}
			if _, hasBase := effects.proposed[saved.Name]; !hasBase {
				return failure("services.base.unavailable", "Managed drop-in requires a parsed external or owned base", true)
			}
			effects.proposed[saved.Name+".d/"+filepath.Base(item.Path)] = file.data
		} else {
			delete(effects.proposed, saved.Name+".d/"+filepath.Base(item.Path))
			if item.Hash == "" || item.Mode == "" {
				if _, err := os.Lstat(filepath.Join(p.Roots.UserConfigDir, saved.Name+".d", filepath.Base(item.Path))); !errors.Is(err, os.ErrNotExist) {
					return failure("services.exact.provenance", "Exact drop-in removal lacks prior content and mode provenance", false)
				}
			}
		}
		op, reason, err := p.planArtifact(saved.Name, item.Path, filepath.Join(p.Roots.UserConfigDir, saved.Name+".d", filepath.Base(item.Path)), file, force, item.Presence == profile.ServiceAbsent)
		if err != nil {
			return effects, "", "", false, err
		}
		if reason != "" {
			return failure("services.artifact.withheld", reason, false)
		}
		if op.ID != "" {
			effects.files = append(effects.files, op)
		}
	}
	if saved.Mask != nil && saved.Mask.Presence == profile.ServiceAbsent && exact && userMask(saved.Name, p.Roots) {
		return failure("services.exact.mask", "Exact unmask requires prior managed mask identity provenance not present in this profile", false)
	}
	if saved.Presence == profile.ServicePresent {
		states, reason := p.planStartIntent(saved.Name, saved.StartIntent, actual, found, force, saved.Name)
		if reason != "" {
			return failure("services.state.withheld", reason, false)
		}
		effects.states = append(effects.states, states...)
	}
	for _, instance := range saved.Instances {
		if instance.Presence == profile.ServiceAbsent {
			if exact && current[instance.Name].Name != "" && current[instance.Name].StartIntent != profile.ServiceStartDisabled {
				return failure("services.exact.instance", "Exact instance removal lacks recorded managed enablement provenance", false)
			}
		}
		if instance.Mask != nil && instance.Mask.Presence == profile.ServiceAbsent && exact && userMask(instance.Name, p.Roots) {
			return failure("services.exact.mask", "Exact instance unmask lacks recorded prior managed mask identity provenance", false)
		}
		if instance.Presence == profile.ServicePresent {
			if saved.Presence != profile.ServicePresent {
				return failure("services.instance.base.unavailable", "Configured instance requires a present template; mutation withheld", true)
			}
			instanceActual := current[instance.Name]
			expectedSource := actual.FragmentPath
			if saved.Management == profile.ServiceManagementDefinition {
				expectedSource = filepath.Join(p.Roots.UserConfigDir, saved.Name)
			} else if saved.LinkedSource != "" {
				expectedSource = saved.LinkedSource
			}
			if instance.StartIntent != profile.ServiceStartMasked && instanceActual.Name != "" && instanceActual.TopologyKnown && instanceActual.FragmentPath != "" && instanceActual.FragmentPath != expectedSource {
				return failure("services.instance.owner.external", "Configured instance resolves to a different external definition; Force cannot acquire it", true)
			}
			states, reason := p.planStartIntent(instance.Name, instance.StartIntent, instanceActual, instanceActual.Name != "", force, saved.Name)
			if reason != "" {
				return failure("services.state.withheld", reason, false)
			}
			effects.states = append(effects.states, states...)
		}
		// Configured-state absence does not erase independent managed
		// mask/drop-in intent for this instance.
		if base, exists := effects.proposed[saved.Name]; exists {
			effects.proposed[instance.Name] = base
		}
		for _, dropIn := range instance.DropIns {
			if dropIn.Presence == profile.ServiceAbsent && !exact {
				continue
			}
			file := capturedFile{hash: dropIn.Hash, mode: dropIn.Mode}
			destination := filepath.Join(p.Roots.UserConfigDir, instance.Name+".d", filepath.Base(dropIn.Path))
			if dropIn.Presence == profile.ServicePresent {
				if _, hasBase := effects.proposed[instance.Name]; !hasBase {
					return failure("services.base.unavailable", "Managed instance drop-in requires a parsed present template base", true)
				}
				var err error
				file, err = p.desiredServiceFile(dropIn.Path, dropIn.Hash, dropIn.Mode)
				if err != nil {
					return effects, "", "", false, err
				}
				effects.proposed[instance.Name+".d/"+filepath.Base(dropIn.Path)] = file.data
			} else {
				delete(effects.proposed, instance.Name+".d/"+filepath.Base(dropIn.Path))
				if dropIn.Hash == "" || dropIn.Mode == "" {
					if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
						return failure("services.exact.provenance", "Exact instance drop-in removal lacks prior content/mode", false)
					}
				}
			}
			op, reason, err := p.planArtifact(saved.Name, dropIn.Path, destination, file, force, dropIn.Presence == profile.ServiceAbsent)
			if err != nil {
				return effects, "", "", false, err
			}
			if reason != "" {
				return failure("services.artifact.withheld", reason, false)
			}
			if op.ID != "" {
				effects.files = append(effects.files, op)
			}
		}
	}
	return effects, "", "", false, nil
}

func (p Provider) planStartIntent(name string, desired profile.ServiceStartIntent, actual ObservedUnit, found, force bool, target string) ([]model.Operation, string) {
	if desired == "" || desired == profile.ServiceStartNotManaged || desired == profile.ServiceStartIndirect {
		return nil, ""
	}
	if reason := unsafeServiceParents(filepath.Join(p.Roots.UserConfigDir, name), p.Roots); reason != "" {
		return nil, reason
	}
	if found && (actual.Generated || actual.Transient || actual.Runtime && !actual.Persistent || !actual.TopologyKnown) {
		return nil, "Configured instance/current persistent topology is not established"
	}
	if found && actual.StartIntent == desired && (desired != profile.ServiceStartMasked || userMask(name, p.Roots)) {
		return nil, ""
	}
	if found && actual.StartIntent == profile.ServiceStartIndirect && (desired == profile.ServiceStartEnabled || desired == profile.ServiceStartDisabled) {
		return nil, "Static/indirect unit has no established direct enablement operation; state change withheld"
	}
	if strings.HasSuffix(actual.RawUnitFileState, "-runtime") {
		return nil, "Runtime-only enablement/mask cannot establish persistent mutation authority"
	}
	var result []model.Operation
	if desired == profile.ServiceStartMasked {
		path := filepath.Join(p.Roots.UserConfigDir, name)
		if _, err := os.Lstat(path); err == nil {
			return nil, "Mask would replace an existing user definition or link; target left untouched"
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, "Mask target cannot be inspected safely"
		}
		return []model.Operation{serviceCommand("mask", name, target)}, ""
	}
	if actual.StartIntent == profile.ServiceStartMasked {
		if !force || !userMask(name, p.Roots) {
			return nil, "Existing mask is a conflict; Safe preserves it and Force cannot change an external mask"
		}
		return nil, "Underlying masked base topology is not established; unmask externally then replan"
	}
	verb := "enable"
	if desired == profile.ServiceStartDisabled {
		verb = "disable"
	}
	result = append(result, serviceCommand(verb, name, target))
	return result, ""
}

func hasPresentDropIn(items []profile.ServiceArtifact) bool {
	for _, item := range items {
		if item.Presence == profile.ServicePresent {
			return true
		}
	}
	return false
}
