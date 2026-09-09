package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

var (
	pemPrivateKey              = regexp.MustCompile(`(?m)^-----BEGIN (?:[A-Z0-9]+ )?PRIVATE KEY-----\r?$`)
	structuredToken            = regexp.MustCompile(`(?i)["']?(?:api_token|access_token|auth_token|refresh_token|client_secret)["']?\s*[:=]\s*["']?[A-Za-z0-9._~-]{16,}`)
	sensitiveContentInspection func()
	sensitiveContentRegexCheck func()
)

const MaxAutomaticConfigFileSize int64 = 16 << 20
const MaxMergeableTextSize int64 = 4 << 20

const sensitiveContentChunkSize = 32 << 10
const sensitiveContentOverlap = 4 << 10

var sensitiveTokenKeys = [][]byte{
	[]byte("api_token"),
	[]byte("access_token"),
	[]byte("auth_token"),
	[]byte("refresh_token"),
	[]byte("client_secret"),
}

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

// NormalizeExclusionPath converts a config policy argument to the logical path
// used by the .config provider.
func NormalizeExclusionPath(input string) (string, error) {
	input = strings.TrimSpace(strings.ReplaceAll(input, "\\", "/"))
	input = strings.TrimPrefix(input, "~/.config/")
	input = strings.TrimPrefix(input, ".config/")
	if input == "~/.config" || input == ".config" {
		return "", fmt.Errorf("config exclusion path must name an entry below ~/.config")
	}
	if input == "" || strings.HasPrefix(input, "~") || strings.HasPrefix(input, "/") {
		return "", fmt.Errorf("invalid config exclusion path %q", input)
	}
	input = path.Clean(input)
	if input == "." || input == ".." || strings.HasPrefix(input, "../") || input == ".ssh" || strings.HasPrefix(input, ".ssh/") {
		return "", fmt.Errorf("invalid config exclusion path %q", input)
	}
	return profile.NormalizeConfigPath(input)
}

// AddExclusion returns copied config metadata with path excluded and all saved
// files and tombstones below that path removed. It never touches live config.
func AddExclusion(saved profile.Configs, path string) (profile.Configs, []string, error) {
	path, err := NormalizeExclusionPath(path)
	if err != nil {
		return saved, nil, err
	}
	result := profile.Configs{Files: append([]profile.ConfigFile{}, saved.Files...), Deletes: append([]profile.ConfigDelete{}, saved.Deletes...), Excluded: append([]string{}, saved.Excluded...)}
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
	return result, removed, nil
}

// RemoveExclusion returns copied config metadata with path no longer excluded.
func RemoveExclusion(saved profile.Configs, path string) (profile.Configs, bool, error) {
	path, err := NormalizeExclusionPath(path)
	if err != nil {
		return saved, false, err
	}
	result := profile.Configs{Files: append([]profile.ConfigFile{}, saved.Files...), Deletes: append([]profile.ConfigDelete{}, saved.Deletes...), Excluded: append([]string{}, saved.Excluded...)}
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
func IsExcludedConfigPath(path string, excluded []string) bool {
	path, err := profile.NormalizeConfigPath(path)
	if err != nil {
		return false
	}
	for _, item := range excluded {
		item, err = profile.NormalizeConfigPath(item)
		if err == nil && (path == item || strings.HasPrefix(path, item+"/")) {
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
	if IsOmarchyUpdateBackupName(name) || IsOmarchySetupBackupName(name) || IsBlueprintBackupName(name) {
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
	if runtimeConfigPath(lower) || strings.HasSuffix(lower, ".pid") || strings.HasSuffix(lower, ".lock") || strings.HasSuffix(lower, "singletonlock") {
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
		switch part {
		case "indexeddb", "local storage", "webstorage", "session storage", "service worker", "code cache", "gpucache", "cache", "dawncache", "blob_storage", "file system":
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
	return strings.HasPrefix(path, ".config/gcloud/")
}

// hasSensitiveContent rejects high-confidence credential material before it is
// persisted in a profile, even when its filename looks harmless.
func hasSensitiveContent(path string) (bool, error) {
	f, _, err := content.OpenRegularFile(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if sensitiveContentInspection != nil {
		sensitiveContentInspection()
	}

	chunk := make([]byte, sensitiveContentChunkSize)
	var previous []byte
	reader := io.LimitReader(f, MaxAutomaticConfigFileSize+1)
	for {
		n, err := reader.Read(chunk)
		if n > 0 {
			window := append(append([]byte{}, previous...), chunk[:n]...)
			if sensitiveContentWindow(window) {
				return true, nil
			}
			if len(window) > sensitiveContentOverlap {
				previous = append(previous[:0], window[len(window)-sensitiveContentOverlap:]...)
			} else {
				previous = append(previous[:0], window...)
			}
		}
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
}

func sensitiveContentWindow(window []byte) bool {
	for start := 0; ; {
		i := bytes.Index(window[start:], []byte("-----BEGIN "))
		if i < 0 {
			break
		}
		i += start
		if i == 0 || window[i-1] == '\n' {
			if sensitiveContentRegexCheck != nil {
				sensitiveContentRegexCheck()
			}
			if pemPrivateKey.Match(window[i:]) {
				return true
			}
		}
		start = i + 1
	}
	for _, key := range sensitiveTokenKeys {
		if containsASCIIFold(window, key) {
			if sensitiveContentRegexCheck != nil {
				sensitiveContentRegexCheck()
			}
			if structuredToken.Match(window) {
				return true
			}
		}
	}
	return false
}

func containsASCIIFold(b, want []byte) bool {
	for i := 0; i+len(want) <= len(b); i++ {
		if bytes.EqualFold(b[i:i+len(want)], want) {
			return true
		}
	}
	return false
}
