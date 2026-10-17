package autosync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
)

type storeFixture struct {
	t     *testing.T
	store Store
	root  string
	dir   string
}

func newStore(t *testing.T) storeFixture {
	t.Helper()
	root := t.TempDir()
	canonical, err := machine.CanonicalProfileRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	f := storeFixture{t: t, store: Store{StateHome: t.TempDir(), ProfileRoot: root}, root: canonical}
	f.dir, err = machine.ProfileStateDir(f.store.StateHome, root)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f storeFixture) proposal(preset Preset, categories ...string) Grant {
	g := DefaultGrant()
	g.Setting, g.Preset, g.Identity = SettingOn, preset, testIdentity(f.root)
	g.Categories = PresetScope(categories)
	return g
}

func (f storeFixture) enable(next Grant) Grant {
	f.t.Helper()
	base, _, err := f.store.Load()
	if err != nil {
		f.t.Fatal(err)
	}
	approval, err := ApproveLocally(base, next, &confirmer{interactive: true, answer: true})
	if err != nil {
		f.t.Fatal(err)
	}
	saved, err := f.store.Save(base, next, &approval)
	if err != nil {
		f.t.Fatal(err)
	}
	return saved
}

func TestStoreDefaultsOffAndNeverCreatesStateOnLoad(t *testing.T) {
	f := newStore(t)
	g, exists, err := f.store.Load()
	if err != nil || exists || g.Setting != SettingOff || g.Active() {
		t.Fatalf("missing grant: %+v %v %v", g, exists, err)
	}
	if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
		t.Fatalf("Load created state: %v", err)
	}
	// A grant-shaped file inside the profile checkout is never read.
	if err := os.WriteFile(filepath.Join(f.store.ProfileRoot, grantFile), []byte("version = 1\nsetting = \"on\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if g, exists, _ := f.store.Load(); exists || g.Setting != SettingOff {
		t.Fatal("grant loaded from the profile checkout")
	}
}

func TestStoreRoundTripPrivateAtomic(t *testing.T) {
	f := newStore(t)
	saved := f.enable(f.proposal(PresetBalanced, "themes", "defaults"))
	loaded, exists, err := f.store.Load()
	if err != nil || !exists || loaded.AuthorityDigest() != saved.AuthorityDigest() || loaded.Generation != 1 || loaded.Approved != saved.AuthorityDigest() {
		t.Fatalf("round trip: %+v %v %v", loaded, exists, err)
	}
	info, err := os.Stat(filepath.Join(f.dir, grantFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("grant file mode: %v %v", info.Mode(), err)
	}
	dirInfo, _ := os.Stat(f.dir)
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("state dir mode %v", dirInfo.Mode())
	}
	entries, _ := os.ReadDir(f.dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
	data, _ := os.ReadFile(filepath.Join(f.dir, grantFile))
	for _, secret := range []string{"https://", "token", "password"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("grant serialised %q", secret)
		}
	}
}

func TestStoreFailsClosedOnUntrustedFiles(t *testing.T) {
	cases := map[string]func(f storeFixture){
		"malformed":       func(f storeFixture) { os.WriteFile(filepath.Join(f.dir, grantFile), []byte("version = ["), 0o600) },
		"unknown-field":   func(f storeFixture) { appendFile(f, "\nsurprise = true\n") },
		"unknown-version": func(f storeFixture) { rewrite(f, "version = 1", "version = 9") },
		"bad-category":    func(f storeFixture) { appendFile(f, "\n[categories.wallpaper]\nall_targets = true\n") },
		"oversize":        func(f storeFixture) { os.WriteFile(filepath.Join(f.dir, grantFile), make([]byte, grantLimit+1), 0o600) },
		"symlink-file": func(f storeFixture) {
			other := filepath.Join(f.t.TempDir(), "elsewhere.toml")
			data, _ := os.ReadFile(filepath.Join(f.dir, grantFile))
			os.WriteFile(other, data, 0o600)
			os.Remove(filepath.Join(f.dir, grantFile))
			os.Symlink(other, filepath.Join(f.dir, grantFile))
		},
		"directory": func(f storeFixture) {
			os.Remove(filepath.Join(f.dir, grantFile))
			os.Mkdir(filepath.Join(f.dir, grantFile), 0o700)
		},
		"fifo": func(f storeFixture) {
			os.Remove(filepath.Join(f.dir, grantFile))
			if err := syscall.Mkfifo(filepath.Join(f.dir, grantFile), 0o600); err != nil {
				f.t.Skip(err)
			}
		},
		"other-profile": func(f storeFixture) {
			rewrite(f, "profile_root = '"+f.root+"'", "profile_root = '/somewhere/else'")
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newStore(t)
			f.enable(f.proposal(PresetConservative, "themes"))
			corrupt(f)
			g, exists, err := f.store.Load()
			if !errors.Is(err, ErrInvalidGrant) || exists || g.Active() {
				t.Fatalf("untrusted grant accepted: %+v %v %v", g, exists, err)
			}
		})
	}
}

