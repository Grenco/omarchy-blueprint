package app

import (
	"context"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestReadAdaptersPreserveCapabilities(t *testing.T) {
	dir, deps := configSandbox(t)
	providers := workflowProviders(deps, &options{profileDir: dir})
	c := observation.New(context.Background())
	defer c.Close()
	for _, p := range providers {
		binder, ok := p.(workflow.ReadCycleBinder)
		if !ok {
			t.Fatalf("%s lacks read binding", p.ID())
		}
		v := binder.BindReadCycle(c)
		if v.ID() != p.ID() {
			t.Fatal("provider ordering/identity changed")
		}
		if _, ok := v.(workflow.Provider); ok {
			t.Fatal("read grants Capture", p.ID())
		}
		if _, ok := v.(workflow.RestoreProvider); ok {
			t.Fatal("read grants Verify", p.ID())
		}
		if _, ok := v.(interface{ CommitCapture() error }); ok {
			t.Fatal("read grants transaction", p.ID())
		}
		if _, ok := v.(workflow.ChangeTargetResolver); !ok {
			t.Fatal("lost change resolver", p.ID())
		}
		if _, ok := v.(workflow.RestoreTargetResolver); !ok {
			t.Fatal("lost restore resolver", p.ID())
		}
		if _, ok := v.(interface{ ValidateTarget(string) (string, error) }); !ok {
			t.Fatal("lost validation", p.ID())
		}
		if _, ok := v.(workflow.ReadRestoreProvider); !ok {
			t.Fatal("lost preview planning", p.ID())
		}
		if p.ID() == "config" {
			if _, ok := v.(scanDiffProvider); !ok {
				t.Fatal("lost config scan")
			}
		}
		if p.ID() == "resources" {
			if _, ok := v.(resourceDiffProvider); !ok {
				t.Fatal("lost resource Git scan")
			}
		}
	}
}
