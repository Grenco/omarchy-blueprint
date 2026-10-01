package services

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// validateProposedSet never writes target configuration or touches the user
// manager. Callers supply the exact effective proposed base/drop-in bytes;
// systemd, rather than a Blueprint parser, validates the complete unit set.
func (p Provider) validateProposedSet(ctx context.Context, files map[string][]byte) error {
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
	return p.Systemd.VerifyUnitSet(ctx, ProposedUnitSet{Root: root, Files: units})
}
