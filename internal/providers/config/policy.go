package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

var (
	pemPrivateKey   = regexp.MustCompile(`(?m)^-----BEGIN (?:[A-Z0-9]+ )?PRIVATE KEY-----\r?$`)
	structuredToken = regexp.MustCompile(`(?im)^\s*["']?(?:api_token|token)["']?\s*[:=]\s*["']?[A-Za-z0-9._~-]{16,}`)
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

func IsOmarchyUpdateBackupName(name string) bool {
	i := strings.Index(name, ".bak.")
	return i > 0 && i+len(".bak.") < len(name)
}
func IsOmarchySetupBackupName(name string) bool {
	i := strings.Index(name, ".backup-")
	return i > 0 && i+len(".backup-") < len(name)
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
	if IsOmarchyUpdateBackupName(name) || IsOmarchySetupBackupName(name) {
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
	if strings.Contains(lower, "cache/") || strings.Contains(lower, "code cache/") || strings.Contains(lower, "session storage/") || strings.Contains(lower, "service worker/") || strings.HasSuffix(lower, ".pid") || strings.HasSuffix(lower, ".lock") || strings.HasSuffix(lower, "singletonlock") {
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
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(b)
	return pemPrivateKey.MatchString(content) || structuredToken.MatchString(content), nil
}
