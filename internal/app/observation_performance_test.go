package app

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/tui"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// This explicitly opted-in probe performs only live observation/preview.
// It never invokes Capture, Apply, or user-manager/package mutators.
func TestLiveObservationRefresh(t *testing.T) {
	if os.Getenv("BLUEPRINT_OBSERVATION_LIVE_BENCHMARK") != "1" {
		t.Skip("opt-in read-only live benchmark")
	}
	root := os.Getenv("BLUEPRINT_BENCHMARK_PROFILE")
	if root == "" {
		t.Fatal("explicit BLUEPRINT_BENCHMARK_PROFILE required")
	}
	// App test isolation normally redirects Mise to a temporary empty file.
	// The explicit live probe must instead inspect the actual global declaration.
	t.Setenv("MISE_GLOBAL_CONFIG_FILE", "")
	for _, mode := range []string{"overview", "status", "services", "packages", "overview-restore", "restore-force-safe"} {
		t.Run(mode, func(t *testing.T) {
			for run := 0; run < 5; run++ {
				started := time.Now()
				code := Execute(context.Background(), []string{"--profile", root, "tui"}, Dependencies{Out: io.Discard, Err: io.Discard, IsTTY: func() bool { return true }, RunTUI: func(ctx context.Context, o tui.Options, deps tui.Dependencies) error {
					s, e := deps.OpenSession(workflow.Options{ProfileDir: o.ProfileDir})
					if e != nil {
						return e
					}
					switch mode {
					case "status", "services", "packages":
						only := mode
						if only == "status" {
							only = ""
						}
						_, e := s.Status(ctx, only)
						return e
					case "overview":
						_, e := s.Overview(ctx)
						return e
					case "overview-restore":
						if _, e := s.Overview(ctx); e != nil {
							return e
						}
						_, e := s.PreviewRestore(ctx, "", nil)
						return e
					default:
						cycle, e := s.BeginRead(ctx)
						if e != nil {
							return e
						}
						defer cycle.Close()
						force := policy.DefaultRestoreOptions()
						force.Conflicts = policy.ConflictForce
						if _, e := cycle.PreviewRestore(ctx, "", &force); e != nil {
							return e
						}
						force.Conflicts = policy.ConflictSafe
						_, e = cycle.PreviewRestore(ctx, "", &force)
						return e
					}
				}})
				if code != 0 {
					t.Fatalf("read-only probe failed (exit %d); inspect separately without publishing private command output", code)
				}
				t.Logf("mode=%s run=%d wall=%s", mode, run, time.Since(started))
			}
		})
	}
}
