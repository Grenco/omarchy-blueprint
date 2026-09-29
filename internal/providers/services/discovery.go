package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Roots describe the current user's persistent systemd source locations,
// injected at the application boundary. Paths classify candidates only;
// neither a location nor systemd enablement grants Blueprint ownership.
type Roots struct {
	UserConfigDir string
	UserDataDir   string
}

type ProvenanceClass string

const (
	ProvenanceUserConfig        ProvenanceClass = "user-config-candidate"
	ProvenanceUserDataAmbiguous ProvenanceClass = "user-data-ambiguous"
	ProvenanceExternal          ProvenanceClass = "external"
	ProvenanceLinked            ProvenanceClass = "linked-source"
	ProvenanceUnknown           ProvenanceClass = "unknown"
	ProvenanceRuntime           ProvenanceClass = "runtime"
)

// Candidate describes one detected unit and the management scope it could
// receive after reviewed selection. Managed is derived only from saved state.
type Candidate struct {
	Unit        ObservedUnit
	Provenance  ProvenanceClass
	Management  profile.ServiceManagementMode
	Recommended bool
	Advanced    bool
	Eligible    bool
	Managed     bool
	Missing     bool
	Reason      string
}

func supportedServiceKind(kind string) bool {
	switch kind {
	case "service", "timer", "socket", "path", "target", "slice":
		return true
	default:
		return false
	}
}

