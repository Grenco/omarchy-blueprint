package updates

import (
	"context"
	"time"
)

const (
	// CheckInterval is how long a successful check is reused.
	CheckInterval = 24 * time.Hour
	// RetryInterval is how long a failed check (offline, rate limited,
	// malformed response) waits before trying again.
	RetryInterval = 6 * time.Hour
)

// Checker decides whether to tell the person about a newer release. Every
// failure is silent: Check only ever reports "nothing to show".
type Checker struct {
	Current string // the running version, e.g. buildinfo.Version
	Source  Source
	Cache   Cache
	Now     func() time.Time
}

// Check returns a notice when the latest release is newer than the running
// version. It contacts the source at most once per CheckInterval (once per
// RetryInterval after a failure), and never for an unversioned build.
func (c Checker) Check(ctx context.Context) (Notice, bool) {
	current, err := ParseVersion(c.Current)
	if err != nil || c.Source == nil {
		return Notice{}, false // a development build has nothing to compare
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	entry, cached := c.Cache.load()
	fresh := cached && entry.Source == c.Source.ID() && !entry.CheckedAt.After(now())
	if fresh && entry.Failed {
		fresh = now().Sub(entry.CheckedAt) < RetryInterval
	} else if fresh {
		fresh = now().Sub(entry.CheckedAt) < CheckInterval
	}
	if !fresh {
		release, err := c.Source.Latest(ctx)
		if ctx.Err() != nil {
			return Notice{}, false // cancelled: neither a result nor a failure
		}
		entry = cacheEntry{Source: c.Source.ID(), CheckedAt: now(), Failed: err != nil}
		if err == nil {
			entry.Latest, entry.DetailsURL, entry.Instructions = release.Version.String(), release.DetailsURL, release.Instructions
		}
		_ = c.Cache.save(entry) // an unwritable cache only costs a repeat check
	}
	if entry.Failed {
		return Notice{}, false
	}
	latest, err := ParseVersion(entry.Latest)
	if err != nil || latest.Compare(current) <= 0 {
		return Notice{}, false
	}
	return Notice{Current: "v" + current.String(), Latest: "v" + latest.String(), DetailsURL: entry.DetailsURL, Instructions: entry.Instructions}, true
}