func appendFile(f storeFixture, text string) {
	file, err := os.OpenFile(filepath.Join(f.dir, grantFile), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	defer file.Close()
	file.WriteString(text)
}

func rewrite(f storeFixture, old, new string) {
	path := filepath.Join(f.dir, grantFile)
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), old) {
		f.t.Fatalf("fixture text %q not found in:\n%s", old, data)
	}
	os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o600)
}

func TestStoreRefusesSymlinkedStateDirectory(t *testing.T) {
	f := newStore(t)
	parent := filepath.Dir(f.dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), f.dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.Load(); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("symlinked state dir: %v", err)
	}
	next := f.proposal(PresetConservative, "themes")
	approval, _ := ApproveLocally(DefaultGrant(), next, &confirmer{interactive: true, answer: true})
	if _, err := f.store.Save(DefaultGrant(), next, &approval); err == nil {
		t.Fatal("saved through a symlinked state directory")
	}
}

func TestWideningNeedsLocalApproval(t *testing.T) {
	f := newStore(t)
	next := f.proposal(PresetConservative, "themes")
	if _, err := f.store.Save(DefaultGrant(), next, nil); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("enabled without approval: %v", err)
	}
	if _, err := ApproveLocally(DefaultGrant(), next, &confirmer{interactive: false, answer: true}); !errors.Is(err, ErrNotInteractive) {
		t.Fatalf("non-interactive approval: %v", err)
	}
	if _, err := ApproveLocally(DefaultGrant(), next, &confirmer{interactive: true, answer: false}); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("declined approval: %v", err)
	}
	other := f.proposal(PresetHandsOff, "themes")
	wrong, _ := ApproveLocally(DefaultGrant(), other, &confirmer{interactive: true, answer: true})
	if _, err := f.store.Save(DefaultGrant(), next, &wrong); !errors.Is(err, ErrApprovalMismatch) {
		t.Fatalf("approval for another authority accepted: %v", err)
	}
	base := f.enable(next)

	widen := map[string]func(Grant) Grant{
		"preset":   func(g Grant) Grant { g.Preset = PresetBalanced; return g },
		"category": func(g Grant) Grant { g.Categories = PresetScope([]string{"themes", "config"}); return g },
		"run-code": func(g Grant) Grant {
			g.Preset = PresetHandsOff
			g.RunCode = map[string]RunCodeScope{"themes": {}}
			return g
		},
		"privileged": func(g Grant) Grant { g.Preset = PresetHandsOff; g.PrivilegedInstall = true; return g },
		"undeny": func(g Grant) Grant {
			g.Categories = map[string]CategoryScope{"themes": {AllTargets: true}}
			return g
		},
	}
	denied := base
	denied.Categories = map[string]CategoryScope{"themes": {AllTargets: true, DenyTargets: []string{"theme:aether"}}}
	base, err := f.store.Save(base, denied, nil)
	if err != nil {
		t.Fatalf("narrowing needed approval: %v", err)
	}
	for name, mutate := range widen {
		if _, err := f.store.Save(base, mutate(base), nil); !errors.Is(err, ErrApprovalRequired) {
			t.Fatalf("%s widened without approval: %v", name, err)
		}
	}
	c := &confirmer{interactive: true, answer: true}
	runCode := widen["run-code"](base)
	approval, _ := ApproveLocally(base, runCode, c)
	if got := c.seen[len(c.seen)-1]; !hasExpansion(got, ExpandRunCode, "themes") {
		t.Fatalf("confirmation did not name the new run-code category: %+v", got)
	}
	if _, err := f.store.Save(base, runCode, &approval); err != nil {
		t.Fatal(err)
	}
}

