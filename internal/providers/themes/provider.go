package themes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type Provider struct {
	Runner                          command.Runner
	BuiltinDir, UserDir, ProfileDir string
}

func (p Provider) Detect(ctx context.Context) (profile.Themes, error) {
	out, err := p.Runner.Run(ctx, "omarchy", "theme", "current")
	if err != nil {
		return profile.Themes{}, fmt.Errorf("detect current Omarchy theme: %w", err)
	}
	current := slug(strings.TrimSpace(out))
	if current == "" || current == "unknown" {
		return profile.Themes{}, fmt.Errorf("Omarchy did not report an active theme")
	}
	state := profile.Themes{Current: current}
	entries, err := os.ReadDir(p.UserDir)
	if err != nil && !os.IsNotExist(err) {
		return state, fmt.Errorf("list user themes: %w", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !safeID(entry.Name()) {
			return state, fmt.Errorf("theme directory has unsafe name %q", entry.Name())
		}
		path := filepath.Join(p.UserDir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return state, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			path, err = filepath.EvalSymlinks(path)
			if err != nil {
				return state, fmt.Errorf("resolve theme %q: %w", entry.Name(), err)
			}
			info, err = os.Stat(path)
			if err != nil {
				return state, err
			}
		}
		if !info.IsDir() {
			continue
		}
		theme, err := p.detectUserTheme(ctx, entry.Name(), path)
		if err != nil {
			return state, err
		}
		theme.Enabled = theme.ID == current
		state.Items = append(state.Items, theme)
	}
	if !hasTheme(state.Items, current) {
		typeName := "unknown"
		if isDir(filepath.Join(p.BuiltinDir, current)) {
			typeName = "builtin"
		}
		state.Items = append(state.Items, profile.Theme{ID: current, Type: typeName, Enabled: true})
	}
	sortThemes(state.Items)
	state.Source = activeSource(state)
	return state, nil
}

func (p Provider) Capture(ctx context.Context) (profile.Themes, error) {
	state, err := p.Detect(ctx)
	if err != nil {
		return state, err
	}
	if p.ProfileDir == "" {
		return state, fmt.Errorf("profile directory is required to capture themes")
	}
	parent := filepath.Join(p.ProfileDir, "themes")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return state, err
	}
	staging, err := os.MkdirTemp(parent, ".local-capture-*")
	if err != nil {
		return state, err
	}
	defer os.RemoveAll(staging)
	for i := range state.Items {
		item := &state.Items[i]
		if item.Type != "local" && item.Type != "overlay" {
			continue
		}
		source, err := filepath.EvalSymlinks(filepath.Join(p.UserDir, item.ID))
		if err != nil {
			return state, fmt.Errorf("resolve theme %q: %w", item.ID, err)
		}
		if err := copySnapshot(source, filepath.Join(staging, item.ID)); err != nil {
			return state, fmt.Errorf("capture theme %q: %w", item.ID, err)
		}
	}
	destination, old := filepath.Join(parent, "local"), filepath.Join(parent, ".local-previous")
	_ = os.RemoveAll(old)
	if _, err := os.Stat(destination); err == nil {
		if err := os.Rename(destination, old); err != nil {
			return state, err
		}
	}
	if err := os.Rename(staging, destination); err != nil {
		_ = os.Rename(old, destination)
		return state, err
	}
	_ = os.RemoveAll(old)
	return state, nil
}

