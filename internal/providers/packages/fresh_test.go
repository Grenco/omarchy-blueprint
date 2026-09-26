package packages

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// fakePacman installs pacman/pacman-conf/omarchy stand-ins on PATH that
// behave like Omarchy 4.0.4 measured on a fresh install: local queries
// succeed with stderr warnings for each missing sync database, native
// classification fails, and foreign classification claims every package.
type fakePacman struct {
	dbPath string
	log    string
}

func newFakePacman(t *testing.T, repos []string, synced []string, installed, explicit string) fakePacman {
	t.Helper()
	dir := t.TempDir()
	bin, dbPath := filepath.Join(dir, "bin"), filepath.Join(dir, "db")
	for _, path := range []string{bin, filepath.Join(dbPath, "sync")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, repo := range synced {
		if err := os.WriteFile(filepath.Join(dbPath, "sync", repo+".db"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(dir, "commands.log")
	warnings := ""
	for _, repo := range repos {
		warnings += "[ -e '" + filepath.Join(dbPath, "sync", repo+".db") + "' ] || echo \"warning: database file for '" + repo + "' does not exist (use '-Sy' to download)\" >&2\n"
	}
	script := func(name, body string) {
		content := "#!/bin/sh\necho \"" + name + " $*\" >> '" + log + "'\n" + body
		if err := os.WriteFile(filepath.Join(bin, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script("pacman-conf", `case "$1" in
--repo-list) printf '`+strings.Join(repos, `\n`)+`\n' ;;
DBPath) echo '`+dbPath+`/' ;;
*) exit 2 ;;
esac
`)
	script("pacman", warnings+`case "$1" in
-Qq) printf '`+installed+`' ;;
-Qqe|-Qqem) printf '`+explicit+`' ;;
-Qqen) exit 1 ;;
-Q) exit 1 ;;
*) echo "unexpected pacman $*" >&2; exit 9 ;;
esac
`)
	script("sh", `exec /bin/sh "$@"`)
	script("cat", `exec /bin/cat "$@"`)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return fakePacman{dbPath: dbPath, log: log}
}

func (f fakePacman) commands(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return string(data)
}

func TestDetectWithoutSyncDatabasesUsesOnlyLocalPackageState(t *testing.T) {
	fake := newFakePacman(t, []string{"core", "extra"}, nil, `alacritty\ngit\nlibfoo\n`, `alacritty\ngit\n`)
	got, err := (Provider{Runner: command.SystemRunner{}}).Detect(context.Background())
	if err != nil {
		t.Fatalf("fresh machine detection failed: %v", err)
	}
	if !got.OriginUnavailable || !reflect.DeepEqual(got.MissingSyncDatabases, []string{"core", "extra"}) {
		t.Fatalf("origin = %v missing = %#v", got.OriginUnavailable, got.MissingSyncDatabases)
	}
	if len(got.Official) != 0 || len(got.AUR) != 0 {
		t.Fatalf("invented classification: official=%#v aur=%#v", got.Official, got.AUR)
	}
	if !reflect.DeepEqual(got.Installed, []string{"alacritty", "git", "libfoo"}) {
		t.Fatalf("installed = %#v (stderr warnings must never become package names)", got.Installed)
	}
	if !reflect.DeepEqual(got.UnclassifiedExplicit, []string{"alacritty", "git"}) {
		t.Fatalf("unclassified explicit = %#v", got.UnclassifiedExplicit)
	}
	if log := fake.commands(t); strings.Contains(log, "-Qqen") || strings.Contains(log, "-Qqem") {
		t.Fatalf("classification queries ran without sync metadata:\n%s", log)
	}
}

func TestDetectTreatsAnyMissingConfiguredRepositoryAsUnavailable(t *testing.T) {
	newFakePacman(t, []string{"core", "extra"}, []string{"core"}, `git\n`, `git\n`)
	got, err := (Provider{Runner: command.SystemRunner{}}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.OriginUnavailable || !reflect.DeepEqual(got.MissingSyncDatabases, []string{"extra"}) {
		t.Fatalf("partial metadata must not be trusted: %v %#v", got.OriginUnavailable, got.MissingSyncDatabases)
	}
}

func TestDetectWithSyncDatabasesKeepsClassification(t *testing.T) {
	newFakePacman(t, []string{"core"}, []string{"core"}, `git\nyay\n`, `yay\n`)
	got, err := (Provider{Runner: command.SystemRunner{}}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.OriginUnavailable || len(got.MissingSyncDatabases) != 0 || len(got.UnclassifiedExplicit) != 0 {
		t.Fatalf("classification unexpectedly unavailable: %#v", got)
	}
	if !reflect.DeepEqual(got.AUR, []string{"yay"}) || len(got.Official) != 0 || !reflect.DeepEqual(got.Installed, []string{"git", "yay"}) {
		t.Fatalf("classification changed: official=%#v aur=%#v installed=%#v", got.Official, got.AUR, got.Installed)
	}
}

func TestDetectNeverRunsMutatingPackageCommands(t *testing.T) {
	fake := newFakePacman(t, []string{"core"}, nil, `git\n`, `git\n`)
	if _, err := (Provider{Runner: command.SystemRunner{}}).Detect(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(fake.commands(t)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "pacman":
			if !strings.HasPrefix(fields[1], "-Q") {
				t.Fatalf("read path ran mutating pacman command: %s", line)
			}
		case "pacman-conf", "sh", "cat":
		default:
			t.Fatalf("read path ran unexpected command: %s", line)
		}
	}
}

func TestSyncMetadataFailuresRemainErrors(t *testing.T) {
	t.Run("pacman-conf fails", func(t *testing.T) {
		runner := scriptedRunner{"pacman-conf --repo-list": {err: &command.RunError{Name: "pacman-conf", ExitCode: 1, Output: "error: config file could not be read", Err: errors.New("exit status 1")}}}
		if _, err := (Provider{Runner: runner}).Detect(context.Background()); err == nil {
			t.Fatal("pacman-conf failure was swallowed")
		}
	})
	t.Run("sync database unreadable", func(t *testing.T) {
		runner := scriptedRunner{"pacman-conf --repo-list": {out: "core\n"}, "pacman-conf DBPath": {out: "/var/lib/pacman/\n"}}
		denied := func(string) (fs.FileInfo, error) { return nil, fs.ErrPermission }
		if _, err := (Provider{Runner: runner, Stat: denied}).Detect(context.Background()); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("permission error = %v", err)
		}
	})
	t.Run("local query fails without metadata", func(t *testing.T) {
		runner := scriptedRunner{
			"pacman-conf --repo-list": {out: "core\n"}, "pacman-conf DBPath": {out: t.TempDir() + "\n"},
			"pacman -Qq": {err: &command.RunError{Name: "pacman", ExitCode: 1, Output: "error: could not open database", Err: errors.New("exit status 1")}},
		}
		if _, err := (Provider{Runner: runner}).Detect(context.Background()); err == nil {
			t.Fatal("corrupt local database was swallowed")
		}
	})
	t.Run("native query fails with metadata present", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "sync"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sync", "core.db"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		runner := scriptedRunner{
			"pacman-conf --repo-list": {out: "core\n"}, "pacman-conf DBPath": {out: dir + "\n"},
			"pacman -Qqen": {err: &command.RunError{Name: "pacman", ExitCode: 1, Output: "error: failed to initialize alpm library", Err: errors.New("exit status 1")}},
		}
		if _, err := (Provider{Runner: runner}).Detect(context.Background()); err == nil {
			t.Fatal("real classification failure was swallowed")
		}
	})
}

type scriptedResult struct {
	out string
	err error
}

type scriptedRunner map[string]scriptedResult

func (r scriptedRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	result, ok := r[name+" "+strings.Join(args, " ")]
	if !ok {
		return "", errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
	}
	return result.out, result.err
}

func freshCurrent(installed []string, explicit ...string) profile.Packages {
	return profile.Packages{Installed: installed, UnclassifiedExplicit: explicit, OriginUnavailable: true, MissingSyncDatabases: []string{"core", "extra"}}
}

func TestPresenceWithoutClassificationSatisfiesDesiredPackages(t *testing.T) {
	saved := profile.Packages{Official: []string{"git"}, AUR: []string{"yay"}}
	current := freshCurrent([]string{"git", "yay", "libfoo"}, "git", "yay")
	if changes := Diff(saved, current); len(changes) != 0 {
		t.Fatalf("installed desired packages reported as drift: %#v", changes)
	}
	if verification := Verify(saved, current); !verification.OK {
		t.Fatalf("verification = %#v", verification)
	}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("installed packages would be reinstalled: %#v", plan.Operations)
	}
}

func TestMissingDesiredPackageStillPlansInstallWithoutClassification(t *testing.T) {
	saved := profile.Packages{Official: []string{"alacritty", "git"}}
	current := freshCurrent([]string{"git"}, "git")
	if changes := Diff(saved, current); len(changes) != 1 || changes[0].Name != "alacritty" || changes[0].Type != model.ChangeRemove {
		t.Fatalf("diff = %#v", changes)
	}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Resource != "official:alacritty" {
		t.Fatalf("operations = %#v", plan.Operations)
	}
}

func TestUnclassifiedExtrasAreReportedAndNeverRemoved(t *testing.T) {
	saved := profile.Packages{Official: []string{"git"}, Absent: []profile.PackageAbsence{{Ref: "official:htop"}, {Ref: "aur:yay"}}}
	current := freshCurrent([]string{"git", "htop", "yay", "vim"}, "git", "htop", "yay", "vim")
	if changes := Diff(saved, current); len(changes) != 0 {
		t.Fatalf("unclassified extras reported as classified drift: %#v", changes)
	}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.0", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range plan.Operations {
		if op.Action == "remove" {
			t.Fatalf("unknown origin authorized removal: %#v", op)
		}
	}
	skips := map[string]string{}
	for _, skip := range plan.Skipped {
		skips[skip.Resource] = skip.Reason
	}
	for _, ref := range []string{"official:htop", "aur:yay"} {
		if !strings.Contains(skips[ref], "origin unavailable") {
			t.Fatalf("%s removal not visibly skipped: %#v", ref, plan.Skipped)
		}
	}
	if !strings.Contains(skips["packages:unclassified"], "unknown origin") || !strings.Contains(skips["packages:unclassified"], "core, extra") {
		t.Fatalf("unclassified extras not explained: %#v", plan.Skipped)
	}
	if verification := Verify(saved, current, VerifyOptions{Exact: true}); verification.OK {
		t.Fatalf("exact verification ignored installed tombstoned packages: %#v", verification)
	}
}

func TestCaptureMergePreservesDesiredOriginWhenClassificationUnavailable(t *testing.T) {
	previous := profile.Packages{Official: []string{"git", "htop"}, AUR: []string{"yay"}, Absent: []profile.PackageAbsence{{Ref: "official:nano"}}}
	current := freshCurrent([]string{"git", "vim"}, "git", "vim")
	merged := Merge(previous, current, func(string) bool { return true })
	if !reflect.DeepEqual(merged.Official, []string{"git", "htop"}) || !reflect.DeepEqual(merged.AUR, []string{"yay"}) {
		t.Fatalf("desired origin overwritten from a guess: official=%#v aur=%#v", merged.Official, merged.AUR)
	}
	if !reflect.DeepEqual(merged.Absent, []profile.PackageAbsence{{Ref: "official:nano"}}) {
		t.Fatalf("absences changed without classification: %#v", merged.Absent)
	}
}
