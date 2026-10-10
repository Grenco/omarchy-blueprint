package updates

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Cache keeps the last check in one machine-local file under the state
// directory, apart from profiles and every other Blueprint state.
type Cache struct{ Path string }

// CachePath is the update-check cache under stateHome.
func CachePath(stateHome string) string {
	return filepath.Join(stateHome, "omarchy-blueprint", "update-check.json")
}

type cacheEntry struct {
	Source       string    `json:"source"`
	CheckedAt    time.Time `json:"checked_at"`
	Failed       bool      `json:"failed,omitempty"`
	Latest       string    `json:"latest,omitempty"`
	DetailsURL   string    `json:"details_url,omitempty"`
	Instructions string    `json:"instructions,omitempty"`
}

func (c Cache) load() (cacheEntry, bool) {
	raw, err := os.ReadFile(c.Path)
	if err != nil {
		return cacheEntry{}, false
	}
	var entry cacheEntry
	if json.Unmarshal(raw, &entry) != nil || entry.CheckedAt.IsZero() {
		return cacheEntry{}, false
	}
	return entry, true
}

// save replaces the cache atomically, so a crash never leaves a torn file.
func (c Cache) save(entry cacheEntry) error {
	if c.Path == "" {
		return errors.New("update cache has no path")
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(c.Path), ".update-check-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), c.Path)
}
