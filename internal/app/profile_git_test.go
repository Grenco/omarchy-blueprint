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

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestProfileGitCommandsRegister(t *testing.T) {
	root := newRoot(Dependencies{})
	for _, args := range [][]string{{"profile", "git", "status"}, {"profile", "git", "init"}, {"profile", "git", "remote"}, {"profile", "git", "remote", "set", "origin"}, {"profile", "git", "remote", "remove"}, {"profile", "git", "fetch"}, {"profile", "git", "diff"}, {"profile", "git", "commit"}, {"profile", "git", "pull"}, {"profile", "git", "push"}} {
		if _, _, err := root.Find(args); err != nil {
			t.Fatalf("Find(%v): %v", args, err)
		}
	}
}

func TestProfileGitStatusNonRepositoryHumanAndJSON(t *testing.T) {
	profileDir := newProfileGitProfile(t)
	code, human := profileGitRun(t, profileDir, "profile", "git", "status")
	if code != 0 || !strings.Contains(human, "repository: no") || !strings.Contains(human, "profile git init") {
		t.Fatalf("human code=%d output=%q", code, human)
	}
	code, output := profileGitRun(t, profileDir, "--json", "profile", "git", "status")
	if code != 0 {
		t.Fatalf("json code=%d output=%q", code, output)
	}
	var envelope struct {
		Command string `json:"command"`
		Data    struct {
			ProfileGit struct {
				Repository bool   `json:"repository"`
				Branch     string `json:"branch"`
				Ahead      int    `json:"ahead"`
				Behind     int    `json:"behind"`
				Changes    []struct {
					Managed bool `json:"managed"`
				} `json:"changes"`
			} `json:"profilegit"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Command != "profile git status" || envelope.Data.ProfileGit.Repository || envelope.Data.ProfileGit.Branch != "" || envelope.Data.ProfileGit.Ahead != 0 || envelope.Data.ProfileGit.Behind != 0 {
		t.Fatalf("status=%+v", envelope)
	}
}

func TestProfileGitLifecycle(t *testing.T) {
	profileDir, origin := newProfileGitProfile(t), filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, "init", "--bare", origin)
	if code, output := profileGitRun(t, profileDir, "profile", "git", "init"); code != 0 || !strings.Contains(output, "init complete") {
		t.Fatalf("init code=%d output=%q", code, output)
	}
	gitRun(t, "-C", profileDir, "config", "user.email", "test@example.invalid")
	gitRun(t, "-C", profileDir, "config", "user.name", "Blueprint Test")
	if code, output := profileGitRun(t, profileDir, "profile", "git", "remote", "set", origin); code != 0 || !strings.Contains(output, "origin updated") {
		t.Fatalf("remote code=%d output=%q", code, output)
	}
	if code, output := profileGitRun(t, profileDir, "profile", "git", "commit", "-m", "Initial profile"); code != 0 || !strings.Contains(output, "Profile Git commit:") {
		t.Fatalf("commit code=%d output=%q", code, output)
	}
	if code, output := profileGitRun(t, profileDir, "profile", "git", "push"); code != 0 || !strings.Contains(output, "push complete") {
		t.Fatalf("push code=%d output=%q", code, output)
	}
	if out := gitRun(t, "--git-dir", origin, "rev-parse", "refs/heads/main"); strings.TrimSpace(out) == "" {
		t.Fatal("origin main has no commit")
	}
	code, output := profileGitRun(t, profileDir, "--json", "profile", "git", "status")
	if code != 0 {
		t.Fatalf("status code=%d output=%q", code, output)
	}
	var envelope struct {
		Data struct {
			ProfileGit struct {
				Repository bool   `json:"repository"`
				Branch     string `json:"branch"`
				Origin     string `json:"origin"`
				Upstream   string `json:"upstream"`
				Ahead      int    `json:"ahead"`
				Behind     int    `json:"behind"`
			} `json:"profilegit"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	status := envelope.Data.ProfileGit
	if !status.Repository || status.Branch != "main" || status.Origin != origin || status.Upstream != "origin/main" || status.Ahead != 0 || status.Behind != 0 {
		t.Fatalf("status=%+v", status)
	}
	profileGitMustRun(t, profileDir, "machine", "add", "commit-regression", "--no-use")
	if code, output := profileGitRun(t, profileDir, "profile", "git", "commit", "-m", "Add machine"); code != 0 || !strings.Contains(output, "Profile Git commit:") {
		t.Fatalf("mutation commit code=%d output=%q", code, output)
	}
	if loaded, err := profile.Load(profileDir); err != nil || len(loaded.Machines.Items) != 1 || loaded.Machines.Items[0].Name != "commit-regression" {
		t.Fatalf("loaded profile=%#v err=%v", loaded.Machines, err)
	}
}

func TestProfileGitRefusesUnsafeOperationsAndSanitizesRemote(t *testing.T) {
	profileDir, origin := newProfileGitProfile(t), filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, "init", "--bare", origin)
	profileGitMustRun(t, profileDir, "profile", "git", "init")
	gitRun(t, "-C", profileDir, "config", "user.email", "test@example.invalid")
	gitRun(t, "-C", profileDir, "config", "user.name", "Blueprint Test")
	gitRun(t, "-C", profileDir, "add", "profile.toml")
	if code, output := profileGitRun(t, profileDir, "profile", "git", "commit"); code != 1 || !strings.Contains(output, "already has staged changes") {
		t.Fatalf("staged commit code=%d output=%q", code, output)
	}
	gitRun(t, "-C", profileDir, "reset")
	profileGitMustRun(t, profileDir, "profile", "git", "commit")
	profileGitMustRun(t, profileDir, "profile", "git", "remote", "set", origin)
	profileGitMustRun(t, profileDir, "profile", "git", "push")
	profileGitMustRun(t, profileDir, "profile", "git", "remote", "set", "https://user:secret@example.invalid/profile.git")
	code, output := profileGitRun(t, profileDir, "--json", "profile", "git", "status")
	if code != 0 || strings.Contains(output, "user:secret") || !strings.Contains(output, "https://example.invalid/profile.git") {
		t.Fatalf("sanitized status code=%d output=%q", code, output)
	}
	profileGitMustRun(t, profileDir, "profile", "git", "remote", "set", origin)
	if err := os.WriteFile(filepath.Join(profileDir, "README.md"), []byte("unmanaged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, output := profileGitRun(t, profileDir, "profile", "git", "pull"); code != 1 || !strings.Contains(output, "uncommitted changes") {
		t.Fatalf("dirty pull code=%d output=%q", code, output)
	}
	gitRun(t, "-C", profileDir, "clean", "-fd")
	gitRun(t, "-C", profileDir, "checkout", "--detach")
	if code, output := profileGitRun(t, profileDir, "profile", "git", "push"); code != 1 || !strings.Contains(output, "detached HEAD") {
		t.Fatalf("detached push code=%d output=%q", code, output)
	}
}

func newProfileGitProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := profile.Save(dir, profile.New("test", time.Now())); err != nil {
		t.Fatal(err)
	}
	return dir
}

func profileGitRun(t *testing.T, profileDir string, args ...string) (int, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := Execute(context.Background(), append([]string{"--profile", profileDir}, args...), Dependencies{Runner: command.SystemRunner{}, Out: &out, Err: &stderr})
	return code, out.String() + stderr.String()
}

func profileGitMustRun(t *testing.T, profileDir string, args ...string) string {
	t.Helper()
	code, output := profileGitRun(t, profileDir, args...)
	if code != 0 {
		t.Fatalf("%v code=%d output=%q", args, code, output)
	}
	return output
}

func gitRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := (command.SystemRunner{}).Run(context.Background(), "git", args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}
