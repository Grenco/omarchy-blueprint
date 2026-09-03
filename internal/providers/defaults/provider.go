package defaults

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/pelletier/go-toml/v2"
)

// kinds are the Omarchy-managed default applications in stable order.
var kinds = []string{"terminal", "browser", "editor", "agent"}

// Provider captures and restores Omarchy's semantic default applications.
// Omarchy validates values and owns the XDG/launch wiring; Blueprint only
// detects, replays, and verifies.
type Provider struct {
	Runner     command.Runner
	ProfileDir string
}

func (p Provider) query(ctx context.Context, kind string) (string, error) {
	out, err := p.Runner.Run(ctx, "omarchy", "default", kind)
	if err != nil {
		return "", fmt.Errorf("detect default %s: %w", kind, err)
	}
	return strings.TrimSpace(out), nil
}

// Detect queries Omarchy for every managed default. An empty value means the
// user never picked one, so the kind is unmanaged.
func (p Provider) Detect(ctx context.Context) (profile.Defaults, error) {
	var d profile.Defaults
	values := map[string]string{}
	for _, kind := range kinds {
		value, err := p.query(ctx, kind)
		if err != nil {
			return d, err
		}
		values[kind] = value
	}
	d.Terminal = values["terminal"]
	d.Browser = values["browser"]
	d.Editor = values["editor"]
	d.Agent = values["agent"]
	return d, nil
}

// Capture stores the detected defaults at defaults/defaults.toml.
func (p Provider) Capture(ctx context.Context) (profile.Defaults, error) {
	current, err := p.Detect(ctx)
	if err != nil {
		return profile.Defaults{}, err
	}
	if p.ProfileDir == "" {
		return profile.Defaults{}, fmt.Errorf("profile directory is required to capture defaults")
	}
	if err := os.MkdirAll(filepath.Join(p.ProfileDir, "defaults"), 0o755); err != nil {
		return profile.Defaults{}, err
	}
	b, err := tomlMarshal(current)
	if err != nil {
		return profile.Defaults{}, err
	}
	return current, atomicWrite(filepath.Join(p.ProfileDir, "defaults", "defaults.toml"), b)
}

// Diff compares saved desired defaults with the live machine state. A
// machine-selected default that the profile does not manage is reported as
// additive drift; restore never removes or unsets it.
func Diff(saved, current profile.Defaults) []model.Change {
	var changes []model.Change
	for _, kind := range kinds {
		desired := valueOf(saved, kind)
		actual := valueOf(current, kind)
		if desired == actual {
			continue
		}
		if desired == "" {
			changes = append(changes, model.Change{
				Type: model.ChangeAdd, Provider: "defaults", Kind: "default", Name: kind,
				Summary: "+ default " + kind + ": " + actual + " (not captured; restore will not remove it)",
			})
			continue
		}
		changes = append(changes, model.Change{
			Type: model.ChangeModify, Provider: "defaults", Kind: "default", Name: kind,
			Summary: "~ default " + kind + ": " + desired + " → " + actual,
		})
	}
	return changes
}

// Plan emits one native Omarchy operation per drifted managed default. An
// empty saved value has no desired state, so it never produces an operation.
//
// Two kinds are deliberately excluded from automatic restore:
//
//   - agent: Omarchy's agent setter ultimately launches the selected agent,
//     so an automatic restore must never invoke it;
//   - raw .desktop values: Omarchy's getters fall back to the desktop ID for
//     unmanaged applications, which its setters reject; they are skipped as
//     non-portable instead of being replayed.
func (p Provider) Plan(saved, current profile.Defaults, schema int, from, to string) model.RestorePlan {
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	for _, kind := range kinds {
		desired := valueOf(saved, kind)
		if desired == "" || desired == valueOf(current, kind) {
			continue
		}
		if kind == "agent" {
			plan.Skipped = append(plan.Skipped, model.Skipped{
				Provider: "defaults", Resource: "default:agent",
				Reason: "Omarchy's agent setter launches the selected agent; automatic set-only restore is not currently safe",
			})
			continue
		}
		if !Portable(desired) {
			plan.Skipped = append(plan.Skipped, model.Skipped{
				Provider: "defaults", Resource: "default:" + kind,
				Reason: fmt.Sprintf("%q is not an Omarchy-managed default and may not be portable", desired),
			})
			continue
		}
		plan.Operations = append(plan.Operations, model.Operation{
			ID:       "defaults.set." + kind,
			Provider: "defaults",
			Action:   "set",
			Resource: "default:" + kind,
			Items:    []string{kind},
			Command:  []string{"omarchy", "default", kind, "--install", desired},
			Risk:     model.RiskLow,
		})
	}
	return plan
}

// Portable reports whether a captured default value can be replayed by
// Omarchy's setters. Omarchy's getters fall back to the raw desktop ID for
// applications they do not manage, and those values are never accepted by the
// setters. Blueprint deliberately keeps no allowlist of Omarchy's choices.
func Portable(value string) bool {
	return value != "" && !strings.HasSuffix(value, ".desktop")
}

// Warn surfaces captured values that Omarchy returned as raw desktop IDs;
// they are visible drift but restore will skip them as non-portable.
func Warn(current profile.Defaults) []model.Change {
	var warnings []model.Change
	for _, kind := range kinds {
		value := valueOf(current, kind)
		if value != "" && !Portable(value) {
			warnings = append(warnings, model.Change{
				Type: model.ChangeWarn, Provider: "defaults", Kind: "default", Name: kind,
				Summary: fmt.Sprintf("! default %s: %q is not an Omarchy-managed default and may not be portable", kind, value),
			})
		}
	}
	return warnings
}

// Verify checks every automatically-settable managed default against the
// desired value. Agent and non-portable values are excluded: restore
// deliberately cannot set them, so they must not fail verification. Unmanaged
// kinds and extra machine defaults never fail verification.
func Verify(saved, current profile.Defaults) model.VerificationResult {
	var missing []string
	for _, kind := range kinds {
		desired := valueOf(saved, kind)
		if desired == "" || desired == valueOf(current, kind) {
			continue
		}
		if kind == "agent" || !Portable(desired) {
			continue
		}
		missing = append(missing, "default:"+kind)
	}
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}

func valueOf(d profile.Defaults, kind string) string {
	switch kind {
	case "terminal":
		return d.Terminal
	case "browser":
		return d.Browser
	case "editor":
		return d.Editor
	case "agent":
		return d.Agent
	}
	return ""
}

func tomlMarshal(d profile.Defaults) ([]byte, error) {
	return toml.Marshal(d)
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".omarchy-blueprint-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
