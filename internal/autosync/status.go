package autosync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/localstate"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
)

// Outcome is the result of one check. It is reported, never a setting, and
// a check is never a completed Sync.
type Outcome string

const (
	OutcomeUpToDate    Outcome = "up-to-date"
	OutcomeApplied     Outcome = "applied-and-verified"
	OutcomeNeedsReview Outcome = "needs-review"
	OutcomeBusy        Outcome = "busy"
	OutcomeOffline     Outcome = "offline"
	OutcomeSignIn      Outcome = "sign-in-needed"
	OutcomeIncomplete  Outcome = "incomplete"
	OutcomeError       Outcome = "error"
)

func (o Outcome) valid() bool {
	switch o {
	case OutcomeUpToDate, OutcomeApplied, OutcomeNeedsReview, OutcomeBusy, OutcomeOffline, OutcomeSignIn, OutcomeIncomplete, OutcomeError:
		return true
	}
	return false
}

// Counts are safe totals; no target names are kept.
type Counts struct {
	Automatic   int `json:"automatic"`
	NeedsReview int `json:"needs_review"`
	Matching    int `json:"matching"`
	Preserved   int `json:"preserved"`
}

// Report is one check, as stored in status and history. Reasons are typed
// codes only.
type Report struct {
	At        time.Time `json:"at"`
	Outcome   Outcome   `json:"outcome"`
	Reasons   []string  `json:"reasons,omitempty"`
	Counts    Counts    `json:"counts"`
	Fetched   bool      `json:"fetched"`
	Scheduled bool      `json:"scheduled"`
}

// Notified remembers the last notification so identical states coalesce.
type Notified struct {
	Key string    `json:"key"`
	At  time.Time `json:"at"`
}

// Status is the machine-local record of checks. It is informative, never
// mutation authority, and kept apart from the grant and the Sync cursor.
type Status struct {
	Version   int       `json:"version"`
	LastCheck *Report   `json:"last_check,omitempty"`
	Notified  *Notified `json:"notified,omitempty"`
	History   []Report  `json:"history,omitempty"`
	// Recovered: a stored status could not be trusted or read and was
	// treated as empty. Never persisted.
	Recovered bool `json:"-"`
}

const (
	statusFile    = "auto-sync-status.json"
	statusLock    = "auto-sync-status.lock"
	statusVersion = 1
	statusLimit   = 64 << 10
	historyLimit  = 50
	historyAge    = 30 * 24 * time.Hour
	reasonLimit   = 16
)

var reasonCode = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// sanitize keeps only typed reason codes, bounded in number.
func (r Report) sanitize() Report {
	var reasons []string
	for _, reason := range r.Reasons {
		if reasonCode.MatchString(reason) && len(reasons) < reasonLimit {
			reasons = append(reasons, reason)
		}
	}
	r.Reasons = reasons
	r.At = r.At.UTC().Truncate(time.Second)
	return r
}

// StatusStore keeps status beside the grant in the profile-keyed private
// state directory.
type StatusStore struct {
	StateHome, ProfileRoot string
}

func (s StatusStore) dir() (string, error) {
	return machine.ProfileStateDir(s.StateHome, s.ProfileRoot)
}

// Load returns the stored status. Missing state is empty; unreadable,
// foreign, symlinked, oversized or malformed status is empty with Recovered
// set, so a corrupt history never blocks checks.
func (s StatusStore) Load() (Status, error) {
	dir, err := s.dir()
	if err != nil {
		return Status{}, err
	}
	empty := Status{Version: statusVersion}
	if err := localstate.InspectPrivateDir(s.StateHome, dir); errors.Is(err, os.ErrNotExist) {
		return empty, nil
	} else if err != nil {
		empty.Recovered = true
		return empty, nil
	}
	data, err := localstate.ReadPrivateFile(filepath.Join(dir, statusFile), statusLimit)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	var st Status
	if err != nil || json.Unmarshal(data, &st) != nil || !st.valid() {
		empty.Recovered = true
		return empty, nil
	}
	return st, nil
}

func (st Status) valid() bool {
	if st.Version != statusVersion || len(st.History) > historyLimit {
		return false
	}
	if st.LastCheck != nil && !st.LastCheck.Outcome.valid() {
		return false
	}
	for _, r := range st.History {
		if !r.Outcome.valid() {
			return false
		}
	}
	return true
}

// Record stores report as the last check and the newest history entry.
func (s StatusStore) Record(report Report) (Status, error) {
	report = report.sanitize()
	return s.update(func(st *Status) {
		st.LastCheck = &report
		st.History = append([]Report{report}, st.History...)
	}, report.At)
}

// MarkNotified records that a notification for key was shown.
func (s StatusStore) MarkNotified(key string, at time.Time) (Status, error) {
	return s.update(func(st *Status) { st.Notified = &Notified{Key: key, At: at.UTC().Truncate(time.Second)} }, at)
}

func (s StatusStore) update(change func(*Status), now time.Time) (Status, error) {
	dir, err := s.dir()
	if err != nil {
		return Status{}, err
	}
	if err := localstate.PrivateDir(s.StateHome, dir); err != nil {
		return Status{}, err
	}
	unlock, err := lockFile(filepath.Join(dir, statusLock))
	if err != nil {
		return Status{}, err
	}
	defer unlock()
	st, err := s.Load()
	if err != nil {
		return Status{}, err
	}
	st.Recovered = false
	change(&st)
	st.trim(now)
	data, err := json.Marshal(st)
	for err == nil && len(data) > statusLimit && len(st.History) > 0 {
		st.History = st.History[:len(st.History)-1]
		data, err = json.Marshal(st)
	}
	if err != nil {
		return Status{}, err
	}
	if err := localstate.WriteFile(filepath.Join(dir, statusFile), data); err != nil {
		return Status{}, err
	}
	return st, nil
}

func (st *Status) trim(now time.Time) {
	kept := st.History[:0]
	for _, r := range st.History {
		if now.Sub(r.At) <= historyAge && len(kept) < historyLimit {
			kept = append(kept, r)
		}
	}
	st.History = kept
}
