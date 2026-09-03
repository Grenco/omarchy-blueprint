package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type Provider struct {
	Runner              command.Runner
	UserDir, ProfileDir string
}
type catalogItem struct {
	ID         string `json:"id"`
	Enabled    bool   `json:"enabled"`
	FirstParty bool   `json:"firstParty"`
	CanDisable bool   `json:"canDisable"`
	ClonedFrom string `json:"clonedFrom"`
}

func (p Provider) Detect(ctx context.Context) (profile.Plugins, error) {
	out, err := p.Runner.Run(ctx, "omarchy", "plugin", "list", "--json")
	if err != nil {
		return profile.Plugins{}, fmt.Errorf("detect Omarchy plugins: %w", err)
	}
	var catalog []catalogItem
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		return profile.Plugins{}, fmt.Errorf("parse Omarchy plugin catalog: %w", err)
	}
	state := profile.Plugins{}
	for _, item := range catalog {
		if item.FirstParty {
			if item.CanDisable {
				state.Items = append(state.Items, profile.Plugin{ID: item.ID, Source: "builtin", Enabled: item.Enabled})
			}
			continue
		}
		path := filepath.Join(p.UserDir, item.ID)
		plugin, err := p.detectUser(ctx, item, path)
		if err != nil {
			return state, err
		}
		state.Items = append(state.Items, plugin)
	}
	sort.Slice(state.Items, func(i, j int) bool { return state.Items[i].ID < state.Items[j].ID })
	return state, nil
}

func (p Provider) Capture(ctx context.Context) (profile.Plugins, error) {
	state, err := p.Detect(ctx)
	if err != nil {
		return state, err
	}
	if p.ProfileDir == "" {
		return state, fmt.Errorf("profile directory is required to capture plugins")
	}
	parent := filepath.Join(p.ProfileDir, "plugins")
	if err := os.MkdirAll(parent, 0755); err != nil {
		return state, err
	}
	stage, err := os.MkdirTemp(parent, ".local-capture-*")
	if err != nil {
		return state, err
	}
	defer os.RemoveAll(stage)
	for _, item := range state.Items {
		if item.Source != "local" {
			continue
		}
		source, err := filepath.EvalSymlinks(filepath.Join(p.UserDir, item.ID))
		if err != nil {
			return state, err
		}
		if err := copyTree(source, filepath.Join(stage, item.ID)); err != nil {
			return state, fmt.Errorf("capture plugin %q: %w", item.ID, err)
		}
	}
	dest, old := filepath.Join(parent, "local"), filepath.Join(parent, ".local-previous")
	_ = os.RemoveAll(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return state, err
		}
	}
	if err := os.Rename(stage, dest); err != nil {
		_ = os.Rename(old, dest)
		return state, err
	}
	_ = os.RemoveAll(old)
	return state, nil
}

func (p Provider) detectUser(ctx context.Context, item catalogItem, path string) (profile.Plugin, error) {
	result := profile.Plugin{ID: item.ID, ClonedFrom: item.ClonedFrom, Enabled: item.Enabled}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return result, fmt.Errorf("resolve plugin %q: %w", item.ID, err)
	}
	if isDir(filepath.Join(resolved, ".git")) {
		remote, re := p.Runner.Run(ctx, "git", "-C", resolved, "remote", "get-url", "origin")
		rev, ve := p.Runner.Run(ctx, "git", "-C", resolved, "rev-parse", "HEAD")
		dirty, se := p.Runner.Run(ctx, "git", "-C", resolved, "status", "--porcelain", "--untracked-files=all")
		if re == nil && ve == nil && se == nil && strings.TrimSpace(dirty) == "" {
			result.Source = "git"
			result.URL = sanitizeURL(strings.TrimSpace(remote))
			result.Revision = strings.TrimSpace(rev)
			return result, nil
		}
	}
	hash, err := hashTree(resolved)
	if err != nil {
		return result, fmt.Errorf("hash plugin %q: %w", item.ID, err)
	}
	result.Source = "local"
	result.Hash = hash
	return result, nil
}

