package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/sensitive"
)

var (
	sensitiveContentInspection func()
)

const MaxAutomaticConfigFileSize int64 = 16 << 20
const MaxMergeableTextSize int64 = 4 << 20

type PolicyReason string

const (
	PolicyAllowed             PolicyReason = "allowed"
	PolicyExcluded            PolicyReason = "excluded"
	PolicyOmarchyUpdateBackup PolicyReason = "omarchy-update-backup"
	PolicyVolatile            PolicyReason = "volatile"
	PolicySensitive           PolicyReason = "sensitive"
	PolicyOversized           PolicyReason = "oversized"
)

type PolicyDecision struct{ Reason PolicyReason }

// NormalizeConfigPolicyPath converts ergonomic Config policy input into the
// canonical HOME-relative namespace used by ConfigFile.Path.
func NormalizeConfigPolicyPath(input string) (string, error) {
	input = strings.TrimSpace(strings.ReplaceAll(input, "\\", "/"))
	if strings.HasPrefix(input, "~/") && !strings.HasPrefix(input, "~/.config/") {
		input = strings.TrimPrefix(input, "~/")
	}
	if strings.HasPrefix(input, ".") && !strings.HasPrefix(input, ".config") {
		for _, spec := range DefaultHomeConfigSpecs() {
			if input == spec.Path {
				return profile.NormalizeConfigPath(input)
			}
		}
		return "", fmt.Errorf("invalid config exclusion path %q", input)
	}
	if input == "~/.config" || input == ".config" {
		return "", fmt.Errorf("config exclusion path must name an entry below ~/.config")
	}
	input = strings.TrimPrefix(input, "~/.config/")
	input = strings.TrimPrefix(input, ".config/")
	if input == "" || strings.HasPrefix(input, "~") || strings.HasPrefix(input, "/") {
		return "", fmt.Errorf("invalid config exclusion path %q", input)
	}
	input = path.Clean(input)
	if input == "." || input == ".." || strings.HasPrefix(input, "../") || input == ".ssh" || strings.HasPrefix(input, ".ssh/") {
		return "", fmt.Errorf("invalid config exclusion path %q", input)
	}
	return profile.NormalizeConfigPath(".config/" + input)
}

func NormalizeExclusionPath(input string) (string, error) { return NormalizeConfigPolicyPath(input) }

// AddExclusion returns copied config metadata with path excluded and all saved
// files and tombstones below that path removed. It never touches live config.
func AddExclusion(saved profile.Configs, path string) (profile.Configs, []string, error) {
	path, err := NormalizeExclusionPath(path)
	if err != nil {
		return saved, nil, err
	}
	result := profile.Configs{Files: append([]profile.ConfigFile{}, saved.Files...), Deletes: append([]profile.ConfigDelete{}, saved.Deletes...), Included: append([]string{}, saved.Included...), Excluded: append([]string{}, saved.Excluded...)}
	for i, exclusion := range result.Excluded {
		result.Excluded[i], err = NormalizeExclusionPath(exclusion)
		if err != nil {
			return saved, nil, err
		}
	}
	if !containsPath(result.Excluded, path) {
		result.Excluded = append(result.Excluded, path)
	}
	sort.Strings(result.Excluded)
	result.Excluded = uniquePaths(result.Excluded)
	var removed []string
	result.Files, removed = pruneFiles(result.Files, path, removed)
	result.Deletes, removed = pruneDeletes(result.Deletes, path, removed)
	result.Included = prunePolicyPaths(result.Included, path)
	return result, removed, nil
}

// RemoveExclusion returns copied config metadata with path no longer excluded.
func RemoveExclusion(saved profile.Configs, path string) (profile.Configs, bool, error) {
	path, err := NormalizeExclusionPath(path)
	if err != nil {
		return saved, false, err
	}
	result := profile.Configs{Files: append([]profile.ConfigFile{}, saved.Files...), Deletes: append([]profile.ConfigDelete{}, saved.Deletes...), Included: append([]string{}, saved.Included...), Excluded: append([]string{}, saved.Excluded...)}
	result.Excluded = result.Excluded[:0]
	removed := false
	for _, exclusion := range saved.Excluded {
		exclusion, err = NormalizeExclusionPath(exclusion)
		if err != nil {
			return saved, false, err
		}
		if exclusion == path {
			removed = true
			continue
		}
		result.Excluded = append(result.Excluded, exclusion)
	}
	sort.Strings(result.Excluded)
	result.Excluded = uniquePaths(result.Excluded)
	return result, removed, nil
}

