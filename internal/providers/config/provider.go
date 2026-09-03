package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/graeme/omarchy-blueprint/internal/content"
	"github.com/graeme/omarchy-blueprint/internal/model"
	"github.com/graeme/omarchy-blueprint/internal/profile"
)

// FileStatus describes how a managed file currently sits on the machine.
type FileStatus string

const (
	FileDefault     FileStatus = "default"
	FileCustomized  FileStatus = "customized"
	FileMissing     FileStatus = "missing"
	FileUnsupported FileStatus = "unsupported"
)

// Spec identifies one managed configuration file by relative path.
type Spec struct {
	ID   string `json:"id" toml:"id"`
	Path string `json:"path" toml:"path"`
}

// DefaultSpecs are the supported Omarchy Hyprland configuration files.
var DefaultSpecs = []Spec{
	{ID: "hypr.main", Path: "hypr/hyprland.lua"},
	{ID: "hypr.bindings", Path: "hypr/bindings.lua"},
	{ID: "hypr.looknfeel", Path: "hypr/looknfeel.lua"},
	{ID: "hypr.autostart", Path: "hypr/autostart.lua"},
}

// Provider captures customized Hyprland configuration files.
type Provider struct {
	UserRoot     string
	BaselineRoot string
	ProfileDir   string
	Specs        []Spec
}

// DetectedFile is the live machine state for one spec.
type DetectedFile struct {
	ID           string     `json:"id"`
	Path         string     `json:"path"`
	Status       FileStatus `json:"status"`
	Hash         string     `json:"hash,omitempty"`
	BaselineHash string     `json:"baseline_hash,omitempty"`
}

// State is the detected state of all managed files.
type State struct {
	Files []DetectedFile `json:"files"`
}

func (p Provider) specs() []Spec {
	if p.Specs != nil {
		return p.Specs
	}
	return DefaultSpecs
}

// Detect inspects the live filesystem without following symlinks.
func (p Provider) Detect() (State, error) {
	state := State{Files: make([]DetectedFile, 0, len(p.specs()))}
	for _, spec := range p.specs() {
		detected, err := p.detect(spec)
		if err != nil {
			return State{}, fmt.Errorf("detect config %s: %w", spec.ID, err)
		}
		state.Files = append(state.Files, detected)
	}
	return state, nil
}

func (p Provider) detect(spec Spec) (DetectedFile, error) {
	detected := DetectedFile{ID: spec.ID, Path: spec.Path}
	baselinePath := filepath.Join(p.BaselineRoot, filepath.FromSlash(spec.Path))
	baselineInfo, err := os.Lstat(baselinePath)
	if os.IsNotExist(err) {
		detected.Status = FileUnsupported
		return detected, nil
	}
	if err != nil {
		return detected, err
	}
	if err := rejectSpecial(baselineInfo, baselinePath); err != nil {
		return detected, err
	}
	baselineHash, err := content.HashRegularFile(baselinePath)
	if err != nil {
		return detected, fmt.Errorf("hash baseline: %w", err)
	}
	detected.BaselineHash = baselineHash
	userPath := filepath.Join(p.UserRoot, filepath.FromSlash(spec.Path))
	userInfo, err := os.Lstat(userPath)
	if os.IsNotExist(err) {
		detected.Status = FileMissing
		return detected, nil
	}
	if err != nil {
		return detected, err
	}
	if err := rejectSpecial(userInfo, userPath); err != nil {
		return detected, err
	}
	userHash, err := content.HashRegularFile(userPath)
	if err != nil {
		return detected, fmt.Errorf("hash user config: %w", err)
	}
	detected.Hash = userHash
	if userHash == baselineHash {
		detected.Status = FileDefault
	} else {
		detected.Status = FileCustomized
	}
	return detected, nil
}

func rejectSpecial(info os.FileInfo, path string) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path is a symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file: %s", path)
	}
	return nil
}

// Capture copies customized files (plus their baselines) into the profile
// using a staged directory swap so a failure cannot corrupt the profile.
func (p Provider) Capture() (profile.Configs, error) {
	state, err := p.Detect()
	if err != nil {
		return profile.Configs{}, err
	}
	if p.ProfileDir == "" {
		return profile.Configs{}, fmt.Errorf("profile directory is required to capture config")
	}
	parent := filepath.Join(p.ProfileDir, "config")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return profile.Configs{}, err
	}
	staging, err := os.MkdirTemp(parent, ".capture-*")
	if err != nil {
		return profile.Configs{}, err
	}
	defer os.RemoveAll(staging)
	configs := profile.Configs{Files: []profile.ConfigFile{}}
	for _, detected := range state.Files {
		if detected.Status != FileCustomized {
			continue
		}
		userPath := filepath.Join(p.UserRoot, filepath.FromSlash(detected.Path))
		baselinePath := filepath.Join(p.BaselineRoot, filepath.FromSlash(detected.Path))
		if err := copyFileInto(staging, "files", detected.Path, userPath); err != nil {
			return profile.Configs{}, fmt.Errorf("capture config %s: %w", detected.ID, err)
		}
		if err := copyFileInto(staging, "baseline", detected.Path, baselinePath); err != nil {
			return profile.Configs{}, fmt.Errorf("capture config %s: %w", detected.ID, err)
		}
		configs.Files = append(configs.Files, profile.ConfigFile{
			ID:           detected.ID,
			Path:         detected.Path,
			Hash:         detected.Hash,
			BaselineHash: detected.BaselineHash,
		})
	}
	sortConfigFiles(configs.Files)
	if err := swapDir(staging, filepath.Join(parent, "files")); err != nil {
		return profile.Configs{}, err
	}
	if err := swapDir(staging, filepath.Join(parent, "baseline")); err != nil {
		return profile.Configs{}, err
	}
	return configs, nil
}