func (p Provider) detectUserTheme(ctx context.Context, id, path string) (profile.Theme, error) {
	if isDir(filepath.Join(path, ".git")) {
		url, urlErr := p.Runner.Run(ctx, "git", "-C", path, "remote", "get-url", "origin")
		revision, revErr := p.Runner.Run(ctx, "git", "-C", path, "rev-parse", "HEAD")
		dirty, statusErr := p.Runner.Run(ctx, "git", "-C", path, "status", "--porcelain", "--untracked-files=all")
		if urlErr == nil && revErr == nil && statusErr == nil && strings.TrimSpace(dirty) == "" {
			return profile.Theme{ID: id, Type: "git", URL: sanitizeURL(strings.TrimSpace(url)), Revision: strings.TrimSpace(revision)}, nil
		}
	}
	hash, err := hashTree(path)
	if err != nil {
		return profile.Theme{}, fmt.Errorf("hash theme %q: %w", id, err)
	}
	typeName := "local"
	if isDir(filepath.Join(p.BuiltinDir, id)) {
		typeName = "overlay"
	}
	return profile.Theme{ID: id, Type: typeName, Hash: hash}, nil
}

func sanitizeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil && parsed.User != nil && parsed.Scheme != "" {
		parsed.User = nil
		return parsed.String()
	}
	return raw
}

func installID(raw string) string {
	repoPath := raw
	if !strings.Contains(repoPath, "://") {
		colon, slash := strings.IndexByte(repoPath, ':'), strings.IndexByte(repoPath, '/')
		if colon >= 0 && (slash < 0 || colon < slash) {
			repoPath = repoPath[colon+1:]
		}
	}
	name := strings.TrimSuffix(filepath.Base(repoPath), ".git")
	name = strings.TrimPrefix(strings.ToLower(name), "omarchy-")
	return strings.TrimSuffix(name, "-theme")
}

func Diff(saved, current profile.Themes) []model.Change {
	saved = legacy(saved)
	want, have := themeMap(saved.Items), themeMap(current.Items)
	var changes []model.Change
	for id, desired := range want {
		actual, ok := have[id]
		if !ok {
			if desired.Type != "builtin" {
				changes = append(changes, change(model.ChangeRemove, "theme", id, "- theme "+id+" ("+desired.Type+")"))
			}
		} else if !equivalent(desired, actual) {
			changes = append(changes, change(model.ChangeAdd, "theme", id, fmt.Sprintf("~ theme %s (%s) differs from profile (%s)", id, actual.Type, desired.Type)))
		}
	}
	for id, actual := range have {
		if _, ok := want[id]; !ok {
			if actual.Type != "builtin" {
				changes = append(changes, change(model.ChangeAdd, "theme", id, "+ theme "+id+" ("+actual.Type+")"))
			}
		}
	}
	if saved.Current != current.Current {
		changes = append(changes, change(model.ChangeAdd, "active", current.Current, fmt.Sprintf("~ active theme %s → %s", saved.Current, current.Current)))
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Kind+changes[i].Name < changes[j].Kind+changes[j].Name })
	return changes
}

