package app

import (
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"strings"
	"testing"
)

func TestSyncCLIReadOnlySurface(t *testing.T) {
	_, deps := configSandbox(t)
	for _, args := range [][]string{{"sync", "inspect"}, {"sync", "bootstrap", "inspect"}} {
		cmd, rest, err := newRoot(deps).Find(args)
		if err != nil || len(rest) != 0 || cmd.Name() != "inspect" {
			t.Fatalf("missing inspection command: %v %v %v", args, rest, err)
		}
		if cmd.Flags().Lookup("no-fetch") == nil {
			t.Fatal("missing offline flag")
		}
		for _, name := range []string{"apply", "yes", "force", "restore", "capture"} {
			if cmd.Flags().Lookup(name) != nil {
				t.Fatal("execution option leaked", name)
			}
		}
	}
}

func TestSyncHumanOutputGroupsDeterministically(t *testing.T) {
	p := syncengine.Plan{Identity: syncengine.Identity{Base: strings.Repeat("a", 40), Local: strings.Repeat("b", 40), Remote: strings.Repeat("c", 40), Branch: "main", Upstream: "origin/main"}, Blockers: []syncengine.Blocker{{Code: "blocked", Message: "review required"}}, Items: []syncengine.Item{
		{TargetInput: syncengine.TargetInput{Key: syncengine.TargetKey{Category: "packages", Target: "official:z"}}, History: syncengine.HistoryRemote},
		{TargetInput: syncengine.TargetInput{Key: syncengine.TargetKey{Category: "packages", Target: "official:a"}}, History: syncengine.HistoryConflict},
	}}
	got := formatSyncPlan(p)
	for _, text := range []string{"aaaaaaa", "bbbbbbb", "ccccccc", "main", "origin/main", "Blockers", "Conflicts", "Remote", "packages:official:a"} {
		if !strings.Contains(got, text) {
			t.Fatal("missing human evidence", text, got)
		}
	}
	if strings.Index(got, "Conflicts") > strings.Index(got, "Remote\n") {
		t.Fatal("nondeterministic classification groups", got)
	}
	if strings.Contains(got, p.Identity.Base) {
		t.Fatal("human output should use short SHAs")
	}
}
