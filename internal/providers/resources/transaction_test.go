package resources

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestPreparedCaptureRollbackRestoresPreviousGeneration(t *testing.T) {
	parent := transactionGeneration(t, "old")
	c := transactionPrepared(t, parent, "new")
	if err := c.Install(); err != nil {
		t.Fatal(err)
	}
	if err := c.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertTransactionGeneration(t, parent, "old")
}

func TestPreparedCaptureFinalizeKeepsNewGenerationAndCleansDebris(t *testing.T) {
	parent := transactionGeneration(t, "old")
	c := transactionPrepared(t, parent, "new")
	if err := c.Install(); err != nil {
		t.Fatal(err)
	}
	if err := c.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := c.Finalize(); err != nil {
		t.Fatal(err)
	}
	assertTransactionGeneration(t, parent, "new")
	for _, name := range []string{".resources.toml-previous", ".files-previous", ".git-state-previous", ".capture-transaction"} {
		if _, err := os.Lstat(filepath.Join(parent, name)); !os.IsNotExist(err) {
			t.Fatalf("debris %s: %v", name, err)
		}
	}
}

func TestPreparedCaptureInstallFailureRollsBackAllComponents(t *testing.T) {
	parent := transactionGeneration(t, "old")
	c := transactionPrepared(t, parent, "new")
	original := renameCapturePath
	t.Cleanup(func() { renameCapturePath = original })
	failed := false
	renameCapturePath = func(old, new string) error {
		if filepath.Base(new) == "git-state" && !failed {
			failed = true
			return errors.New("injected rename failure")
		}
		return original(old, new)
	}
	if err := c.Install(); err == nil {
		t.Fatal("Install succeeded")
	}
	assertTransactionGeneration(t, parent, "old")
}

func TestPreparedCaptureUsesExclusiveAdvisoryLock(t *testing.T) {
	parent := transactionGeneration(t, "old")
	c := transactionPrepared(t, parent, "new")
	defer c.Rollback()
	if _, err := prepareCapture(parent, profile.Resources{}, nil); err == nil {
		t.Fatal("second capture acquired lock")
	}
}

func TestPrepareCaptureRecoversRollbackMarker(t *testing.T) {
	parent := transactionGeneration(t, "old")
	c := transactionPrepared(t, parent, "new")
	if err := c.Install(); err != nil {
		t.Fatal(err)
	}
	// Simulate a process exit after Install's rollback marker was durable.
	if err := os.WriteFile(filepath.Join(parent, ".capture-transaction"), []byte("rollback\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.close()
	recovered, err := prepareCapture(parent, profile.Resources{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Rollback()
	assertTransactionGeneration(t, parent, "old")
}

func TestPrepareCaptureRecoversCommittedMarkerAndStaleStage(t *testing.T) {
	parent := transactionGeneration(t, "old")
	c := transactionPrepared(t, parent, "new")
	if err := c.Install(); err != nil {
		t.Fatal(err)
	}
	if err := c.Commit(); err != nil {
		t.Fatal(err)
	}
	c.close()
	recovered, err := prepareCapture(parent, profile.Resources{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertTransactionGeneration(t, parent, "new")
	stale, err := os.MkdirTemp(parent, ".capture-stage-*")
	if err != nil {
		t.Fatal(err)
	}
	c, err = prepareCapture(parent, profile.Resources{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Rollback()
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale stage retained: %v", err)
	}
}

func TestPrepareCaptureRecoversInstalledGenerationByRollingBack(t *testing.T) {
	parent := transactionGeneration(t, "old")
	c := transactionPrepared(t, parent, "new")
	if err := c.Install(); err != nil {
		t.Fatal(err)
	}
	c.close()
	recovered, err := prepareCapture(parent, profile.Resources{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Rollback()
	assertTransactionGeneration(t, parent, "old")
}

func transactionGeneration(t *testing.T, value string) string {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "resources")
	for _, name := range captureComponents {
		path := filepath.Join(parent, name)
		if name != "resources.toml" {
			path = filepath.Join(path, "value")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return parent
}

func transactionPrepared(t *testing.T, parent, value string) *PreparedCapture {
	t.Helper()
	c, err := prepareCapture(parent, profile.Resources{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range captureComponents {
		path := c.stagePath(name)
		if name != "resources.toml" {
			path = filepath.Join(path, "value")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func assertTransactionGeneration(t *testing.T, parent, want string) {
	t.Helper()
	for _, name := range captureComponents {
		path := filepath.Join(parent, name)
		if name != "resources.toml" {
			path = filepath.Join(path, "value")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s=%q err=%v want=%q", name, got, err, want)
		}
	}
}