func TestPauseResumeOffAndSuspension(t *testing.T) {
	f := newStore(t)
	base := f.enable(f.proposal(PresetBalanced, "themes"))
	paused := base
	paused.Setting = SettingPaused
	base, err := f.store.Save(base, paused, nil)
	if err != nil {
		t.Fatalf("pause needed approval: %v", err)
	}
	resumed := base
	resumed.Setting = SettingOn
	if base, err = f.store.Save(base, resumed, nil); err != nil {
		t.Fatalf("resume of the approved authority needed approval: %v", err)
	}
	// Resuming with wider authority is not a resume.
	paused = base
	paused.Setting = SettingPaused
	base, _ = f.store.Save(base, paused, nil)
	wider := base
	wider.Setting, wider.Preset = SettingOn, PresetHandsOff
	if _, err := f.store.Save(base, wider, nil); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("resume widened silently: %v", err)
	}
	suspended := base
	suspended.Setting = SettingOn
	suspended.Suspended = &Suspension{Reason: "incomplete"}
	if base, err = f.store.Save(base, suspended, nil); err != nil {
		t.Fatalf("setting the guard needed approval: %v", err)
	}
	cleared := base
	cleared.Suspended = nil
	if _, err := f.store.Save(base, cleared, nil); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("guard lifted without a person: %v", err)
	}
	off := base
	off.Setting = SettingOff
	if _, err := f.store.Save(base, off, nil); err != nil {
		t.Fatalf("turning off needed approval: %v", err)
	}
}

func TestStaleWriterRefused(t *testing.T) {
	f := newStore(t)
	base := f.enable(f.proposal(PresetConservative, "themes"))
	first := base
	first.Setting = SettingPaused
	if _, err := f.store.Save(base, first, nil); err != nil {
		t.Fatal(err)
	}
	second := base
	second.Setting = SettingOff
	if _, err := f.store.Save(base, second, nil); !errors.Is(err, ErrConcurrentChange) {
		t.Fatalf("stale writer accepted: %v", err)
	}
}

func TestCopiedGrantDoesNotAuthoriseAnotherHost(t *testing.T) {
	f := newStore(t)
	saved := f.enable(f.proposal(PresetConservative, "themes"))
	elsewhere := testIdentity(f.root)
	elsewhere.HostID = strings.Repeat("c", 64)
	result := Evaluate(saved, elsewhere, testRegistry(), nil, []OperationInput{themeCopy("garden")})
	if result.Eligible || result.Decisions[0].Reason != ReasonIdentityMismatch {
		t.Fatalf("copied grant authorised another host: %+v", result.Decisions)
	}
	if Evaluate(saved, testIdentity(f.root), testRegistry(), nil, []OperationInput{themeCopy("garden")}).Decisions[0].Reason != ReasonEligible {
		t.Fatal("grant did not authorise its own host")
	}
}

func TestHostIdentityIsOneWay(t *testing.T) {
	raw := "0123456789abcdef0123456789abcdef"
	id, err := HostIdentity(func(path string) ([]byte, error) {
		if path == "/etc/machine-id" {
			return []byte(raw + "\n"), nil
		}
		return nil, os.ErrNotExist
	})
	if err != nil || len(id) != 64 || strings.Contains(id, raw) {
		t.Fatalf("host identity %q %v", id, err)
	}
	fallback, err := HostIdentity(func(path string) ([]byte, error) {
		if path == "/var/lib/dbus/machine-id" {
			return []byte(raw), nil
		}
		return nil, os.ErrNotExist
	})
	if err != nil || fallback != id {
		t.Fatalf("fallback %q %v", fallback, err)
	}
	if _, err := HostIdentity(func(string) ([]byte, error) { return []byte("not-an-id"), nil }); err == nil {
		t.Fatal("invalid machine ID accepted")
	}
}

