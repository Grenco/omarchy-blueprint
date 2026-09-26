package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A profile captured on a classified machine, then inspected on a fresh
// Omarchy install whose configured repositories have no sync database.
func TestFreshPackageMetadataKeepsReadCommandsUsableAndSafe(t *testing.T) {
	profileDir := t.TempDir()
	runner := &machineRunner{official: map[string]bool{"git": true, "htop": true}, aur: map[string]bool{"yay": true}}
	var out, stderr bytes.Buffer
	deps := Dependencies{Runner: runner, In: strings.NewReader(""), Out: &out, Err: &stderr, Now: time.Now}
	run := func(args ...string) (int, string) {
		out.Reset()
		stderr.Reset()
		code := Execute(context.Background(), append([]string{"--profile", profileDir}, args...), deps)
		return code, out.String() + stderr.String()
	}
	if code := Execute(context.Background(), []string{"init", profileDir}, deps); code != 0 {
		t.Fatalf("init: %s", stderr.String())
	}
	if code, output := run("capture", "packages"); code != 0 {
		t.Fatalf("classified capture: %s", output)
	}
	official, err := os.ReadFile(filepath.Join(profileDir, "packages", "official.txt"))
	if err != nil {
		t.Fatal(err)
	}

	runner.repos, runner.dbPath = []string{"core", "extra"}, t.TempDir()

	if code, output := run("check"); code != 0 || !strings.Contains(output, "package origin classification unavailable") ||
		!strings.Contains(output, "core, extra") || !strings.Contains(output, "omarchy update") {
		t.Fatalf("check code=%d output=%s", code, output)
	}
	if code, output := run("--json", "check"); code != 0 || !strings.Contains(output, `"notes"`) {
		t.Fatalf("check json code=%d output=%s", code, output)
	}
	if code, output := run("status", "packages"); code != 0 {
		t.Fatalf("installed desired packages reported as drift: code=%d output=%s", code, output)
	}

	code, output := run("--json", "capture", "packages", "--dry-run")
	if code != 0 {
		t.Fatalf("capture preview code=%d output=%s", code, output)
	}
	var preview struct {
		Data struct {
			Sections []struct {
				Targets []struct {
					Key          string `json:"key"`
					Current      string `json:"current"`
					Outcome      string `json:"outcome"`
					SafetyReason string `json:"safety_reason"`
				} `json:"targets"`
			} `json:"sections"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &preview); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	seen := map[string]bool{}
	for _, section := range preview.Data.Sections {
		for _, target := range section.Targets {
			if !strings.HasPrefix(target.Key, "official:") && !strings.HasPrefix(target.Key, "aur:") {
				continue
			}
			seen[target.Key] = true
			if target.Outcome != "blocked" || target.Current != "present" || !strings.Contains(target.SafetyReason, "origin unavailable") {
				t.Fatalf("unclassified target not blocked safely: %#v", target)
			}
		}
	}
	for _, key := range []string{"official:git", "official:htop", "aur:yay"} {
		if !seen[key] {
			t.Fatalf("desired target %s missing from preview: %s", key, output)
		}
	}

	if code, output := run("capture", "packages"); code != 0 {
		t.Fatalf("capture without classification: %s", output)
	}
	after, err := os.ReadFile(filepath.Join(profileDir, "packages", "official.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(official) {
		t.Fatalf("capture rewrote desired official packages from a guess:\nbefore=%q\nafter=%q", official, after)
	}
	if aur, err := os.ReadFile(filepath.Join(profileDir, "packages", "aur.txt")); err != nil || strings.TrimSpace(string(aur)) != "yay" {
		t.Fatalf("aur.txt = %q, %v", aur, err)
	}

	if code, output := run("restore", "packages", "--dry-run"); code != 0 || strings.Contains(output, "omarchy pkg add") {
		t.Fatalf("restore plan reinstalls present packages: code=%d output=%s", code, output)
	}
}
