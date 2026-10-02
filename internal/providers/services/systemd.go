package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Systemctl uses the current user's manager. Inspection never invokes a
// mutator; all later effects must come from explicit approved operations.
type Systemctl struct{ Runner command.Runner }

var _ Systemd = Systemctl{}

const inspectionOutputLimit = 4 << 20

const userUnitProperties = "Id,Names,LoadState,UnitFileState,ActiveState,FragmentPath,DropInPaths,SourcePath,Transient,Requires,Wants,BindsTo,PartOf,Triggers,TriggeredBy,Type,Result"

// ValidationUnavailableError distinguishes missing inspection capability from
// affirmative rejection of a proposed effective unit set.
type ValidationUnavailableError struct{ Err error }

func (e *ValidationUnavailableError) Error() string {
	return "systemd unit verification is unavailable: " + e.Err.Error()
}
func (e *ValidationUnavailableError) Unwrap() error { return e.Err }

func (s Systemctl) inspect(ctx context.Context, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Runner == nil {
		return "", fmt.Errorf("systemd user inspection has no command runner")
	}
	output, err := command.RunOutput(ctx, s.Runner, inspectionOutputLimit, "systemctl", append([]string{"--user"}, args...)...)
	if canceled := ctx.Err(); canceled != nil {
		return "", canceled
	}
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func (s Systemctl) InspectUserUnits(ctx context.Context) ([]ObservedUnit, error) {
	files, err := s.inspect(ctx, "list-unit-files", "--all", "--no-legend", "--plain")
	if err != nil {
		return nil, fmt.Errorf("list persistent user units: %w", err)
	}
	loaded, err := s.inspect(ctx, "list-units", "--all", "--no-legend", "--plain")
	if err != nil {
		return nil, fmt.Errorf("list loaded user units: %w", err)
	}
	identities := map[string]string{}
	for _, line := range strings.Split(files, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.Contains(fields[0], ".") {
			identities[fields[0]] = fields[1]
		}
	}
	for _, line := range strings.Split(loaded, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "●" {
			fields = fields[1:]
		}
		if len(fields) > 0 && strings.Contains(fields[0], ".") {
			if _, listed := identities[fields[0]]; !listed {
				identities[fields[0]] = ""
			}
		}
	}
	names := make([]string, 0, len(identities))
	for name := range identities {
		names = append(names, name)
	}
	sort.Strings(names)
	byID := make(map[string]ObservedUnit, len(names))
	eligible := make([]string, 0, len(names))
	for _, name := range names {
		if strings.Contains(name, "@.") {
			unit := catalogTemplate(name, identities[name])
			byID[unit.Name] = unit
			continue
		}
		eligible = append(eligible, name)
	}
	canonical := make(map[string]map[string]string)
	aliasIDs := make(map[string]string)
	for start := 0; start < len(eligible); start += userUnitBatchSize {
		end := min(start+userUnitBatchSize, len(eligible))
		batch := eligible[start:end]
		args := append([]string{"show", "--all", "--no-pager", "--property=" + userUnitProperties, "--"}, batch...)
		output, err := s.inspect(ctx, args...)
		if err != nil {
			return nil, fmt.Errorf("inspect user unit batch: %w", err)
		}
		records, err := parseUnitRecords(output)
		if err != nil {
			return nil, err
		}
		mapped, err := mapUnitRecords(batch, records)
		if err != nil {
			return nil, err
		}
		// Aliases may straddle chunks. Reject ambiguity or changing normalized
		// facts before accepting any complete inventory.
		for _, record := range records {
			if previous, exists := canonical[record.id]; exists && !sameUnitFacts(previous, record.properties) {
				return nil, fmt.Errorf("user unit inventory returned conflicting canonical facts across batches")
			}
			canonical[record.id] = record.properties
			for _, alias := range append([]string{record.id}, record.names...) {
				if previous, exists := aliasIDs[alias]; exists && previous != record.id {
					return nil, fmt.Errorf("user unit inventory returned ambiguous identity across batches")
				}
				aliasIDs[alias] = record.id
			}
		}
		for _, name := range batch {
			unit := normalizeObservedUnit(name, identities[name], mapped[name])
			byID[unit.Name] = unit
		}
	}
	names = names[:0]
	for name := range byID {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]ObservedUnit, 0, len(names))
	for _, name := range names {
		result = append(result, byID[name])
	}
	return result, nil
}

// A naked template is listed by list-unit-files but has no runnable object
// for show. `systemctl cat` mixes its own filename comments with authored
// comments, so it is not structured topology evidence. Preserve the catalogue
// state and identity, but leave source and drop-ins explicitly unresolved for
// PR B's provenance-aware discovery to establish or withhold.
func catalogTemplate(name, catalogState string) ObservedUnit {
	return normalizeObservedUnit(name, catalogState, nil)
}

func parseUnitProperties(output string) map[string]string {
	properties := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			properties[key] = value
		}
	}
	return properties
}

