package packages

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestResolveMiseGlobalConfigPathPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("MISE_CONFIG_DIR", "")
	t.Setenv("MISE_GLOBAL_CONFIG_FILE", "")
	assertMisePath(t, filepath.Join(home, ".config", "mise", "config.toml"))
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	assertMisePath(t, filepath.Join(xdg, "mise", "config.toml"))
	miseDir := filepath.Join(t.TempDir(), "mise")
	t.Setenv("MISE_CONFIG_DIR", miseDir)
	assertMisePath(t, filepath.Join(miseDir, "config.toml"))
	explicit := filepath.Join(t.TempDir(), "global.toml")
	t.Setenv("MISE_GLOBAL_CONFIG_FILE", explicit)
	assertMisePath(t, explicit)
}

func assertMisePath(t *testing.T, want string) {
	t.Helper()
	got, err := ResolveMiseGlobalConfigPath()
	if err != nil || got != want {
		t.Fatalf("path = %q, %v; want %q", got, err, want)
	}
}

func TestNormalizeMiseTool(t *testing.T) {
	tests := []struct {
		id   string
		raw  any
		want profile.MiseTool
	}{
		{"node", "24", profile.MiseTool{"version": "24"}},
		{"python", []any{"3.12", "3.13"}, profile.MiseTool{"version": []any{"3.12", "3.13"}}},
		{"npm:@anthropic-ai/claude-code", "latest", profile.MiseTool{"version": "latest"}},
		{"foo", map[string]any{"version": "2", "postinstall": "foo setup", "install_env": map[string]any{"FOO_MODE": "portable"}}, profile.MiseTool{"version": "2", "postinstall": "foo setup", "install_env": map[string]any{"FOO_MODE": "portable"}}},
		{"omitted-version", map[string]any{"postinstall": "echo ok"}, profile.MiseTool{"version": "latest", "postinstall": "echo ok"}},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got, err := NormalizeMiseTool(tt.id, tt.raw)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tool = %#v, %v; want %#v", got, err, tt.want)
			}
		})
	}
	for _, invalid := range []struct {
		id  string
		raw any
	}{{"", "24"}, {"has space", "24"}, {"bad\n", "24"}, {"node", nil}, {"node", []any{nil}}} {
		if _, err := NormalizeMiseTool(invalid.id, invalid.raw); err == nil {
			t.Fatalf("invalid %#v was accepted", invalid)
		}
	}
}

func TestReadMiseTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	contents := "[tools]\nnode = '24'\npython = ['3.12', '3.13']\n'npm:@anthropic-ai/claude-code' = 'latest'\nfoo = { version = '2', postinstall = 'foo setup' }\n\n[env]\nUNCHANGED = 'not package state'\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadMiseTools(path)
	if err != nil || len(got) != 4 || got["node"]["version"] != "24" || got["env"] != nil {
		t.Fatalf("tools = %#v, %v", got, err)
	}
	if missing, err := ReadMiseTools(filepath.Join(dir, "missing.toml")); err != nil || len(missing) != 0 {
		t.Fatalf("missing = %#v, %v", missing, err)
	}
	if err := os.WriteFile(path, []byte("[tools\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMiseTools(path); err == nil {
		t.Fatal("malformed TOML was accepted")
	}
}

func TestMiseSecretGuardAndPostinstall(t *testing.T) {
	literal := profile.MiseTools{"github:example/private": {"version": "latest", "install_env": map[string]any{"GITHUB_TOKEN": "ghp_secret"}}}
	if err := ValidateMiseSecrets(literal); err == nil || !strings.Contains(err.Error(), "install_env.GITHUB_TOKEN") {
		t.Fatalf("secret error = %v", err)
	}
	reference := profile.MiseTools{"github:example/private": {"version": "latest", "install_env": map[string]any{"Private-Key": "{{ env.PRIVATE_KEY }}"}}}
	if err := ValidateMiseSecrets(reference); err != nil {
		t.Fatal(err)
	}
	if MiseToolsHavePostinstall(profile.MiseTools{"node": {"version": "24"}}) {
		t.Fatal("clean tool is risky")
	}
	if !MiseToolsHavePostinstall(profile.MiseTools{"foo": {"nested": map[string]any{"POSTINSTALL": "setup"}}}) {
		t.Fatal("postinstall was missed")
	}
}

func TestEncodeMiseToolsAndSummaryAreDeterministic(t *testing.T) {
	tools := profile.MiseTools{"node": {"version": "24"}, "python": {"version": []any{"3.12", "3.13"}}}
	first, err := EncodeMiseTools(tools)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncodeMiseTools(tools)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("encoded = %q, %q, %v", first, second, err)
	}
	if got := SummarizeMiseTool(tools["node"]); got != "'24'" {
		t.Fatalf("summary = %q", got)
	}
	if got := SummarizeMiseTool(profile.MiseTool{"version": "2", "postinstall": "setup"}); !strings.Contains(got, "postinstall") {
		t.Fatalf("summary = %q", got)
	}
}

func TestBuildMiseAppendCandidatePreservesExistingBytes(t *testing.T) {
	existing := []byte("# user comment\n[tools]\nnode = '24'\n\n[env]\nWORK = '1'\n\n[settings]\njobs = 3\n")
	current := profile.MiseTools{"node": {"version": "24"}}
	additions := profile.MiseTools{"npm:@anthropic-ai/claude-code": {"version": "latest"}}
	candidate, err := BuildMiseAppendCandidate(existing, current, additions)
	if err != nil || !bytes.HasPrefix(candidate, existing) {
		t.Fatalf("candidate = %q, %v", candidate, err)
	}
	parsed, err := ReadMiseToolsFromBytes(candidate)
	if err != nil || !EqualMiseTool(parsed["node"], current["node"]) || !EqualMiseTool(parsed["npm:@anthropic-ai/claude-code"], additions["npm:@anthropic-ai/claude-code"]) {
		t.Fatalf("parsed = %#v, %v", parsed, err)
	}
	if _, err := BuildMiseAppendCandidate([]byte("tools = { node = '24' }\n"), current, profile.MiseTools{"python": {"version": "3.13"}}); err == nil || !strings.Contains(err.Error(), "cannot be safely extended") {
		t.Fatalf("inline tools error = %v", err)
	}
	if _, err := BuildMiseAppendCandidate([]byte("[tools\n"), current, additions); err == nil {
		t.Fatal("malformed TOML was accepted")
	}
}

func TestMiseSnapshotAndMutationPathSafety(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "real", "config.toml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("[tools]\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadMiseConfigSnapshot(config)
	if err != nil || !snapshot.Exists || snapshot.Hash != hashBytes(snapshot.Bytes) || snapshot.Mode.Perm() != 0o640 {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(config, link); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMiseMutationPath(link); err == nil {
		t.Fatal("symlink destination was accepted")
	}
	parentLink := filepath.Join(dir, "parent-link")
	if err := os.Symlink(filepath.Join(dir, "real"), parentLink); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMiseMutationPath(filepath.Join(parentLink, "new.toml")); err == nil {
		t.Fatal("symlink parent was accepted")
	}
	if err := os.MkdirAll(filepath.Join(dir, "real", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMiseMutationPath(filepath.Join(parentLink, "nested", "new.toml")); err == nil {
		t.Fatal("resolvable symlink parent was accepted")
	}
	if err := ValidateMiseMutationPath(filepath.Join(dir, "missing", "config.toml")); err != nil {
		t.Fatal(err)
	}
}
