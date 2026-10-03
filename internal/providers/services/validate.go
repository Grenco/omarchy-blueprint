package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

// UnitSetRejectedError is systemd refusing the proposed unit set. Output is
// systemd-analyze's own report with the temporary validation root removed,
// so it names units as the person knows them (ADR 0027).
type UnitSetRejectedError struct {
	Err    error
	Output string
}

func (e *UnitSetRejectedError) Error() string { return e.Err.Error() }
func (e *UnitSetRejectedError) Unwrap() error { return e.Err }

// Reasons attributes systemd's messages to the units they name. A message
// about a drop-in belongs to its unit. Warnings systemd itself ignores are
// dropped; messages naming no proposed unit are returned as general.
func (e *UnitSetRejectedError) Reasons(units []string) (map[string][]string, []string) {
	byUnit := map[string][]string{}
	var general []string
	for _, line := range strings.Split(e.Output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "ignoring") {
			continue
		}
		attributed := false
		for _, unit := range units {
			rest, ok := strings.CutPrefix(line, unit+":")
			if !ok {
				rest, ok = strings.CutPrefix(line, unit+".d/")
			}
			if ok {
				byUnit[unit] = append(byUnit[unit], strings.TrimSpace(rest))
				attributed = true
				break
			}
		}
		if !attributed {
			general = append(general, line)
		}
	}
	return byUnit, general
}

// validateProposedSet never writes target configuration or touches the user
// manager. Callers supply the exact effective proposed base/drop-in bytes;
// systemd, rather than a Blueprint parser, validates the complete unit set.
func (p *Provider) validateProposedSet(ctx context.Context, files map[string][]byte) error {
	if len(files) == 0 {
		return nil
	}
	if p.Systemd == nil {
		return fmt.Errorf("proposed Services validation needs systemd")
	}
	names := make([]string, 0, len(files))
	for name := range files {
		if name == "" || name != path.Clean(name) || path.IsAbs(name) || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") {
			return fmt.Errorf("invalid proposed Services path %q", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	root, err := os.MkdirTemp("", "blueprint-services-verify-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	units := []string{}
	for _, name := range names {
		destination := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(destination, files[name], 0o600); err != nil {
			return err
		}
		if !strings.Contains(name, "/") {
			units = append(units, destination)
		}
	}
	if len(units) == 0 {
		return fmt.Errorf("proposed Services drop-ins require a base definition for validation")
	}
	err = p.Systemd.VerifyUnitSet(ctx, ProposedUnitSet{Root: root, Files: units})
	var unavailable *ValidationUnavailableError
	var run *command.RunError
	if err == nil || errors.As(err, &unavailable) || !errors.As(err, &run) {
		return err
	}
	output := run.Output
	roots := []string{root}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil && resolved != root {
		roots = append(roots, resolved)
	}
	for _, prefix := range roots {
		output = strings.ReplaceAll(output, prefix+string(filepath.Separator), "")
	}
	return &UnitSetRejectedError{Err: err, Output: output}
}