func TestGrantValidation(t *testing.T) {
	registry := DefaultRegistry()
	valid := activeGrant("/profile", PresetBalanced, "themes")
	cases := map[string]func(Grant) Grant{
		"run-code-in-balanced":   func(g Grant) Grant { g.RunCode = map[string]RunCodeScope{"themes": {}}; return g },
		"privileged-in-balanced": func(g Grant) Grant { g.PrivilegedInstall = true; return g },
		"unknown-family": func(g Grant) Grant {
			g.Categories["themes"] = CategoryScope{DenyFamilies: []string{"themes.teleport"}}
			return g
		},
		"cross-category-family": func(g Grant) Grant {
			g.Categories["themes"] = CategoryScope{AllowFamilies: []string{"hooks.write"}}
			return g
		},
		"control-char-target": func(g Grant) Grant {
			g.Categories["themes"] = CategoryScope{Targets: []string{"theme:\x1b[31m"}}
			return g
		},
		"interval":               func(g Grant) Grant { g.IntervalMinutes = 1; return g },
		"custom-class-in-preset": func(g Grant) Grant { g.Categories["themes"] = CategoryScope{MaxClass: ClassHandsOff}; return g },
		"relative-root":          func(g Grant) Grant { g.Identity.ProfileRoot = "profile"; return g },
		"raw-host":               func(g Grant) Grant { g.Identity.HostID = "0123456789abcdef0123456789abcdef"; return g },
		"too-many-targets": func(g Grant) Grant {
			targets := make([]string, maxTargets+1)
			for i := range targets {
				targets[i] = "theme:x"
			}
			g.Categories["themes"] = CategoryScope{Targets: targets}
			return g
		},
	}
	if err := valid.Validate(registry); err != nil {
		t.Fatalf("valid grant rejected: %v", err)
	}
	for name, mutate := range cases {
		g := activeGrant("/profile", PresetBalanced, "themes")
		if err := mutate(g).Validate(registry); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestAuthorityDigestIsOrderIndependent(t *testing.T) {
	a := activeGrant("/profile", PresetCustom)
	a.Categories = map[string]CategoryScope{"themes": {Targets: []string{"theme:a", "theme:b"}, MaxClass: ClassConservative}}
	b := a
	b.Categories = map[string]CategoryScope{"themes": {Targets: []string{"theme:b", "theme:a"}, MaxClass: ClassConservative}}
	b.Generation, b.Setting = 9, SettingPaused
	if a.AuthorityDigest() != b.AuthorityDigest() {
		t.Fatal("digest depends on order or non-authority fields")
	}
	b.Categories["themes"] = CategoryScope{Targets: []string{"theme:a"}, MaxClass: ClassConservative}
	if a.AuthorityDigest() == b.AuthorityDigest() {
		t.Fatal("digest ignored a scope change")
	}
}

// The grant is authority: a file or state directory that another user could
// write (or read, for the file) never authorises anything. The digest is a
// consistency check, not a signature, so permissions are the protection.
func TestStoreRejectsUnsafePermissions(t *testing.T) {
	cases := map[string]func(f storeFixture){
		"grant-group-readable": func(f storeFixture) { os.Chmod(filepath.Join(f.dir, grantFile), 0o640) },
		"grant-world-writable": func(f storeFixture) { os.Chmod(filepath.Join(f.dir, grantFile), 0o666) },
		"state-dir-group-writable": func(f storeFixture) {
			os.Chmod(f.dir, 0o770)
		},
		"parent-dir-world-writable": func(f storeFixture) {
			os.Chmod(filepath.Dir(f.dir), 0o777)
		},
	}
	for name, loosen := range cases {
		t.Run(name, func(t *testing.T) {
			f := newStore(t)
			f.enable(f.proposal(PresetConservative, "themes"))
			loosen(f)
			g, exists, err := f.store.Load()
			if !errors.Is(err, ErrInvalidGrant) || exists || g.Active() {
				t.Fatalf("grant with unsafe permissions accepted: %+v %v %v", g, exists, err)
			}
		})
	}
}

// An approval is bound to the grant the person was shown as "before", not
// only to the proposal: a confirmation described against another starting
// point (one that hid an expansion, or a since-replaced grant) is refused.
func TestApprovalIsBoundToTheConfirmedBase(t *testing.T) {
	f := newStore(t)
	next := f.proposal(PresetHandsOff, "themes", "hooks")
	next.RunCode = map[string]RunCodeScope{"hooks": {}}

	// Shown against a base that already allowed Hooks to run code, the
	// confirmation names no run-code expansion; the stored grant is off.
	shown := next
	shown.Setting = SettingOff
	c := &confirmer{interactive: true, answer: true}
	approval, err := ApproveLocally(shown, next, c)
	if err != nil {
		t.Fatal(err)
	}
	if hasExpansion(c.seen[0], ExpandRunCode, "hooks") {
		t.Fatal("fixture: the hidden base should not show the run-code expansion")
	}
	if _, err := f.store.Save(DefaultGrant(), next, &approval); !errors.Is(err, ErrApprovalMismatch) {
		t.Fatalf("approval described against another base accepted: %v", err)
	}

	// Confirmed against the stored grant, then the stored grant changes:
	// the approval no longer describes the change being saved.
	base := f.enable(f.proposal(PresetConservative, "themes"))
	widened := f.proposal(PresetBalanced, "themes")
	approval, err = ApproveLocally(base, widened, &confirmer{interactive: true, answer: true})
	if err != nil {
		t.Fatal(err)
	}
	narrowed := base
	narrowed.Categories = map[string]CategoryScope{"themes": {AllTargets: true, DenyTargets: []string{"theme:aether"}}}
	current, err := f.store.Save(base, narrowed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Save(current, widened, &approval); !errors.Is(err, ErrApprovalMismatch) {
		t.Fatalf("approval for a replaced base accepted: %v", err)
	}
	if _, err := f.store.Save(base, widened, &approval); !errors.Is(err, ErrConcurrentChange) {
		t.Fatalf("stale base accepted: %v", err)
	}
}
