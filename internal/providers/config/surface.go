package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxSurfaceProbeEntries = 256
	maxSurfaceProbeDepth   = 3
)

type SurfaceClassification string

const (
	SurfaceConfigLean SurfaceClassification = "config-lean"
	SurfaceStateHeavy SurfaceClassification = "state-heavy"
	SurfaceMixed      SurfaceClassification = "mixed"
	SurfaceSensitive  SurfaceClassification = "sensitive"
)

type SurfaceSummary struct {
	Path           string                `json:"path"`
	Classification SurfaceClassification `json:"classification"`
	Reasons        []string              `json:"reasons,omitempty"`
	SampledEntries int                   `json:"sampled_entries,omitempty"`
	Explicit       bool                  `json:"explicit,omitempty"`
}

type SurfaceProbe struct {
	Entries, RegularFiles, ConfigFiles, Directories, MaxDepth int
	ProbeLimitHit                                             bool
	Names, RelativeNames                                      map[string]bool
	config, state, sensitive                                  map[string]bool
}

// ProbeSurface gathers bounded names and metadata only. ReadDir's ordering is
// made explicit so all classification results are stable across filesystems.
func ProbeSurface(root string) (SurfaceProbe, error) {
	p := SurfaceProbe{Names: map[string]bool{}, RelativeNames: map[string]bool{}, config: map[string]bool{}, state: map[string]bool{}, sensitive: map[string]bool{}}
	var walk func(string, string, int) error
	walk = func(dir, rel string, depth int) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if p.Entries >= maxSurfaceProbeEntries {
				p.ProbeLimitHit = true
				return nil
			}
			path := filepath.Join(dir, entry.Name())
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			p.Entries++
			childRel := filepath.ToSlash(filepath.Join(rel, entry.Name()))
			lowerName, lowerRel := strings.ToLower(entry.Name()), strings.ToLower(childRel)
			p.Names[lowerName], p.RelativeNames[lowerRel] = true, true
			if info.IsDir() {
				p.Directories++
				if depth+1 > p.MaxDepth {
					p.MaxDepth = depth + 1
				}
			} else if info.Mode().IsRegular() {
				p.RegularFiles++
			}
			p.observe(lowerName, lowerRel, info.IsDir())
			if info.IsDir() && info.Mode()&os.ModeSymlink == 0 && depth < maxSurfaceProbeDepth && !IsBackupArtifactName(entry.Name(), true) {
				if err := walk(path, childRel, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root, "", 0); err != nil {
		return p, err
	}
	return p, nil
}

func (p *SurfaceProbe) observe(name, rel string, dir bool) {
	if isRuntimeComponent(name) {
		p.state["state-runtime-directories"] = true
	}
	if dir && (name == "themes" || name == "snippets" || name == "keybindings" || name == "macros" || name == "templates") {
		p.config["config-like-files"] = true
	}
	if !dir && isConfigName(name) {
		p.ConfigFiles++
		p.config["config-like-files"] = true
	}
	if strings.HasSuffix(name, ".sqlite-wal") || strings.HasSuffix(name, ".sqlite-shm") || strings.HasSuffix(name, ".db-journal") {
		p.state["state-database-family"] = true
	}
	if name == "current" || name == "lock" || strings.HasPrefix(name, "manifest-") || strings.HasSuffix(name, ".ldb") {
		p.state["state-leveldb"] = true
	}
	if name == "cookies" || name == "history" || name == "dips" || name == "sharedstorage" || name == "network persistent state" {
		p.state["state-runtime-directories"] = true
	}
	if name == "login data" || name == "key4.db" {
		p.sensitive["hard-sensitive-structure"] = true
	}
}

func isRuntimeComponent(name string) bool {
	switch name {
	case "cache", "code cache", "gpucache", "dawncache", "dawngraphitecache", "dawnwebgpucache", "indexeddb", "local storage", "session storage", "service worker", "webstorage", "blob_storage", "file system", "crashpad", "crashes", "logs", "telemetry", "sessionstore-backups", "bookmarkbackups", "draftsrecover":
		return true
	}
	return false
}
func isConfigName(name string) bool {
	if name == "config" || name == "settings" || name == "keybindings" || name == "preferences" {
		return true
	}
	for _, ext := range []string{".conf", ".ini", ".toml", ".yaml", ".yml", ".json", ".jsonc", ".lua", ".vim", ".fish", ".nu", ".css", ".scss", ".py", ".sh"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func ClassifySurface(p SurfaceProbe) (SurfaceClassification, []string) {
	reasons := func(m map[string]bool) []string {
		r := make([]string, 0, len(m))
		for k := range m {
			r = append(r, k)
		}
		sort.Strings(r)
		return r
	}
	chromiumProfiles := false
	for rel := range p.RelativeNames {
		if strings.HasPrefix(rel, "default/") || strings.HasPrefix(rel, "profile ") {
			chromiumProfiles = true
			break
		}
	}
	markers := 0
	for _, marker := range []string{"preferences", "secure preferences", "cookies", "history", "web data", "extensions", "network", "dips"} {
		if p.Names[marker] {
			markers++
		}
	}
	if p.Names["local state"] && chromiumProfiles && markers >= 2 {
		return SurfaceStateHeavy, []string{"browser-profile-chromium"}
	}
	geckoMarkers := 0
	for _, marker := range []string{"places.sqlite", "cookies.sqlite", "extensions.json", "sessionstore.jsonlz4", "storage", "sessionstore-backups"} {
		if p.Names[marker] {
			geckoMarkers++
		}
	}
	if (p.Names["profiles.ini"] && p.Names["prefs.js"] && geckoMarkers >= 1) || (p.Names["prefs.js"] && geckoMarkers >= 2) {
		return SurfaceStateHeavy, []string{"browser-profile-gecko"}
	}
	webkit := 0
	for _, marker := range []string{"websitedata", "webkitwebsitedata", "localstorage", "indexeddb", "cookies", "networkcache", "service worker", "session storage"} {
		if p.Names[marker] {
			webkit++
		}
	}
	if webkit >= 3 {
		return SurfaceStateHeavy, []string{"browser-profile-webkit"}
	}
	if len(p.sensitive) > 0 && len(p.state) > 0 {
		return SurfaceSensitive, append(reasons(p.sensitive), reasons(p.state)...)
	}
	if len(p.config) > 0 && len(p.state) > 0 {
		return SurfaceMixed, append(reasons(p.config), reasons(p.state)...)
	}
	if len(p.state) >= 2 && len(p.config) == 0 {
		return SurfaceStateHeavy, reasons(p.state)
	}
	if len(p.state) == 0 && (p.Entries <= 64 && p.MaxDepth <= maxSurfaceProbeDepth || p.RegularFiles > 0 && p.ConfigFiles*10 >= p.RegularFiles*7) {
		return SurfaceConfigLean, reasons(p.config)
	}
	r := reasons(p.state)
	if p.ProbeLimitHit {
		r = append(r, "probe-limit-reached")
	}
	if len(r) == 0 {
		r = []string{"mixed-config-and-runtime"}
	}
	return SurfaceMixed, r
}