// AddInclusion persists a safe, canonical discovery root. Exclusions remain
// authoritative so callers cannot use inclusion to resurrect excluded state.
func AddInclusion(saved profile.Configs, input string) (profile.Configs, bool, error) {
	path, err := NormalizeExclusionPath(input)
	if err != nil {
		return saved, false, err
	}
	for _, excluded := range saved.Excluded {
		if IsExcludedConfigPath(path, []string{excluded}) {
			return saved, false, fmt.Errorf("config include %q is below excluded path %q; remove the exclusion first", path, excluded)
		}
	}
	result := saved
	result.Included = append([]string{}, saved.Included...)
	if containsPath(result.Included, path) {
		return result, false, nil
	}
	result.Included = append(result.Included, path)
	sort.Strings(result.Included)
	return result, true, nil
}

// ClearPolicy removes only explicit policy records for the canonical path.
// Ancestor and descendant records deliberately remain in effect.
func ClearPolicy(saved profile.Configs, input string) (profile.Configs, bool, error) {
	path, err := NormalizeConfigPolicyPath(input)
	if err != nil {
		return saved, false, err
	}
	result := saved
	result.Included = make([]string, 0, len(saved.Included))
	result.Excluded = make([]string, 0, len(saved.Excluded))
	changed := false
	for _, item := range saved.Included {
		item, err = NormalizeConfigPolicyPath(item)
		if err != nil {
			return saved, false, err
		}
		if item == path {
			changed = true
			continue
		}
		result.Included = append(result.Included, item)
	}
	for _, item := range saved.Excluded {
		item, err = NormalizeConfigPolicyPath(item)
		if err != nil {
			return saved, false, err
		}
		if item == path {
			changed = true
			continue
		}
		result.Excluded = append(result.Excluded, item)
	}
	sort.Strings(result.Included)
	sort.Strings(result.Excluded)
	result.Included = uniquePaths(result.Included)
	result.Excluded = uniquePaths(result.Excluded)
	return result, changed, nil
}

func prunePolicyPaths(paths []string, parent string) []string {
	result := paths[:0]
	for _, item := range paths {
		if !IsExcludedConfigPath(item, []string{parent}) {
			result = append(result, item)
		}
	}
	return result
}

func containsPath(paths []string, want string) bool {
	for _, item := range paths {
		if item == want {
			return true
		}
	}
	return false
}

func uniquePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	result := paths[:1]
	for _, item := range paths[1:] {
		if item != result[len(result)-1] {
			result = append(result, item)
		}
	}
	return result
}

func pruneFiles(files []profile.ConfigFile, excluded string, removed []string) ([]profile.ConfigFile, []string) {
	result := files[:0]
	for _, file := range files {
		if IsExcludedConfigPath(file.Path, []string{excluded}) {
			removed = append(removed, file.Path)
			continue
		}
		result = append(result, file)
	}
	return result, removed
}

func pruneDeletes(deletes []profile.ConfigDelete, excluded string, removed []string) ([]profile.ConfigDelete, []string) {
	result := deletes[:0]
	for _, deletion := range deletes {
		if IsExcludedConfigPath(deletion.Path, []string{excluded}) {
			removed = append(removed, deletion.Path)
			continue
		}
		result = append(result, deletion)
	}
	return result, removed
}

