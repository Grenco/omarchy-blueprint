package screens

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestSyncScreenActionsCoverProfileGitStates(t *testing.T) {
	managed := profilegit.Change{Path: "profile.toml", Managed: true}
	unmanaged := profilegit.Change{Path: "notes.txt"}
	cases := []struct {
		name              string
		status            profilegit.Status
		enabled, disabled []string
	}{
		{"not repository", profilegit.Status{}, []string{"sync.init"}, []string{"sync.fetch", "sync.commit", "sync.pull", "sync.push"}},
		{"repo no origin", profilegit.Status{Repository: true, Branch: "main"}, []string{"sync.set-remote"}, []string{"sync.fetch", "sync.push"}},
		{"clean tracked", profilegit.Status{Repository: true, Branch: "main", Head: "abc", Origin: "https://example.test/me/profile.git", Upstream: "origin/main"}, []string{"sync.fetch", "sync.set-remote", "sync.remove-remote", "sync.push"}, []string{"sync.commit", "sync.pull"}},
		{"managed dirty", profilegit.Status{Repository: true, Branch: "main", Head: "abc", Origin: "https://example.test/me/profile.git", Changes: []profilegit.Change{managed}}, []string{"sync.commit", "sync.commit-push"}, nil},
		{"unmanaged dirty", profilegit.Status{Repository: true, Branch: "main", Head: "abc", Origin: "https://example.test/me/profile.git", Changes: []profilegit.Change{unmanaged}}, []string{"sync.lazygit", "sync.copy-profile"}, []string{"sync.commit", "sync.pull", "sync.push"}},
		{"no upstream", profilegit.Status{Repository: true, Branch: "main", Head: "abc", Origin: "https://example.test/me/profile.git"}, []string{"sync.push"}, []string{"sync.pull"}},
		{"ahead", profilegit.Status{Repository: true, Branch: "main", Head: "abc", Origin: "https://example.test/me/profile.git", Upstream: "origin/main", Ahead: 1}, []string{"sync.push", "sync.commit-push"}, nil},
		{"behind", profilegit.Status{Repository: true, Branch: "main", Head: "abc", Origin: "https://example.test/me/profile.git", Upstream: "origin/main", Behind: 1}, []string{"sync.pull", "sync.push"}, nil},
		{"diverged", profilegit.Status{Repository: true, Branch: "main", Head: "abc", Origin: "https://example.test/me/profile.git", Upstream: "origin/main", Ahead: 1, Behind: 1}, []string{"sync.lazygit"}, []string{"sync.pull", "sync.push", "sync.commit"}},
		{"detached", profilegit.Status{Repository: true, Head: "abc", Origin: "https://example.test/me/profile.git"}, []string{"sync.lazygit"}, []string{"sync.push", "sync.commit"}},
		{"staged", profilegit.Status{Repository: true, Branch: "main", Head: "abc", Origin: "https://example.test/me/profile.git", Changes: []profilegit.Change{{Path: "profile.toml", Managed: true, Index: "M"}}}, []string{"sync.lazygit"}, []string{"sync.commit", "sync.push"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			screen := &Sync{status: tc.status}
			actions := syncActionsByID(screen.Actions())
			for _, id := range tc.enabled {
				if !actions[id].Enabled {
					t.Errorf("%s disabled: %s", id, actions[id].DisabledReason)
				}
			}
			for _, id := range tc.disabled {
				if actions[id].Enabled || actions[id].DisabledReason == "" {
					t.Errorf("%s=%#v, want disabled with reason", id, actions[id])
				}
			}
		})
	}
}

