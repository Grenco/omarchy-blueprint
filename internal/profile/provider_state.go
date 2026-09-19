package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// savePackageAbsenceFile writes packages/absent.toml, or removes it when
// absent is empty: only tombstones that actually exist need to be written.
func savePackageAbsenceFile(dir string, absent []PackageAbsence) error {
	path := filepath.Join(dir, "packages", "absent.toml")
	if len(absent) == 0 {
		err := os.Remove(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := toml.Marshal(packageAbsenceFile{Package: absent})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "packages"), 0o755); err != nil {
		return err
	}
	return atomicWrite(path, b)
}

// validateProviderDesiredState enforces, for every provider with desired
// absence, that the same target cannot be both desired-present and
// desired-absent at once, and that Absent carries no duplicate identity.
func validateProviderDesiredState(d Data) error {
	if err := validatePackagesDesiredState(d.Packages); err != nil {
		return fmt.Errorf("packages: %w", err)
	}
	if err := validateThemesDesiredState(d.Themes); err != nil {
		return fmt.Errorf("themes: %w", err)
	}
	if err := validatePluginsDesiredState(d.Plugins); err != nil {
		return fmt.Errorf("plugins: %w", err)
	}
	if err := validateHooksDesiredState(d.Hooks); err != nil {
		return fmt.Errorf("hooks: %w", err)
	}
	return nil
}

func validatePackagesDesiredState(packages Packages) error {
	present := make(map[string]bool, len(packages.Official)+len(packages.AUR)+len(packages.Mise))
	for _, name := range packages.Official {
		present["official:"+name] = true
	}
	for _, name := range packages.AUR {
		present["aur:"+name] = true
	}
	for id := range packages.Mise {
		present["mise:"+id] = true
	}
	seen := make(map[string]bool, len(packages.Absent))
	for _, absence := range packages.Absent {
		if absence.Ref == "" {
			return errors.New("desired-absent package has an empty ref")
		}
		if present[absence.Ref] {
			return fmt.Errorf("%q is both desired-present and desired-absent", absence.Ref)
		}
		if seen[absence.Ref] {
			return fmt.Errorf("duplicate desired-absent package %q", absence.Ref)
		}
		seen[absence.Ref] = true
	}
	return nil
}

func validateThemesDesiredState(themes Themes) error {
	present := make(map[string]bool, len(themes.Items))
	for _, item := range themes.Items {
		present[item.ID] = true
	}
	seen := make(map[string]bool, len(themes.Absent))
	for _, absent := range themes.Absent {
		if absent.ID == "" {
			return errors.New("desired-absent theme has an empty id")
		}
		if present[absent.ID] {
			return fmt.Errorf("%q is both desired-present and desired-absent", absent.ID)
		}
		if seen[absent.ID] {
			return fmt.Errorf("duplicate desired-absent theme %q", absent.ID)
		}
		seen[absent.ID] = true
	}
	return nil
}

func validatePluginsDesiredState(plugins Plugins) error {
	present := make(map[string]bool, len(plugins.Items))
	for _, item := range plugins.Items {
		present[item.ID] = true
	}
	seen := make(map[string]bool, len(plugins.Absent))
	for _, absent := range plugins.Absent {
		if absent.ID == "" {
			return errors.New("desired-absent plugin has an empty id")
		}
		if present[absent.ID] {
			return fmt.Errorf("%q is both desired-present and desired-absent", absent.ID)
		}
		if seen[absent.ID] {
			return fmt.Errorf("duplicate desired-absent plugin %q", absent.ID)
		}
		seen[absent.ID] = true
	}
	return nil
}

func validateHooksDesiredState(hooks Hooks) error {
	present := make(map[string]bool, len(hooks.Items))
	for _, item := range hooks.Items {
		present[item.Path] = true
	}
	seen := make(map[string]bool, len(hooks.Absent))
	for _, absent := range hooks.Absent {
		if absent.Path == "" {
			return errors.New("desired-absent hook has an empty path")
		}
		if present[absent.Path] {
			return fmt.Errorf("%q is both desired-present and desired-absent", absent.Path)
		}
		if seen[absent.Path] {
			return fmt.Errorf("duplicate desired-absent hook %q", absent.Path)
		}
		seen[absent.Path] = true
	}
	return nil
}

func sortProviderDesiredState(d *Data) {
	sort.Slice(d.Packages.Absent, func(i, j int) bool { return d.Packages.Absent[i].Ref < d.Packages.Absent[j].Ref })
	sort.Slice(d.Themes.Absent, func(i, j int) bool { return d.Themes.Absent[i].ID < d.Themes.Absent[j].ID })
	sort.Slice(d.Plugins.Absent, func(i, j int) bool { return d.Plugins.Absent[i].ID < d.Plugins.Absent[j].ID })
	sort.Slice(d.Hooks.Absent, func(i, j int) bool { return d.Hooks.Absent[i].Path < d.Hooks.Absent[j].Path })
}
