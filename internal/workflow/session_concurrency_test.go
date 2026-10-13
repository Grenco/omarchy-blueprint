package workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// STATE-02: the TUI reads the Session (header, screens) while asynchronous
// commands mutate and reload it. Run with -race.
func TestSessionReadsAndMutationsDoNotRace(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Manifest.Capture.Packages = true
	session := newCaptureSession(t, data)
	session.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}}})
	ctx := context.Background()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	reader := func(read func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					read()
				}
			}
		}()
	}
	reader(func() { _ = session.Profile().Machines.Items; _ = session.Machine().Name })
	reader(func() {
		_, _ = session.EffectivePolicy(ctx, PolicyScope{}, "packages", TargetInspection{Key: "official:git"})
	})
	reader(func() { _, _ = session.Status(ctx, "") })
	var mutations sync.WaitGroup
	for i := range 8 {
		mutations.Add(1)
		go func() {
			defer mutations.Done()
			name := fmt.Sprintf("machine-%d", i)
			if err := session.AddMachine(ctx, name, false); err != nil {
				t.Errorf("add %s: %v", name, err)
			}
			_ = session.SetProviderCaptured(ctx, "themes", i%2 == 0)
			_ = session.Reload()
		}()
	}
	for range 4 {
		mutations.Add(1)
		go func() {
			defer mutations.Done()
			if _, err := session.CaptureMany(ctx, []string{"packages"}); err != nil {
				t.Errorf("capture: %v", err)
			}
		}()
	}
	done := make(chan struct{})
	go func() { mutations.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent mutations deadlocked")
	}
	close(stop)
	wg.Wait()
	// No lost updates: every concurrently added machine is in the profile,
	// in memory and on disk.
	if got := len(session.Profile().Machines.Items); got != 8 {
		t.Fatalf("%d of 8 concurrently added machines survived in the session", got)
	}
	saved, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Machines.Items) != 8 {
		t.Fatalf("%d of 8 concurrently added machines were saved", len(saved.Machines.Items))
	}
}

// Read APIs return snapshots: changing what they return never changes the
// Session.
func TestSessionReadsReturnSnapshots(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Official = []string{"git"}
	data.Machines.Items = []profile.Machine{{Name: "desktop"}}
	session := newCaptureSession(t, data)
	got := session.Profile()
	got.Packages.Official[0] = "changed"
	got.Machines.Items[0].Name = "changed"
	if again := session.Profile(); again.Packages.Official[0] != "git" || again.Machines.Items[0].Name != "desktop" {
		t.Fatalf("mutating a returned profile changed the Session: %v, %v", again.Packages.Official, again.Machines.Items)
	}
}

// A mutation whose save fails leaves the Session as it was.
func TestFailedMutationLeavesTheSessionUnchanged(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	if err := session.AddMachine(context.Background(), "desktop", false); err != nil {
		t.Fatal(err)
	}
	if err := session.RenameMachine(context.Background(), "desktop", "bad name with spaces/"); err == nil {
		t.Fatal("an invalid rename succeeded")
	}
	if items := session.Profile().Machines.Items; len(items) != 1 || items[0].Name != "desktop" {
		t.Fatalf("a failed rename changed the Session: %v", items)
	}
}