func copyFileInto(staging, kind, relPath, source string) error {
	target := filepath.Join(staging, kind, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, info, err := content.OpenRegularFile(source)
	if err != nil {
		return err
	}
	defer f.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, f); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// swapDir atomically replaces dir with staging/<base name>.
func swapDir(staging, dir string) error {
	name := filepath.Base(dir)
	staged := filepath.Join(staging, name)
	if err := os.MkdirAll(staged, 0o755); err != nil {
		return err
	}
	old := filepath.Join(filepath.Dir(dir), "."+name+"-previous")
	_ = os.RemoveAll(old)
	if _, err := os.Lstat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			return err
		}
	}
	if err := os.Rename(staged, dir); err != nil {
		_ = os.Rename(old, dir)
		return err
	}
	return os.RemoveAll(old)
}

func sortConfigFiles(files []profile.ConfigFile) {
	sort.Slice(files, func(i, j int) bool { return files[i].ID < files[j].ID })
}

// DiffConfigs compares the previous captured configuration with a new capture.
func DiffConfigs(previous, next profile.Configs) []model.Change {
	prevMap := map[string]profile.ConfigFile{}
	for _, f := range previous.Files {
		prevMap[f.ID] = f
	}
	var changes []model.Change
	for _, f := range next.Files {
		if p, ok := prevMap[f.ID]; ok {
			delete(prevMap, f.ID)
			if p.Hash != f.Hash {
				changes = append(changes, model.Change{Type: model.ChangeModify, Provider: "config", Kind: "config", Name: f.ID, Summary: "~ config " + f.Path + " differs"})
			}
			continue
		}
		changes = append(changes, model.Change{Type: model.ChangeAdd, Provider: "config", Kind: "config", Name: f.ID, Summary: "+ config " + f.Path + " customized"})
	}
	for _, f := range prevMap {
		changes = append(changes, model.Change{Type: model.ChangeRemove, Provider: "config", Kind: "config", Name: f.ID, Summary: "- config " + f.Path + " customization removed"})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes
}

// Diff compares the saved profile configuration with the live machine state.
func Diff(saved profile.Configs, current State) []model.Change {	savedMap := map[string]profile.ConfigFile{}
	for _, f := range saved.Files {
		savedMap[f.ID] = f
	}
	changes := make([]model.Change, 0, len(current.Files))
	for _, f := range current.Files {
		switch f.Status {
		case FileCustomized:
			if s, ok := savedMap[f.ID]; ok {
				delete(savedMap, f.ID)
				if s.Hash != f.Hash {
					changes = append(changes, change(model.ChangeModify, f, fmt.Sprintf("~ config %s differs", f.Path)))
				}
				continue
			}
			changes = append(changes, change(model.ChangeAdd, f, "+ config "+f.Path+" customized"))
		case FileDefault, FileMissing, FileUnsupported:
			if _, ok := savedMap[f.ID]; ok {
				delete(savedMap, f.ID)
				changes = append(changes, change(model.ChangeRemove, f, "- config "+f.Path+" customization removed"))
			}
		}
	}
	for _, f := range savedMap {
		changes = append(changes, model.Change{Type: model.ChangeRemove, Provider: "config", Kind: "config", Name: f.ID, Summary: "- config " + f.Path + " customization removed"})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes
}

// Verify checks that every saved customization exists with the desired hash.
// Extra customization on the machine is drift, not verification failure.
func Verify(saved profile.Configs, current State) model.VerificationResult {
	currentMap := map[string]DetectedFile{}
	for _, f := range current.Files {
		currentMap[f.ID] = f
	}
	var missing []string
	for _, s := range saved.Files {
		f, ok := currentMap[s.ID]
		if !ok || f.Status != FileCustomized || f.Hash != s.Hash {
			missing = append(missing, "config:"+s.Path)
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}

func change(kind model.ChangeType, f DetectedFile, summary string) model.Change {
	return model.Change{Type: kind, Provider: "config", Kind: "config", Name: f.ID, Summary: summary}
}