func withinUserRoot(path, root string) bool {
	if !filepath.IsAbs(path) || !filepath.IsAbs(root) || root == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func userMask(name string, roots Roots) bool {
	if roots.UserConfigDir == "" {
		return false
	}
	path := filepath.Join(roots.UserConfigDir, name)
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	link, err := os.Readlink(path)
	return err == nil && link == "/dev/null"
}

func hasUnrepresentableDropIn(unit ObservedUnit, roots Roots) bool {
	for _, path := range unit.DropInPaths {
		if withinUserRoot(path, roots.UserDataDir) {
			return true
		}
		if withinUserRoot(path, roots.UserConfigDir) && (filepath.Base(filepath.Dir(path)) != unit.Name+".d" || filepath.Ext(path) != ".conf") {
			return true
		}
	}
	return false
}

func classifyUnit(unit ObservedUnit, roots Roots) Candidate {
	candidate := Candidate{Unit: unit, Advanced: unit.Kind != "service" && unit.Kind != "timer"}
	switch {
	case unit.Generated || unit.Transient || unit.Runtime && !unit.Persistent:
		candidate.Provenance, candidate.Reason = ProvenanceRuntime, "Runtime or generated user service is not portable"
	case !unit.TopologyKnown:
		candidate.Provenance, candidate.Reason = ProvenanceUnknown, "Source and customizations not established read-only"
	case unit.InstanceOf != "" && filepath.Base(unit.FragmentPath) != unit.Name:
		candidate.Provenance, candidate.Reason = ProvenanceUnknown, "Instance uses a template definition; capture the template once after its source is established"
	case hasUnrepresentableDropIn(unit, roots):
		candidate.Provenance, candidate.Reason = ProvenanceUnknown, "Effective drop-in cannot be safely represented as a unit-specific customization"
	case unit.RawUnitFileState == "masked" && userMask(unit.Name, roots):
		candidate.Provenance, candidate.Management, candidate.Eligible = ProvenanceUserConfig, profile.ServiceManagementCustomization, true
		candidate.Reason = "Blocked from starting by a persistent user mask"
	case unit.LinkedSource != "":
		candidate.Provenance, candidate.Management, candidate.Eligible, candidate.Advanced = ProvenanceLinked, profile.ServiceManagementCustomization, true, true
		candidate.Reason = "Definition stored elsewhere; source remains external"
	case withinUserRoot(unit.FragmentPath, roots.UserConfigDir):
		candidate.Provenance, candidate.Management, candidate.Eligible = ProvenanceUserConfig, profile.ServiceManagementDefinition, true
		candidate.Recommended = !candidate.Advanced
		candidate.Reason = "Custom user service; review before managing"
	case withinUserRoot(unit.FragmentPath, roots.UserDataDir):
		candidate.Provenance, candidate.Management, candidate.Eligible, candidate.Advanced = ProvenanceUserDataAmbiguous, profile.ServiceManagementDefinition, true, true
		candidate.Reason = "Application-installed user service; authorship is uncertain"
	default:
		for _, path := range unit.DropInPaths {
			if withinUserRoot(path, roots.UserConfigDir) {
				candidate.Provenance, candidate.Management, candidate.Eligible = ProvenanceExternal, profile.ServiceManagementCustomization, true
				candidate.Reason = "Managed customization on an external service; base remains external"
				return candidate
			}
		}
		candidate.Provenance, candidate.Reason = ProvenanceExternal, "External service; base definition remains outside Blueprint"
	}
	return candidate
}

// Discover never turns a path heuristic into ownership. A saved target absent
// from the current inventory stays visible for reviewed removal or Preserve.
func Discover(units []ObservedUnit, saved profile.Services, roots Roots) []Candidate {
	wants := make(map[string]profile.ServiceUnit, len(saved.Units))
	for _, item := range saved.Units {
		wants[item.Name] = item
	}
	seen := map[string]bool{}
	var candidates []Candidate
	for _, unit := range units {
		if seen[unit.Name] || !supportedServiceKind(unit.Kind) {
			continue
		}
		seen[unit.Name] = true
		candidate := classifyUnit(unit, roots)
		prior, managed := wants[unit.Name]
		candidate.Managed = managed
		if managed && prior.Management == profile.ServiceManagementCustomization && candidate.Provenance == ProvenanceExternal && unit.TopologyKnown {
			candidate.Management, candidate.Eligible = profile.ServiceManagementCustomization, true
			candidate.Reason = "Managed customization; external base remains outside Blueprint"
		}
		if managed && !candidate.Missing && candidate.Eligible && managedServiceArtifactMissing(prior, candidate, roots) {
			candidate.Reason = "Previously managed service artifact is missing; review before recording its removal"
		}
		if candidate.Provenance == ProvenanceRuntime && !candidate.Managed {
			continue
		}
		candidates = append(candidates, candidate)
	}
	for _, saved := range saved.Units {
		if seen[saved.Name] {
			continue
		}
		candidates = append(candidates, Candidate{
			Unit: ObservedUnit{Name: saved.Name, Kind: saved.Kind}, Provenance: ProvenanceUnknown,
			Management: saved.Management, Managed: true, Missing: true, Eligible: true,
			Reason: "Previously managed service is missing; review before recording absence",
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Unit.Name < candidates[j].Unit.Name })
	return candidates
}

func (p Provider) discovered(ctx context.Context, saved profile.Services) ([]Candidate, error) {
	if p.Systemd == nil {
		return nil, fmt.Errorf("Services needs a user systemd inspection source")
	}
	units, err := p.Systemd.InspectUserUnits(ctx)
	if err != nil {
		if len(saved.Units) == 0 {
			return []Candidate{{Unit: ObservedUnit{Name: "user-manager", Kind: "service"}, Provenance: ProvenanceUnknown, Reason: "User service manager unavailable; Services cannot be inspected: " + err.Error()}}, nil
		}
		return nil, err
	}
	return Discover(units, saved, p.Roots), nil
}

func (p Provider) InspectTargets(ctx context.Context, data profile.Data) ([]workflow.TargetInspection, error) {
	candidates, err := p.discovered(ctx, data.Services)
	if err != nil {
		return nil, err
	}
	wants := make(map[string]profile.ServiceUnit, len(data.Services.Units))
	for _, unit := range data.Services.Units {
		wants[unit.Name] = unit
	}
	byName := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		byName[candidate.Unit.Name] = candidate
	}
	targets := make([]workflow.TargetInspection, 0, len(candidates))
	for _, candidate := range candidates {
		want, managed := wants[candidate.Unit.Name]
		desired := workflow.TargetUnknown
		if managed {
			if want.Presence == profile.ServiceAbsent {
				desired = workflow.TargetAbsent
			} else {
				desired = workflow.TargetPresent
			}
		}
		current := workflow.TargetPresent
		if candidate.Missing {
			current = workflow.TargetAbsent
		}
		fingerprint, safeReason := p.candidateFingerprint(candidate)
		eligible := candidate.Eligible && safeReason == ""
		reason := candidate.Reason
		if safeReason != "" {
			reason = safeReason
		}
		var recommended []string
		for _, name := range candidate.Unit.RelatedUnits {
			if child, found := byName[name]; found && child.Eligible && !child.Managed && child.Provenance == ProvenanceUserConfig {
				recommended = append(recommended, name)
			}
		}
		sort.Strings(recommended)
		needsRemovalReview := managed && managedServiceArtifactMissing(want, candidate, p.Roots)
		targets = append(targets, workflow.TargetInspection{
			Key: candidate.Unit.Name, Label: candidate.Unit.Name + " · " + candidate.Reason,
			Desired: desired, Current: current, CaptureEligible: eligible,
			RestoreEligible: eligible && managed, SafetyReason: map[bool]string{true: "", false: reason}[eligible],
			RequiresSelection: candidate.Missing && managed || needsRemovalReview && eligible || eligible && !managed,
			ReviewRemoval:     candidate.Missing && managed || needsRemovalReview,
			Advanced:          candidate.Advanced, RecommendedDependencies: recommended,
			Fingerprint: fingerprint, Capabilities: workflow.TargetCapabilities{
				SupportsCapture: true, SupportsRestore: true, SupportsDesiredAbsence: managed,
				SupportsExactRemoval: managed, PreservesMissingDesired: !managed,
			},
		})
	}
	return targets, nil
}

func managedServiceArtifactMissing(saved profile.ServiceUnit, candidate Candidate, roots Roots) bool {
	if candidate.Missing {
		return true
	}
	current := map[string]bool{}
	for _, path := range candidate.Unit.DropInPaths {
		current[path] = true
	}
	for _, dropIn := range saved.DropIns {
		if dropIn.Presence == profile.ServicePresent && !current[filepath.Join(roots.UserConfigDir, saved.Name+".d", filepath.Base(dropIn.Path))] {
			return true
		}
	}
	return saved.Mask != nil && saved.Mask.Presence == profile.ServicePresent && !userMask(saved.Name, roots)
}

func (p Provider) candidateFingerprint(candidate Candidate) (string, string) {
	type artifact struct{ Path, Hash, Mode string }
	var artifacts []artifact
	if candidate.Eligible && !candidate.Missing {
		paths := []string{}
		if candidate.Management == profile.ServiceManagementDefinition && candidate.Unit.LinkedSource == "" {
			paths = append(paths, candidate.Unit.FragmentPath)
		}
		for _, path := range candidate.Unit.DropInPaths {
			if withinUserRoot(path, p.Roots.UserConfigDir) {
				paths = append(paths, path)
			}
		}
		for _, path := range paths {
			if path == "" {
				return "", "Service source is not established read-only"
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil || resolved != filepath.Clean(path) {
				return "", "Service source passes through a link; leave it external"
			}
			hash, err := content.HashRegularFile(path)
			if err != nil {
				return "", "Service source cannot be read safely"
			}
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				return "", "Service source mode cannot be established safely"
			}
			artifacts = append(artifacts, artifact{Path: path, Hash: hash, Mode: fmt.Sprintf("%04o", info.Mode().Perm())})
		}
	}
	value, err := json.Marshal(struct {
		Candidate Candidate
		Artifacts []artifact
	}{candidate, artifacts})
	if err != nil {
		return "", "Service inspection could not be fingerprinted"
	}
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:]), ""
}