func Diff(saved, current profile.Plugins) []model.Change {
	have := pluginMap(current.Items)
	var out []model.Change
	for _, want := range saved.Items {
		got, ok := have[want.ID]
		if !ok {
			out = append(out, change(want.ID, "- plugin "+want.ID+" ("+want.Source+")"))
			continue
		}
		if !equivalent(want, got) {
			out = append(out, change(want.ID, fmt.Sprintf("~ plugin %s differs (%s → %s)", want.ID, want.Source, got.Source)))
		} else if got.Enabled != want.Enabled {
			out = append(out, change(want.ID, fmt.Sprintf("~ plugin %s enabled: %t → %t", want.ID, got.Enabled, want.Enabled)))
		}
	}
	for _, got := range current.Items {
		if _, ok := pluginMap(saved.Items)[got.ID]; !ok && got.Source != "builtin" {
			out = append(out, change(got.ID, "+ plugin "+got.ID+" ("+got.Source+")"))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (p Provider) Plan(saved, current profile.Plugins, schema int, from, to string) model.RestorePlan {
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	have := pluginMap(current.Items)
	wantMap := pluginMap(saved.Items)
	for _, want := range saved.Items {
		got, ok := have[want.ID]
		lastDependency := ""
		if !safeID(want.ID) {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + want.ID, Reason: "unsafe plugin identifier"})
			continue
		}
		if ok && !equivalent(want, got) {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + want.ID, Reason: "existing plugin differs; overwrite disabled"})
			continue
		}
		if !ok {
			switch want.Source {
			case "builtin", "":
				plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + want.ID, Reason: "first-party plugin unavailable in this Omarchy version"})
				continue
			case "git":
				if !validRevision(want.Revision) || want.URL == "" {
					plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + want.ID, Reason: "invalid Git provenance"})
					continue
				}
				install := op("install", want.ID, []string{"omarchy", "plugin", "add", want.URL, "--yes"}, model.RiskHigh)
				pin := op("pin", want.ID, []string{"git", "-C", filepath.Join(p.UserDir, want.ID), "checkout", "--detach", want.Revision}, model.RiskLow)
				pin.DependsOn = []string{install.ID}
				validate := op("validate", want.ID, []string{"omarchy", "plugin", "validate", filepath.Join(p.UserDir, want.ID)}, model.RiskLow)
				validate.DependsOn = []string{pin.ID}
				rescan := op("rescan", want.ID, []string{"omarchy-shell", "shell", "rescanPlugins"}, model.RiskLow)
				rescan.DependsOn = []string{validate.ID}
				plan.Operations = append(plan.Operations, install, pin, validate, rescan)
				lastDependency = rescan.ID
			case "local":
				source := filepath.Join(p.ProfileDir, "plugins", "local", want.ID)
				validate := op("validate", want.ID, []string{"omarchy", "plugin", "validate", source}, model.RiskLow)
				copy := model.Operation{ID: "plugins.copy." + want.ID, Provider: "plugins", Action: "copy", Resource: "plugin:" + want.ID, Items: []string{want.ID}, Copy: &model.Copy{Source: source, Destination: filepath.Join(p.UserDir, want.ID)}, DependsOn: []string{validate.ID}, Risk: model.RiskHigh, Reversible: true}
				rescan := op("rescan", want.ID, []string{"omarchy-shell", "shell", "rescanPlugins"}, model.RiskLow)
				rescan.DependsOn = []string{copy.ID}
				plan.Operations = append(plan.Operations, validate, copy, rescan)
				lastDependency = rescan.ID
			default:
				continue
			}
		}
		if want.Enabled && (!ok || !got.Enabled) {
			enable := op("enable", want.ID, []string{"omarchy", "plugin", "enable", want.ID}, model.RiskHigh)
			if lastDependency != "" {
				enable.DependsOn = []string{lastDependency}
			}
			plan.Operations = append(plan.Operations, enable)
		} else if ok && !want.Enabled && got.Enabled {
			plan.Operations = append(plan.Operations, op("disable", want.ID, []string{"omarchy", "plugin", "disable", want.ID}, model.RiskLow))
		}
	}
	for _, got := range current.Items {
		if _, ok := wantMap[got.ID]; !ok && got.Source != "builtin" {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + got.ID, Reason: "additional plugin left installed; removal disabled"})
		}
	}
	return plan
}

func Verify(saved, current profile.Plugins) model.VerificationResult {
	have := pluginMap(current.Items)
	var missing []string
	for _, want := range saved.Items {
		got, ok := have[want.ID]
		if !ok || !equivalent(want, got) || got.Enabled != want.Enabled {
			missing = append(missing, "plugin:"+want.ID)
		}
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}
func equivalent(a, b profile.Plugin) bool {
	as, bs := a.Source, b.Source
	if as == "" {
		as = "builtin"
	}
	if bs == "" {
		bs = "builtin"
	}
	if as != bs {
		return false
	}
	switch as {
	case "git":
		return a.URL == b.URL && a.Revision == b.Revision
	case "local":
		return a.Hash == b.Hash && a.ClonedFrom == b.ClonedFrom
	default:
		return true
	}
}
func op(action, id string, argv []string, risk model.Risk) model.Operation {
	return model.Operation{ID: "plugins." + action + "." + id, Provider: "plugins", Action: action, Resource: "plugin:" + id, Items: []string{id}, Command: argv, Risk: risk, Reversible: action != "install"}
}
func change(id, summary string) model.Change {
	return model.Change{Type: model.ChangeAdd, Provider: "plugins", Kind: "plugin", Name: id, Summary: summary}
}
func pluginMap(items []profile.Plugin) map[string]profile.Plugin {
	out := map[string]profile.Plugin{}
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}
func sanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.User != nil && u.Scheme != "" {
		u.User = nil
		return u.String()
	}
	return raw
}
func validRevision(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
func safeID(id string) bool {
	return id != "" && id != "." && id != ".." && !filepath.IsAbs(id) && filepath.Base(id) == id
}
func isDir(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }
func hashTree(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, we error) error {
		if we != nil {
			return we
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symlink: %s", rel)
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%o\x00", filepath.ToSlash(rel), info.Mode().Perm())
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, ce := io.Copy(h, f)
			cl := f.Close()
			if ce != nil {
				return ce
			}
			return cl
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func copyTree(source, dest string) error {
	return filepath.WalkDir(source, func(path string, e os.DirEntry, we error) error {
		if we != nil {
			return we
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symlink: %s", rel)
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if e.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file: %s", rel)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			in.Close()
			return err
		}
		_, ce := io.Copy(out, in)
		ie, oe := in.Close(), out.Close()
		if ce != nil {
			return ce
		}
		if ie != nil {
			return ie
		}
		return oe
	})
}
