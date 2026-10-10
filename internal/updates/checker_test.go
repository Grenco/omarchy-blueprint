package updates

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeSource struct {
	id      string
	release Release
	err     error
	calls   int
}

func (f *fakeSource) ID() string {
	if f.id == "" {
		return "fake"
	}
	return f.id
}

func (f *fakeSource) Latest(context.Context) (Release, error) {
	f.calls++
	return f.release, f.err
}

func release(version string) Release {
	v, err := ParseVersion(version)
	if err != nil {
		panic(err)
	}
	return Release{Version: v, DetailsURL: "https://github.com/Grenco/omarchy-blueprint/releases/tag/v" + version, Instructions: "update via the release page"}
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func checker(t *testing.T, current string, source *fakeSource) (Checker, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	return Checker{Current: current, Source: source, Cache: Cache{Path: CachePath(t.TempDir())}, Now: c.Now}, c
}

func TestCheckAnnouncesANewerRelease(t *testing.T) {
	check, _ := checker(t, "0.1.1", &fakeSource{release: release("0.1.2")})
	notice, ok := check.Check(context.Background())
	if !ok || notice.Current != "v0.1.1" || notice.Latest != "v0.1.2" || notice.DetailsURL == "" || notice.Instructions == "" {
		t.Fatalf("notice = %+v ok=%t", notice, ok)
	}
	prerelease, _ := checker(t, "0.2.0-rc.1", &fakeSource{release: release("0.2.0")})
	if _, ok := prerelease.Check(context.Background()); !ok {
		t.Fatal("a release newer than the running prerelease was not announced")
	}
}

func TestCheckIsSilentForTheSameOrAnOlderRelease(t *testing.T) {
	for _, latest := range []string{"0.1.1", "0.1.0", "0.0.9"} {
		check, _ := checker(t, "0.1.1", &fakeSource{release: release(latest)})
		if notice, ok := check.Check(context.Background()); ok {
			t.Fatalf("latest %s announced to 0.1.1: %+v", latest, notice)
		}
	}
}

func TestCheckNeverContactsTheSourceForAnUnversionedBuild(t *testing.T) {
	for _, current := range []string{"dev", "", "(devel)", "0.1"} {
		source := &fakeSource{release: release("9.9.9")}
		check, _ := checker(t, current, source)
		if _, ok := check.Check(context.Background()); ok || source.calls != 0 {
			t.Fatalf("current %q: ok=%t calls=%d", current, ok, source.calls)
		}
	}
}

func TestCheckIsSilentWhenTheSourceFails(t *testing.T) {
	source := &fakeSource{err: errors.New("dial tcp: network is unreachable")}
	check, clock := checker(t, "0.1.1", source)
	if _, ok := check.Check(context.Background()); ok {
		t.Fatal("a failed check showed a notice")
	}
	clock.now = clock.now.Add(RetryInterval - time.Minute)
	if _, ok := check.Check(context.Background()); ok || source.calls != 1 {
		t.Fatalf("failure was not cached: calls=%d", source.calls)
	}
	source.err, source.release = nil, release("0.1.2")
	clock.now = clock.now.Add(2 * time.Minute)
	if _, ok := check.Check(context.Background()); !ok || source.calls != 2 {
		t.Fatalf("retry after a failure did not happen: calls=%d", source.calls)
	}
}

func TestCheckCachesForADay(t *testing.T) {
	source := &fakeSource{release: release("0.1.2")}
	check, clock := checker(t, "0.1.1", source)
	check.Check(context.Background())
	clock.now = clock.now.Add(CheckInterval - time.Minute)
	if notice, ok := check.Check(context.Background()); !ok || notice.Latest != "v0.1.2" || source.calls != 1 {
		t.Fatalf("cache hit: ok=%t calls=%d", ok, source.calls)
	}
	source.release = release("0.1.3")
	clock.now = clock.now.Add(2 * time.Minute)
	if notice, ok := check.Check(context.Background()); !ok || notice.Latest != "v0.1.3" || source.calls != 2 {
		t.Fatalf("expired cache was reused: %+v calls=%d", notice, source.calls)
	}
	// The cache is per source, and an upgraded binary re-compares the
	// cached release against its own version without a request.
	check.Current = "0.1.3"
	if _, ok := check.Check(context.Background()); ok || source.calls != 2 {
		t.Fatalf("after upgrading: ok=%t calls=%d", ok, source.calls)
	}
	other := &fakeSource{id: "aur", release: release("0.1.4")}
	check.Source = other
	if _, ok := check.Check(context.Background()); !ok || other.calls != 1 {
		t.Fatal("a cache from another source was reused")
	}
}

func TestCheckTreatsAMissingCorruptOrFutureCacheAsAMiss(t *testing.T) {
	source := &fakeSource{release: release("0.1.2")}
	check, clock := checker(t, "0.1.1", source)
	if err := os.MkdirAll(filepath.Dir(check.Cache.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(check.Cache.Path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := check.Check(context.Background()); !ok || source.calls != 1 {
		t.Fatalf("corrupt cache: calls=%d", source.calls)
	}
	clock.now = clock.now.Add(-48 * time.Hour) // the clock went backwards
	if _, ok := check.Check(context.Background()); !ok || source.calls != 2 {
		t.Fatalf("a cache from the future was trusted: calls=%d", source.calls)
	}
}

func TestCheckSurvivesAnUnwritableCache(t *testing.T) {
	source := &fakeSource{release: release("0.1.2")}
	check, _ := checker(t, "0.1.1", source)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	check.Cache = Cache{Path: filepath.Join(blocker, "update-check.json")}
	if _, ok := check.Check(context.Background()); !ok {
		t.Fatal("an unwritable cache hid the notice")
	}
}

func TestCheckCancelledIsNeitherResultNorFailure(t *testing.T) {
	source := &fakeSource{err: context.Canceled}
	check, _ := checker(t, "0.1.1", source)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := check.Check(ctx); ok {
		t.Fatal("cancelled check showed a notice")
	}
	if _, err := os.Stat(check.Cache.Path); !os.IsNotExist(err) {
		t.Fatal("a cancelled check was cached as a failure")
	}
}