func IsOmarchyUpdateBackupName(name string) bool {
	i := strings.Index(name, ".bak.")
	return i > 0 && i+len(".bak.") < len(name)
}
func IsOmarchySetupBackupName(name string) bool {
	i := strings.Index(name, ".backup-")
	return i > 0 && i+len(".backup-") < len(name)
}
func IsBlueprintBackupName(name string) bool {
	i := strings.LastIndex(name, ".omarchy-blueprint-backup-")
	if !strings.HasPrefix(name, ".") || i <= 1 || i+len(".omarchy-blueprint-backup-") == len(name) {
		return false
	}
	for _, r := range name[i+len(".omarchy-blueprint-backup-"):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func IsBackupArtifactName(name string, directory bool) bool {
	lower := strings.ToLower(name)
	if IsBlueprintBackupName(name) {
		return true
	}
	if directory {
		return lower == "backup" || lower == "backups" || strings.HasSuffix(lower, ".bak") || strings.HasSuffix(lower, ".backup") || strings.HasSuffix(lower, "-backup") || strings.HasSuffix(lower, "-backups") || strings.HasSuffix(lower, "_backup") || strings.HasSuffix(lower, "_backups")
	}
	return strings.HasSuffix(lower, ".bak") || strings.Contains(lower, ".bak.") || strings.HasSuffix(lower, ".backup") || strings.Contains(lower, ".backup-") || strings.HasSuffix(lower, ".orig") || strings.HasSuffix(name, "~")
}
func IsExcludedConfigPath(path string, excluded []string) bool {
	path, err := profile.NormalizeConfigPath(path)
	if err != nil {
		return false
	}
	canonical := path
	if !strings.HasPrefix(canonical, ".config/") {
		canonical = ".config/" + canonical
	}
	for _, item := range excluded {
		item, err = profile.NormalizeConfigPath(item)
		if err == nil && (path == item || strings.HasPrefix(path, item+"/") || canonical == item || strings.HasPrefix(canonical, item+"/")) {
			return true
		}
	}
	return false
}
func ClassifyConfigPolicy(path string, info os.FileInfo, excluded []string) PolicyDecision {
	path, err := profile.NormalizeConfigPath(path)
	if err != nil {
		return PolicyDecision{PolicyExcluded}
	}
	name := filepath.Base(path)
	if IsBackupArtifactName(name, info.IsDir()) {
		return PolicyDecision{PolicyOmarchyUpdateBackup}
	}
	if IsExcludedConfigPath(path, excluded) {
		return PolicyDecision{PolicyExcluded}
	}
	if sensitiveConfigPath(path) {
		return PolicyDecision{PolicySensitive}
	}
	if !info.IsDir() && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return PolicyDecision{PolicyVolatile}
	}
	lower := strings.ToLower(path)
	if runtimeConfigPath(lower) || strings.HasSuffix(lower, ".pid") || strings.HasSuffix(lower, ".lock") || strings.HasSuffix(lower, ".log") || strings.HasSuffix(lower, "singletonlock") || strings.HasPrefix(strings.ToLower(name), "cached_") {
		return PolicyDecision{PolicyVolatile}
	}
	if strings.Contains(lower, "credential") || strings.Contains(lower, "private_key") || strings.Contains(lower, "secret") {
		return PolicyDecision{PolicySensitive}
	}
	if info.Size() > MaxAutomaticConfigFileSize {
		return PolicyDecision{PolicyOversized}
	}
	return PolicyDecision{PolicyAllowed}
}

func runtimeConfigPath(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if isRuntimeComponent(part) {
			return true
		}
	}
	return false
}

func sensitiveConfigPath(path string) bool {
	for _, exact := range []string{".config/gh/hosts.yml", ".config/rclone/rclone.conf", ".config/sops/age/keys.txt", ".config/containers/auth.json"} {
		if path == exact {
			return true
		}
	}
	if strings.HasPrefix(path, ".config/gcloud/") {
		return true
	}
	switch strings.ToLower(filepath.Base(path)) {
	case "login data", "logins.json", "key4.db", "cookies", "restore_token", "auth_token", "access_token":
		return true
	}
	return strings.HasSuffix(strings.ToLower(path), ".psk")
}

// hasSensitiveContent rejects high-confidence credential material before it is
// persisted in a profile, even when its filename looks harmless.
func hasSensitiveContent(path string) (bool, error) {
	if sensitiveContentInspection != nil {
		sensitiveContentInspection()
	}
	result, err := sensitive.ScanRegularFile(path, MaxAutomaticConfigFileSize)
	return result.Sensitive, err
}