func normalizeObservedUnit(name, catalogState string, p map[string]string) ObservedUnit {
	if p["Id"] != "" {
		name = p["Id"]
	}
	state := p["UnitFileState"]
	if state == "" {
		state = catalogState
	}
	fragment := p["FragmentPath"]
	generated := state == "generated" || strings.Contains(fragment, "/generator/")
	transient := state == "transient" || p["Transient"] == "yes"
	runtimeSource := strings.HasPrefix(fragment, "/run/")
	runtime := generated || transient || runtimeSource || strings.HasSuffix(state, "-runtime")
	unit := ObservedUnit{
		Name: name, Kind: strings.TrimPrefix(filepath.Ext(name), "."),
		FragmentPath: fragment, DropInPaths: sortedUnitNames(strings.Fields(p["DropInPaths"])),
		TopologyKnown:    fragment != "" || p["DropInPaths"] != "",
		LoadState:        p["LoadState"],
		RawUnitFileState: state, StartIntent: normalizeStartIntent(state),
		ObservedActive: p["ActiveState"] == "active", Generated: generated, Transient: transient, Runtime: runtime,
		ServiceType: p["Type"], ActiveState: p["ActiveState"], ExecutionResult: p["Result"],
		Persistent: !generated && !transient && !runtimeSource && (fragment != "" || state == "masked") && state != "masked-runtime",
	}
	unit.Template = strings.Contains(name, "@.")
	if at := strings.IndexByte(name, '@'); at >= 0 && !unit.Template && filepath.Ext(name) != "" {
		unit.InstanceOf = name[:at+1] + filepath.Ext(name)
	}
	if strings.HasPrefix(state, "linked") {
		unit.LinkedSource = p["SourcePath"]
		if unit.LinkedSource == "" {
			unit.LinkedSource = fragment
		}
	}
	var related []string
	for _, key := range []string{"Requires", "Wants", "BindsTo", "PartOf", "Triggers", "TriggeredBy"} {
		related = append(related, strings.Fields(p[key])...)
	}
	unit.RelatedUnits = sortedUnitNames(related)
	unit.TriggeredBy = sortedUnitNames(strings.Fields(p["TriggeredBy"]))
	return unit
}

func normalizeStartIntent(raw string) profile.ServiceStartIntent {
	switch raw {
	case "enabled":
		return profile.ServiceStartEnabled
	case "disabled":
		return profile.ServiceStartDisabled
	case "masked":
		return profile.ServiceStartMasked
	case "static", "indirect":
		return profile.ServiceStartIndirect
	default:
		// In particular, *-runtime, generated and linked are observations,
		// not portable persistent enablement values.
		return profile.ServiceStartNotManaged
	}
}

func sortedUnitNames(items []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}

// VerifyUnitSet delegates parsing to systemd's non-mutating verifier. Task 6
// constructs the isolated proposed tree before calling this boundary.
func (s Systemctl) VerifyUnitSet(ctx context.Context, proposed ProposedUnitSet) error {
	if s.Runner == nil || proposed.Root == "" || len(proposed.Files) == 0 {
		return fmt.Errorf("proposed user-service validation requires a runner and isolated unit files")
	}
	rootInfo, err := os.Lstat(proposed.Root)
	if err != nil {
		return fmt.Errorf("inspect proposed user-service validation root: %w", err)
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("proposed user-service validation root must be a real directory")
	}
	root, err := filepath.EvalSymlinks(proposed.Root)
	if err != nil {
		return fmt.Errorf("resolve proposed user-service validation root: %w", err)
	}
	args := []string{"SYSTEMD_UNIT_PATH=" + root + ":", "XDG_RUNTIME_DIR=" + root, "systemd-analyze", "--user", "--generators=no", "--man=no", "verify"}
	for _, file := range proposed.Files {
		rel, err := filepath.Rel(proposed.Root, file)
		if !filepath.IsAbs(file) || err != nil || filepath.IsAbs(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("proposed service file %q is outside its validation tree", file)
		}
		info, err := os.Lstat(file)
		if err != nil {
			return fmt.Errorf("inspect proposed service file %q: %w", file, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("proposed service file %q must be regular", file)
		}
		resolved, err := filepath.EvalSymlinks(file)
		if err != nil {
			return fmt.Errorf("resolve proposed service file %q: %w", file, err)
		}
		rel, err = filepath.Rel(root, resolved)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("proposed service file %q resolves outside its validation tree", file)
		}
		args = append(args, file)
	}
	if _, err := s.Runner.Run(ctx, "env", args...); err != nil {
		var execution *command.RunError
		if errors.As(err, &execution) && (execution.ExitCode == 127 || execution.ExitCode < 0) {
			return &ValidationUnavailableError{Err: err}
		}
		return fmt.Errorf("verify proposed user-service definitions: %w", err)
	}
	return nil
}

func (s Systemctl) mutate(ctx context.Context, verb string, names ...string) error {
	if s.Runner == nil {
		return fmt.Errorf("systemd user mutation has no command runner")
	}
	args := []string{"--user", verb}
	if verb != "daemon-reload" {
		if len(names) == 0 {
			return fmt.Errorf("systemd user %s needs a selected unit", verb)
		}
		for _, name := range names {
			if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "/\\ \t\n\r") {
				return fmt.Errorf("invalid user unit %q for %s", name, verb)
			}
		}
		args = append(args, "--")
		args = append(args, names...)
	}
	if _, err := s.Runner.Run(ctx, "systemctl", args...); err != nil {
		return fmt.Errorf("systemd user %s: %w", verb, err)
	}
	return nil
}

func (s Systemctl) DaemonReload(ctx context.Context) error { return s.mutate(ctx, "daemon-reload") }
func (s Systemctl) Enable(ctx context.Context, names ...string) error {
	return s.mutate(ctx, "enable", names...)
}
func (s Systemctl) Disable(ctx context.Context, names ...string) error {
	return s.mutate(ctx, "disable", names...)
}
func (s Systemctl) Mask(ctx context.Context, names ...string) error {
	return s.mutate(ctx, "mask", names...)
}
func (s Systemctl) Unmask(ctx context.Context, names ...string) error {
	return s.mutate(ctx, "unmask", names...)
}
func (s Systemctl) Start(ctx context.Context, names ...string) error {
	return s.mutate(ctx, "start", names...)
}
