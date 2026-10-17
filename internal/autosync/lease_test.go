package autosync

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func leaseFixture(t *testing.T) (LeaseStore, string) {
	t.Helper()
	f := newStore(t)
	return LeaseStore{StateHome: f.store.StateHome, ProfileRoot: f.store.ProfileRoot}, f.dir
}

func TestReviewLeaseHeldHeartbeatAndReleased(t *testing.T) {
	s, dir := leaseFixture(t)
	if live, err := s.Live(t0); err != nil || live {
		t.Fatalf("no lease: %v %v", live, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("checking for a lease created state")
	}
	lease, err := s.Hold(t0)
	if err != nil {
		t.Fatal(err)
	}
	if live, _ := s.Live(t0.Add(LeaseTTL - time.Second)); !live {
		t.Fatal("held lease not live")
	}
	if live, _ := s.Live(t0.Add(LeaseTTL + time.Second)); live {
		t.Fatal("lease outlived its TTL without a heartbeat")
	}
	if err := lease.Heartbeat(t0.Add(LeaseTTL - time.Second)); err != nil {
		t.Fatal(err)
	}
	if live, _ := s.Live(t0.Add(LeaseTTL + time.Second)); !live {
		t.Fatal("heartbeat did not extend the lease")
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	if live, _ := s.Live(t0); live {
		t.Fatal("released lease still live")
	}
	if err := lease.Release(); err != nil {
		t.Fatal("second release:", err)
	}
}

// Two reviews (two terminals) each hold their own lease; releasing one
// leaves the other in force.
func TestReviewLeasesAreIndependent(t *testing.T) {
	s, _ := leaseFixture(t)
	a, _ := s.Hold(t0)
	b, _ := s.Hold(t0)
	a.Release()
	if live, _ := s.Live(t0); !live {
		t.Fatal("releasing one review dropped another's lease")
	}
	b.Release()
	if live, _ := s.Live(t0); live {
		t.Fatal("lease left behind")
	}
}

// A lease from an exited process, a far-future, foreign, symlinked or
// malformed lease is ignored: it can never stop checks for good.
func TestUntrustedOrDeadLeasesAreIgnored(t *testing.T) {
	s, dir := leaseFixture(t)
	lease, _ := s.Hold(t0)
	leases := filepath.Join(dir, leaseDir)
	write := func(name, content string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(leases, name)
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		os.Chmod(path, mode)
	}
	lease.Release()
	future := t0.Add(LeaseTTL).Unix()
	write("dead.lease", `{"pid":2147483000,"expires":`+itoa(future)+`}`, 0o600)
	write("far.lease", `{"pid":`+itoa(int64(os.Getpid()))+`,"expires":`+itoa(t0.Add(48*time.Hour).Unix())+`}`, 0o600)
	write("readable.lease", `{"pid":`+itoa(int64(os.Getpid()))+`,"expires":`+itoa(future)+`}`, 0o644)
	write("garbage.lease", `{`, 0o600)
	target := filepath.Join(t.TempDir(), "x")
	os.WriteFile(target, []byte(`{"pid":`+itoa(int64(os.Getpid()))+`,"expires":`+itoa(future)+`}`), 0o600)
	os.Symlink(target, filepath.Join(leases, "link.lease"))
	if live, err := s.Live(t0); err != nil || live {
		t.Fatalf("untrusted lease honoured: %v %v", live, err)
	}
	// Holding a new lease prunes expired ones.
	next, _ := s.Hold(t0.Add(LeaseTTL * 2))
	defer next.Release()
	if _, err := os.Stat(filepath.Join(leases, "dead.lease")); !os.IsNotExist(err) {
		t.Fatal("expired lease not pruned")
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