func TestSyncScreenInitAndFetchAreExplicit(t *testing.T) {
	session, root := newSyncSession(t)
	git(t, root, "init", "-b", "main")
	git(t, root, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	screen := NewSync(session)
	msg := screen.Init()()
	screen.Update(msg)
	if !screen.status.Repository || !screen.lastFetched.IsZero() {
		t.Fatalf("init status=%#v fetched=%v", screen.status, screen.lastFetched)
	}
	msg = screen.Update(tea.KeyPressMsg{Code: 'f'})()
	screen.Update(msg)
	if screen.err == nil {
		t.Fatal("fetch unexpectedly succeeded against missing origin")
	}
}

func TestSyncScreenCommitMessageFlow(t *testing.T) {
	session, root := newSyncSession(t)
	git(t, root, "init", "-b", "main")
	git(t, root, "add", "profile.toml")
	git(t, root, "commit", "-m", "initial")
	appendFile(t, filepath.Join(root, "profile.toml"), "# changed\n")
	screen := NewSync(session)
	screen.Update(screen.Init()())
	screen.Update(tea.KeyPressMsg{Code: 'c'})
	if want := profilegit.SuggestedCommitMessage(screen.status); screen.message != want {
		t.Fatalf("message=%q want=%q", screen.message, want)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if screen.confirm != "commit" {
		t.Fatalf("confirm=%q", screen.confirm)
	}
	msg := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})()
	screen.Update(msg)
	if screen.err != nil || screen.status.Ahead != 0 || len(screen.status.Changes) != 0 {
		t.Fatalf("commit status=%#v err=%v", screen.status, screen.err)
	}
}

func TestSyncScreenCommitPushStopsOnCommitErrorAndPushesNoopAhead(t *testing.T) {
	session, root := newSyncSession(t)
	remote := filepath.Join(t.TempDir(), "origin.git")
	git(t, t.TempDir(), "init", "--bare", remote)
	git(t, root, "init", "-b", "main")
	git(t, root, "add", "profile.toml")
	git(t, root, "commit", "-m", "initial")
	git(t, root, "remote", "add", "origin", remote)
	git(t, root, "push", "-u", "origin", "main")
	remoteHead := git(t, remote, "rev-parse", "main")
	appendFile(t, filepath.Join(root, "profile.toml"), "# staged\n")
	git(t, root, "add", "profile.toml")
	screen := NewSync(session)
	screen.Update(screen.Init()())
	msg := screen.commit(true)()
	screen.Update(msg)
	if screen.err == nil {
		t.Fatal("staged commit unexpectedly succeeded")
	}
	if got := git(t, remote, "rev-parse", "main"); strings.TrimSpace(got) != strings.TrimSpace(remoteHead) {
		t.Fatal("push ran after commit error")
	}
	git(t, root, "reset", "--hard", "HEAD")
	appendFile(t, filepath.Join(root, "profile.toml"), "# ahead\n")
	git(t, root, "add", "profile.toml")
	git(t, root, "commit", "-m", "ahead")
	screen.Update(screen.Init()())
	if !screen.allowed("commit-push") {
		t.Fatal("no-op commit & push should be allowed for ahead history")
	}
	msg = screen.commit(true)()
	screen.Update(msg)
	if screen.err != nil {
		t.Fatal(screen.err)
	}
	if strings.TrimSpace(git(t, remote, "rev-parse", "main")) != strings.TrimSpace(git(t, root, "rev-parse", "HEAD")) {
		t.Fatal("no-op commit & push did not push ahead history")
	}
}

func TestSyncScreenRemoteBrowserConversion(t *testing.T) {
	for _, tc := range []struct {
		remote, want string
		ok           bool
	}{{"git@github.com:me/profile.git", "https://github.com/me/profile", true}, {"https://user@example.com/me/profile.git", "https://example.com/me/profile", true}, {"file:///tmp/profile.git", "", false}} {
		got, ok := profilegit.BrowserURL(tc.remote)
		if got != tc.want || ok != tc.ok {
			t.Errorf("BrowserURL(%q)=(%q,%v)", tc.remote, got, ok)
		}
	}
}

func syncActionsByID(actions []SyncAction) map[string]SyncAction {
	result := make(map[string]SyncAction, len(actions))
	for _, action := range actions {
		result[action.ID] = action
	}
	return result
}
func newSyncSession(t *testing.T) (*workflow.Session, string) {
	t.Helper()
	root, state := t.TempDir(), t.TempDir()
	if err := profile.Save(root, profile.New("test", time.Now())); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(workflow.Dependencies{Runner: command.SystemRunner{}, StateHome: func() (string, error) { return state, nil }}, workflow.Options{ProfileDir: root})
	if err != nil {
		t.Fatal(err)
	}
	return session, root
}
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := command.SystemRunner{}
	if out, err := command.Run(context.Background(), "git", append([]string{"-C", dir}, args...)...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	} else {
		return out
	}
	return ""
}
func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}
