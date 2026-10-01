package services

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

type serviceEffectRunner struct {
	calls []string
	t     *testing.T
	live  string
}

func (r *serviceEffectRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	if _, err := os.Stat(r.live); err != nil {
		r.t.Fatalf("semantic command ran before required definition: %v", err)
	}
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return "", nil
}
func TestServicesExecutionFileFailureBlocksReloadAndStateCommands(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units = nil
	fragment := persistentPlan(t, p, *data, false, false)
	if !hasCommand(fragment, "disable") {
		t.Fatal("fixture did not include dependent persistent state")
	}
	if err := os.WriteFile(live, []byte("unapproved destination race"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal, err := restore.NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	runner := &serviceEffectRunner{t: t, live: live}
	result, err := restore.Execute(context.Background(), runner, model.RestorePlan{Operations: fragment.Operations}, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 1 || len(result.Blocked) < 2 || len(runner.calls) != 0 {
		t.Fatalf("failed definition retained semantic authority: result=%+v calls=%v err=%v", result, runner.calls, err)
	}
}
func TestServicesExecutionPersistentOrderingNeverActivates(t *testing.T) {
	p, data, s, live := persistentPlanFixture(t)
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	s.units = nil
	fragment := persistentPlan(t, p, *data, false, false)
	journal, err := restore.NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	runner := &serviceEffectRunner{t: t, live: live}
	result, err := restore.Execute(context.Background(), runner, model.RestorePlan{Operations: fragment.Operations}, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) > 0 || len(runner.calls) != 2 || runner.calls[0] != "systemctl --user daemon-reload" || runner.calls[1] != "systemctl --user disable --no-reload -- backup.service" {
		t.Fatalf("persistent effects ordered incorrectly: calls=%v result=%+v err=%v", runner.calls, result, err)
	}
}

func TestServicesExecutionGuardedReplacementAndDeletionUseObjectHashes(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		p, data, _, live := persistentPlanFixture(t)
		if deletion {
			data.Services.Units[0].Presence, data.Services.Units[0].StartIntent = profile.ServiceAbsent, profile.ServiceStartNotManaged
		} else {
			if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/false\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		fragment := persistentPlan(t, p, *data, true, deletion)
		journal, err := restore.NewJournal(t.TempDir(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		runner := &recordingRunner{}
		result, err := restore.Execute(context.Background(), runner, model.RestorePlan{Operations: fragment.Operations}, journal, time.Now, time.Second, nil)
		journal.Close()
		if err != nil || len(result.Failed) > 0 || len(result.Blocked) > 0 {
			t.Fatalf("guarded filesystem effect failed: deletion=%v result=%+v err=%v", deletion, result, err)
		}
		if deletion {
			if _, err := os.Stat(live); !os.IsNotExist(err) {
				t.Fatalf("Exact failed to remove matching artifact: %v", err)
			}
		} else {
			bytes, err := os.ReadFile(live)
			if err != nil || !strings.Contains(string(bytes), "/usr/bin/true") {
				t.Fatalf("Force did not restore desired bytes: %s err=%v", bytes, err)
			}
		}
	}
}
