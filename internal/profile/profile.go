package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const Schema = 1

type Manifest struct {
	Schema  int         `toml:"schema"`
	Profile ProfileMeta `toml:"profile"`
	Omarchy OmarchyMeta `toml:"omarchy"`
	Capture CaptureMeta `toml:"capture"`
}

type ProfileMeta struct {
	Name      string    `toml:"name"`
	CreatedAt time.Time `toml:"created_at"`
	UpdatedAt time.Time `toml:"updated_at"`
}

type OmarchyMeta struct {
	CapturedVersion string `toml:"captured_version"`
	Channel         string `toml:"channel"`
}

type CaptureMeta struct {
	Packages bool `toml:"packages"`
}

type Packages struct {
	Official        []string `json:"official"`
	AUR             []string `json:"aur"`
	MachineSpecific []string `json:"machine_specific,omitempty"`
}

type Data struct {
	Manifest Manifest `json:"manifest"`
	Packages Packages `json:"packages"`
}

func New(name string, now time.Time) Data {
	return Data{Manifest: Manifest{Schema: Schema, Profile: ProfileMeta{Name: name, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}}}
}

func Load(dir string) (Data, error) {
	var d Data
	b, err := os.ReadFile(filepath.Join(dir, "profile.toml"))
	if err != nil {
		return d, err
	}
	if err := toml.Unmarshal(b, &d.Manifest); err != nil {
		return d, fmt.Errorf("parse profile.toml: %w", err)
	}
	if d.Manifest.Schema != Schema {
		return d, fmt.Errorf("unsupported profile schema %d (supported: %d)", d.Manifest.Schema, Schema)
	}
	d.Packages.Official, err = readList(filepath.Join(dir, "packages", "official.txt"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return d, err
	}
	d.Packages.AUR, err = readList(filepath.Join(dir, "packages", "aur.txt"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return d, err
	}
	d.Packages.MachineSpecific, err = readList(filepath.Join(dir, "packages", "machine-specific.txt"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return d, err
	}
	return d, nil
}

func Save(dir string, d Data) error {
	d.Manifest.Schema = Schema
	d.Packages.Official = normalize(d.Packages.Official)
	d.Packages.AUR = normalize(d.Packages.AUR)
	d.Packages.MachineSpecific = normalize(d.Packages.MachineSpecific)
	if err := os.MkdirAll(filepath.Join(dir, "packages"), 0o755); err != nil {
		return err
	}
	b, err := toml.Marshal(d.Manifest)
	if err != nil {
		return err
	}
	writes := []struct {
		path string
		data []byte
	}{
		{filepath.Join(dir, "profile.toml"), b},
		{filepath.Join(dir, "packages", "official.txt"), []byte(joinList(d.Packages.Official))},
		{filepath.Join(dir, "packages", "aur.txt"), []byte(joinList(d.Packages.AUR))},
		{filepath.Join(dir, "packages", "machine-specific.txt"), []byte(joinList(d.Packages.MachineSpecific))},
	}
	for _, w := range writes {
		if err := atomicWrite(w.path, w.data); err != nil {
			return err
		}
	}
	return nil
}

func Validate(d Data) error {
	if d.Manifest.Schema != Schema {
		return fmt.Errorf("unsupported profile schema %d", d.Manifest.Schema)
	}
	if strings.TrimSpace(d.Manifest.Profile.Name) == "" {
		return errors.New("profile name is empty")
	}
	return nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".omarchy-blueprint-*.tmp")
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

func readList(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return normalize(strings.Split(string(b), "\n")), nil
}

func normalize(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

func joinList(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, "\n") + "\n"
}
