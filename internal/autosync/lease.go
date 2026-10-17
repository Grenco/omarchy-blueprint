package autosync

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/localstate"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
)

// A manual Sync review a person is reading holds no profile lock, so it
// holds a review lease instead: scheduled checks skip fetching while any
// live lease exists. Leases lapse on their own when the review stops
// heartbeating (an unclean exit), and one from an exited process is ignored.
const (
	LeaseTTL       = 90 * time.Second
	LeaseHeartbeat = 30 * time.Second
	leaseDir       = "review-leases"
	leaseSuffix    = ".lease"
	leaseLimit     = 1 << 10
	leaseScan      = 64
)

type leaseRecord struct {
	PID     int   `json:"pid"`
	Expires int64 `json:"expires"`
}

// LeaseStore finds and creates review leases for one profile.
type LeaseStore struct {
	StateHome, ProfileRoot string
}

func (s LeaseStore) dir() (string, error) {
	dir, err := machine.ProfileStateDir(s.StateHome, s.ProfileRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, leaseDir), nil
}

// ReviewLease is one review's lease.
type ReviewLease struct {
	home, path string
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (r leaseRecord) live(now time.Time) bool {
	expires := time.Unix(r.Expires, 0)
	return expires.After(now) && !expires.After(now.Add(2*LeaseTTL)) && pidAlive(r.PID)
}

// leases reads the trusted lease files. Untrusted, symlinked, malformed or
// oversized files are skipped.
func (s LeaseStore) leases() (map[string]leaseRecord, error) {
	dir, err := s.dir()
	if err != nil {
		return nil, err
	}
	if err := localstate.InspectPrivateDir(s.StateHome, dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		// A lease directory others can write is not trusted to stop checks.
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]leaseRecord{}
	for i, entry := range entries {
		if i >= leaseScan {
			break
		}
		if !strings.HasSuffix(entry.Name(), leaseSuffix) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := localstate.ReadPrivateFile(path, leaseLimit)
		if err != nil {
			continue
		}
		var record leaseRecord
		if json.Unmarshal(data, &record) != nil {
			out[path] = leaseRecord{}
			continue
		}
		out[path] = record
	}
	return out, nil
}

// Live reports whether any manual review currently holds a lease. It never
// creates state.
func (s LeaseStore) Live(now time.Time) (bool, error) {
	leases, err := s.leases()
	if err != nil {
		return false, err
	}
	for _, record := range leases {
		if record.live(now) {
			return true, nil
		}
	}
	return false, nil
}

// Hold starts a lease for this process and prunes expired ones.
func (s LeaseStore) Hold(now time.Time) (*ReviewLease, error) {
	dir, err := s.dir()
	if err != nil {
		return nil, err
	}
	if err := localstate.PrivateDir(s.StateHome, dir); err != nil {
		return nil, err
	}
	if leases, err := s.leases(); err == nil {
		for path, record := range leases {
			if !record.live(now) {
				_ = localstate.RemoveFile(s.StateHome, path)
			}
		}
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	lease := &ReviewLease{home: s.StateHome, path: filepath.Join(dir, hex.EncodeToString(token)+leaseSuffix)}
	if err := lease.Heartbeat(now); err != nil {
		return nil, err
	}
	return lease, nil
}

// Heartbeat extends the lease.
func (l *ReviewLease) Heartbeat(now time.Time) error {
	data, err := json.Marshal(leaseRecord{PID: os.Getpid(), Expires: now.Add(LeaseTTL).Unix()})
	if err != nil {
		return err
	}
	return localstate.WriteFile(l.path, data)
}

// Release ends the lease. Releasing twice is harmless.
func (l *ReviewLease) Release() error {
	if l == nil {
		return nil
	}
	return localstate.RemoveFile(l.home, l.path)
}
