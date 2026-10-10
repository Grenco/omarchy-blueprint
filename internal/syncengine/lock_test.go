package syncengine

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// An existence-only lock or unlink-on-close allows overlapping/crash-stuck executions.
func TestLockBusyReleaseAndProfileIsolation(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	first, err := AcquireProfileLock(home, root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := AcquireProfileLock(home, link); !errors.Is(err, ErrProfileBusy) {
		t.Fatal("did not lock canonical root", err)
	}
	other, err := AcquireProfileLock(home, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireProfileLock(home, root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	// Closing an old handle again must not unlock/unlink the newer holder's lock.
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireProfileLock(home, root); !errors.Is(err, ErrProfileBusy) {
		t.Fatal(err)
	}
}

func TestLockProcessCrashReleasesKernelLock(t *testing.T) {
	if os.Getenv("BLUEPRINT_TEST_LOCK_CHILD") == "1" {
		lock, err := AcquireProfileLock(os.Getenv("BLUEPRINT_TEST_STATE"), os.Getenv("BLUEPRINT_TEST_ROOT"))
		if err != nil {
			t.Fatal(err)
		}
		_ = lock // Intentionally exit without Close: kernel must release the lock.
		os.Exit(0)
	}
	home, root := t.TempDir(), t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^TestLockProcessCrashReleasesKernelLock$")
	child.Env = append(os.Environ(), "BLUEPRINT_TEST_LOCK_CHILD=1", "BLUEPRINT_TEST_STATE="+home, "BLUEPRINT_TEST_ROOT="+root)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("child: %v %s", err, out)
	}
	lock, err := AcquireProfileLock(home, root)
	if err != nil {
		t.Fatal("crashed process retained lock", err)
	}
	defer lock.Close()
}