func (p Provider) Plan(saved, current profile.Themes, schema int, from, to string) model.RestorePlan {
	saved = legacy(saved)
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	have := themeMap(current.Items)
	needsActivation := saved.Current != "" && saved.Current != current.Current
	for _, desired := range saved.Items {
		if !safeID(desired.ID) {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "themes", Resource: "theme:" + desired.ID, Reason: "unsafe theme identifier"})
			continue
		}
		actual, present := have[desired.ID]
		if present {
			if !equivalent(desired, actual) {
				plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "themes", Resource: "theme:" + desired.ID, Reason: "existing theme differs; overwrite disabled"})
			}
			continue
		}
		switch desired.Type {
		case "builtin":
			if !isDir(filepath.Join(p.BuiltinDir, desired.ID)) {
				plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "themes", Resource: "theme:" + desired.ID, Reason: "built-in theme unavailable in this Omarchy version"})
			}
		case "git":
			if desired.URL == "" || !validRevision(desired.Revision) {
				plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "themes", Resource: "theme:" + desired.ID, Reason: "invalid Git provenance"})
				continue
			}
			if installID(desired.URL) != desired.ID {
				plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "themes", Resource: "theme:" + desired.ID, Reason: "Git URL installs under a different theme name"})
				continue
			}
			plan.Operations = append(plan.Operations, operation("install", desired.ID, []string{"omarchy", "theme", "install", desired.URL}), operation("pin", desired.ID, []string{"git", "-C", filepath.Join(p.UserDir, desired.ID), "checkout", "--detach", desired.Revision}))
			needsActivation = true
		case "local", "overlay":
			plan.Operations = append(plan.Operations, model.Operation{ID: "themes.copy." + desired.ID, Provider: "themes", Action: "copy", Resource: "theme:" + desired.ID, Items: []string{desired.ID}, Copy: &model.Copy{Source: filepath.Join(p.ProfileDir, "themes", "local", desired.ID), Destination: filepath.Join(p.UserDir, desired.ID)}, Risk: model.RiskLow, Reversible: true})
			if desired.ID == saved.Current {
				needsActivation = true
			}
		default:
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "themes", Resource: "theme:" + desired.ID, Reason: "unsupported theme source " + desired.Type})
		}
	}
	want := themeMap(saved.Items)
	for _, actual := range current.Items {
		if _, managed := want[actual.ID]; !managed && actual.Type != "builtin" {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "themes", Resource: "theme:" + actual.ID, Reason: "additional theme left installed; removal disabled"})
		}
	}
	if saved.Current != "" && needsActivation {
		plan.Operations = append(plan.Operations, operation("activate", saved.Current, []string{"omarchy", "theme", "set", saved.Current}))
	}
	return plan
}

func Verify(saved, current profile.Themes) model.VerificationResult {
	saved = legacy(saved)
	have := themeMap(current.Items)
	var missing []string
	for _, desired := range saved.Items {
		actual, ok := have[desired.ID]
		if !ok || !equivalent(desired, actual) {
			missing = append(missing, "theme:"+desired.ID)
		}
	}
	if saved.Current != "" && saved.Current != current.Current {
		missing = append(missing, "active-theme:"+saved.Current)
	}
	sort.Strings(missing)
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}

func legacy(state profile.Themes) profile.Themes {
	if len(state.Items) == 0 && state.Current != "" {
		state.Items = []profile.Theme{{ID: state.Current, Type: state.Source, Enabled: true}}
	}
	return state
}
func equivalent(a, b profile.Theme) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case "git":
		return a.URL == b.URL && a.Revision == b.Revision
	case "local", "overlay":
		return a.Hash == b.Hash
	default:
		return true
	}
}

func hashTree(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symlink: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%o\x00", filepath.ToSlash(rel), info.Mode().Perm())
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(h, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copySnapshot(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symlink: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
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
		_, copyErr := io.Copy(out, in)
		inErr, outErr := in.Close(), out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		return outErr
	})
}

func operation(action, id string, argv []string) model.Operation {
	return model.Operation{ID: "themes." + action + "." + id, Provider: "themes", Action: action, Resource: "theme:" + id, Items: []string{id}, Command: argv, Risk: model.RiskLow, Reversible: action != "install"}
}
func change(kind model.ChangeType, changeKind, id, summary string) model.Change {
	return model.Change{Type: kind, Provider: "themes", Kind: changeKind, Name: id, Summary: summary}
}
func themeMap(items []profile.Theme) map[string]profile.Theme {
	out := map[string]profile.Theme{}
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}
func hasTheme(items []profile.Theme, id string) bool { _, ok := themeMap(items)[id]; return ok }
func sortThemes(items []profile.Theme) {
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
}
func isDir(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }
func activeSource(state profile.Themes) string {
	for _, item := range state.Items {
		if item.ID == state.Current {
			return item.Type
		}
	}
	return "unknown"
}

func safeID(id string) bool {
	if id == "" || id[0] == '.' || id[0] == '-' {
		return false
	}
	for _, r := range id {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._+-", r) {
			return false
		}
	}
	return true
}

func validRevision(revision string) bool {
	if len(revision) != 40 && len(revision) != 64 {
		return false
	}
	for _, r := range revision {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
