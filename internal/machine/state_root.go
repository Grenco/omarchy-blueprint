package machine

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

// ProfileStateDir identifies local state for the canonical profile root. It does not create it.
func ProfileStateDir(stateHome, profileRoot string) (string, error) {
	root, err := CanonicalProfileRoot(profileRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(stateHome, "omarchy-blueprint", "profiles", fmt.Sprintf("%x", sha256.Sum256([]byte(root)))), nil
}
