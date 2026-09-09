package restore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func generatedHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func modePtr(value uint32) *uint32 { return &value }

func executeModeFileWrite(t *testing.T, action model.FileWrite) error {
	t.Helper()
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	return writeFileAtomic("test.write", action, journal, time.Now)
}

type delayedRunner struct{ delay time.Duration }

func (r delayedRunner) Run(context.Context, string, ...string) (string, error) {
	time.Sleep(r.delay)
	return "", nil
}

type failingRunner struct{ calls []string }

func (r *failingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name)
	if name == "validate" {
		return "", fmt.Errorf("invalid")
	}
	return "", nil
}

func TestExecuteBlocksDependentOperationsButContinuesIndependentOnes(t *testing.T) {
	j, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	runner := &failingRunner{}
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "validate", Command: []string{"validate"}}, {ID: "copy", Command: []string{"copy"}, DependsOn: []string{"validate"}}, {ID: "independent", Command: []string{"other"}}}}
	result, err := Execute(context.Background(), runner, plan, j, time.Now, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failed) != 1 || len(result.Blocked) != 1 || len(result.Completed) != 1 || result.Blocked[0].Dependency != "validate" {
		t.Fatalf("result=%#v", result)
	}
	if !reflect.DeepEqual(runner.calls, []string{"validate", "other"}) {
		t.Fatalf("calls=%#v", runner.calls)
	}
}

func TestSummarizeErrorRetainsUsefulCommandContext(t *testing.T) {
	err := fmt.Errorf("git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.")
	got := summarizeError(err)
	if !strings.Contains(got, "Permission denied (publickey)") || !strings.Contains(got, "Could not read from remote repository") {
		t.Fatalf("summary=%q", got)
	}
}

func TestExecuteReportsAndJournalsProgress(t *testing.T) {
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "packages.install.official", Provider: "packages", Action: "install",
		Items: []string{"git", "zoxide"}, Command: []string{"omarchy", "pkg", "add", "git", "zoxide"},
	}}}
	var events []ProgressType
	result, err := Execute(context.Background(), delayedRunner{delay: 15 * time.Millisecond}, plan, journal, time.Now, 2*time.Millisecond, func(event Progress) {
		events = append(events, event.Type)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 || events[0] != ProgressStarted || events[len(events)-1] != ProgressCompleted {
		t.Fatalf("events = %#v", events)
	}
	foundHeartbeat := false
	for _, event := range events {
		if event == ProgressHeartbeat {
			foundHeartbeat = true
		}
	}
	if !foundHeartbeat {
		t.Fatalf("no heartbeat in %#v", events)
	}
	if len(result.Completed) != 1 || len(result.Failed) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(journal.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "OPERATION_PROGRESS") {
		t.Fatalf("journal missing progress: %s", b)
	}
}

func TestFileWriteGeneratedContentCreatesMissingFile(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "created.json")
	data := []byte("generated content\n")
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "shell.write", File: &model.FileWrite{Generated: true, Content: data, Destination: destination, SourceHash: generatedHash(data), ExpectedMissing: true}}}}
	result, err := Execute(context.Background(), delayedRunner{}, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(data) {
		t.Fatalf("content=%q err=%v", got, err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
}

func TestFileWriteGeneratedContentPreservesExistingMode(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "existing.json")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := []byte("new")
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "shell.write", File: &model.FileWrite{Generated: true, Content: data, Destination: destination, SourceHash: generatedHash(data), ExpectedHash: hashFile(t, destination), Backup: true}}}}
	if _, err := Execute(context.Background(), delayedRunner{}, plan, journal, time.Now, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
}

func TestFileWriteForceReplacementBacksUpObjectsAndRefusesChangedTarget(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(string)
	}{
		{"directory", func(destination string) {
			if err := os.Mkdir(destination, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", func(destination string) {
			if err := os.Symlink("target", destination); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
			if err := os.WriteFile(source, []byte("desired"), 0o644); err != nil {
				t.Fatal(err)
			}
			tt.setup(destination)
			precondition := filesystemPreconditionForTest(t, destination)
			journal, err := NewJournal(t.TempDir(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			result, err := Execute(context.Background(), delayedRunner{}, model.RestorePlan{Operations: []model.Operation{{ID: "config.force", File: &model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ReplaceExisting: true, ExpectedExisting: &precondition, Backup: true, RejectSymlinkParents: true}}}}, journal, time.Now, time.Second, nil)
			if err != nil || len(result.Completed) != 1 || !result.Completed[0].Reversible {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if got, err := os.ReadFile(destination); err != nil || string(got) != "desired" {
				t.Fatalf("destination=%q err=%v", got, err)
			}
			matches, err := filepath.Glob(filepath.Join(dir, ".destination.omarchy-blueprint-backup-*"))
			if err != nil || len(matches) != 1 {
				t.Fatalf("backups=%v err=%v", matches, err)
			}
		})
	}

	dir := t.TempDir()
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("desired"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("planned"), 0o644); err != nil {
		t.Fatal(err)
	}
	precondition := filesystemPreconditionForTest(t, destination)
	if err := os.WriteFile(destination, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	result, err := Execute(context.Background(), delayedRunner{}, model.RestorePlan{Operations: []model.Operation{{ID: "config.force.changed", File: &model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ReplaceExisting: true, ExpectedExisting: &precondition, Backup: true}}}}, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "changed" {
		t.Fatalf("destination=%q err=%v", got, err)
	}
}

func TestReplacementOfSymlinkJournalsSiblingWithoutCopyingOrDereferencing(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(root, "external")
	if err := os.WriteFile(external, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "destination")
	if err := os.Symlink(external, destination); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(destination)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	action := model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ReplaceExisting: true, Backup: true, ExpectedExisting: &model.FilesystemPrecondition{Type: "symlink", Target: target, Mode: uint32(info.Mode().Perm())}}
	if err := writeFileAtomic("symlink.replace", action, journal, time.Now); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(external); err != nil || string(got) != "external" {
		t.Fatalf("external=%q err=%v", got, err)
	}
	if info, err := os.Lstat(destination); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("destination=%v err=%v", info, err)
	}
	entries, err := os.ReadFile(journal.Path)
	if err != nil || strings.Contains(string(entries), ".backup") {
		t.Fatalf("journal=%q err=%v", entries, err)
	}
}

func TestFileWriteGeneratedContentRejectsInvalidSourcesAndHash(t *testing.T) {
	cases := []model.FileWrite{
		{Generated: true, Source: "unexpected", Content: []byte("x"), SourceHash: generatedHash([]byte("x")), ExpectedMissing: true},
		{Generated: true, SourceHash: generatedHash([]byte("x")), ExpectedMissing: true},
		{Source: "", Content: []byte("x"), SourceHash: generatedHash([]byte("x")), ExpectedMissing: true},
		{Generated: true, Content: []byte("x"), SourceHash: "wrong", ExpectedMissing: true},
	}
	for _, action := range cases {
		action.Destination = filepath.Join(t.TempDir(), "target")
		journal, err := NewJournal(t.TempDir(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		result, err := Execute(context.Background(), delayedRunner{}, model.RestorePlan{Operations: []model.Operation{{ID: "shell.write", File: &action}}}, journal, time.Now, time.Second, nil)
		journal.Close()
		if err != nil || len(result.Failed) != 1 {
			t.Fatalf("action=%#v result=%#v err=%v", action, result, err)
		}
	}
}

func TestReplacementInstallDoesNotOverwriteConcurrentNewWork(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("desired"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	precondition := filesystemPreconditionForTest(t, destination)
	originalInstaller := fileWriteNoReplace
	fileWriteNoReplace = func(old, new string) error {
		if err := os.WriteFile(new, []byte("concurrent"), 0o600); err != nil {
			return err
		}
		return renameNoReplace(old, new)
	}
	defer func() { fileWriteNoReplace = originalInstaller }()
	err := executeModeFileWrite(t, model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ReplaceExisting: true, ExpectedExisting: &precondition, Backup: true})
	if err == nil {
		t.Fatal("replacement unexpectedly succeeded")
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "concurrent" {
		t.Fatalf("destination=%q err=%v", got, err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, ".destination.omarchy-blueprint-backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	if got, err := os.ReadFile(backups[0]); err != nil || string(got) != "original" {
		t.Fatalf("backup=%q err=%v", got, err)
	}
}

func TestFileWriteGeneratedContentIsOmittedFromJSON(t *testing.T) {
	action := model.FileWrite{Generated: true, Content: []byte("secret shell settings"), Destination: "/tmp/shell.json", SourceHash: "hash", ExpectedMissing: true}
	encoded, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret shell settings") || !strings.Contains(string(encoded), `"generated":true`) {
		t.Fatalf("JSON=%s", encoded)
	}
}

func TestFileWriteAppliesExplicitModeAndValidatesExpectedMode(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("desired\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if err := executeModeFileWrite(t, model.FileWrite{Source: source, Destination: missing, SourceHash: hashFile(t, source), ExpectedMissing: true, Mode: modePtr(0o755)}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(missing)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("created mode=%v err=%v", info.Mode(), err)
	}
	existing := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(existing, []byte("desired\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executeModeFileWrite(t, model.FileWrite{Source: source, Destination: existing, SourceHash: hashFile(t, source), ExpectedHash: hashFile(t, existing), ExpectedMode: modePtr(0o600), Backup: true, Mode: modePtr(0o755)}); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(existing)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("replaced mode=%v err=%v", info.Mode(), err)
	}
}

func TestFileWriteModePreconditionsRejectInvalidOrChangedDestination(t *testing.T) {
	source, destination := filepath.Join(t.TempDir(), "source"), filepath.Join(t.TempDir(), "destination")
	if err := os.WriteFile(source, []byte("desired\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, action := range []model.FileWrite{
		{Source: source, Destination: destination, SourceHash: hashFile(t, source), ExpectedHash: hashFile(t, destination), ExpectedMode: modePtr(0o600), Mode: modePtr(0o755)},
		{Source: source, Destination: destination, SourceHash: hashFile(t, source), ExpectedHash: hashFile(t, destination), Mode: modePtr(0o1000)},
		{Source: source, Destination: filepath.Join(t.TempDir(), "missing"), SourceHash: hashFile(t, source), ExpectedMissing: true, ExpectedMode: modePtr(0o644)},
	} {
		err := executeModeFileWrite(t, action)
		if err == nil {
			t.Fatalf("action=%#v succeeded", action)
		}
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "current\n" {
		t.Fatalf("destination=%q err=%v", content, err)
	}
}

func TestFileWriteRejectSymlinkParentsPreventsExternalWrite(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("desired\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	root := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "linked", "config.toml")
	err := executeModeFileWrite(t, model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ExpectedMissing: true, RejectSymlinkParents: true})
	if err == nil || !strings.Contains(err.Error(), "parent is a symlink") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(external, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("external destination was written: %v", err)
	}
}

func TestExecuteCopiesThemeOnlyWhenDestinationIsMissing(t *testing.T) {
	source, destination := t.TempDir(), filepath.Join(t.TempDir(), "custom")
	if err := os.WriteFile(filepath.Join(source, "colors.toml"), []byte("accent = '#fff'\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	j, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "themes.copy.custom", Provider: "themes", Action: "copy", Copy: &model.Copy{Source: source, Destination: destination}}}}
	result, err := Execute(context.Background(), delayedRunner{}, plan, j, time.Now, time.Second, nil)
	if err != nil || len(result.Completed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if got, err := os.ReadFile(filepath.Join(destination, "colors.toml")); err != nil || string(got) != "accent = '#fff'\n" {
		t.Fatalf("file=%q err=%v", got, err)
	}

	j2, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	result, err = Execute(context.Background(), delayedRunner{}, plan, j2, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 1 {
		t.Fatalf("existing destination result=%#v err=%v", result, err)
	}
}

func TestExecuteFileWriteAtomicallyReplacesDestinationAndCreatesBackup(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "saved")
	destination := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(source, []byte("saved configuration\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("default configuration\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	sourceHash := hashFile(t, source)
	destinationHash := hashFile(t, destination)
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "config.write.hypr.bindings", File: &model.FileWrite{Source: source, Destination: destination, SourceHash: sourceHash, ExpectedHash: destinationHash, Backup: true},
	}}}
	result, err := Execute(context.Background(), delayedRunner{}, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Completed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !result.Completed[0].Reversible {
		t.Fatal("backed-up file write was not marked reversible")
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "saved configuration\n" {
		t.Fatalf("destination=%q err=%v", got, err)
	}
	if got, err := io.ReadAll(old); err != nil || string(got) != "default configuration\n" {
		t.Fatalf("open destination=%q err=%v", got, err)
	}
	if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("destination mode=%v err=%v", info.Mode(), err)
	}
	backup := filepath.Join(strings.TrimSuffix(journal.Path, ".jsonl")+".backup", "config.write.hypr.bindings")
	if got, err := os.ReadFile(backup); err != nil || string(got) != "default configuration\n" {
		t.Fatalf("backup=%q err=%v", got, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadFile(journal.Path)
	if err != nil || !strings.Contains(string(entries), "BACKUP_CREATED") || !strings.Contains(string(entries), backup) {
		t.Fatalf("journal=%q err=%v", entries, err)
	}
}

func TestExecuteFileWriteCreatesMissingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "saved")
	destination := filepath.Join(dir, "nested", "config.lua")
	if err := os.WriteFile(source, []byte("saved configuration\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "config.write.new", File: &model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ExpectedMissing: true}}}}
	result, err := Execute(context.Background(), delayedRunner{}, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Completed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "saved configuration\n" {
		t.Fatalf("destination=%q err=%v", got, err)
	}
	if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("destination mode=%v err=%v", info.Mode(), err)
	}
}

func TestExecuteFileWriteRejectsCorruptionDriftAndSymlinks(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, source, destination string) *model.FileWrite
		verify func(t *testing.T, destination string)
	}{
		{
			name: "source corruption", setup: func(t *testing.T, source, destination string) *model.FileWrite {
				hash := hashFile(t, source)
				if err := os.WriteFile(source, []byte("changed after planning"), 0o600); err != nil {
					t.Fatal(err)
				}
				return &model.FileWrite{Source: source, Destination: destination, SourceHash: hash, ExpectedMissing: true}
			},
			verify: func(t *testing.T, destination string) {
				if _, err := os.Lstat(destination); !os.IsNotExist(err) {
					t.Fatalf("destination was created: %v", err)
				}
			},
		},
		{
			name: "destination drift", setup: func(t *testing.T, source, destination string) *model.FileWrite {
				if err := os.WriteFile(destination, []byte("captured baseline"), 0o600); err != nil {
					t.Fatal(err)
				}
				hash := hashFile(t, destination)
				if err := os.WriteFile(destination, []byte("unknown work"), 0o600); err != nil {
					t.Fatal(err)
				}
				return &model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ExpectedHash: hash, Backup: true}
			},
			verify: func(t *testing.T, destination string) {
				if got, err := os.ReadFile(destination); err != nil || string(got) != "unknown work" {
					t.Fatalf("destination=%q err=%v", got, err)
				}
			},
		},
		{
			name: "destination symlink", setup: func(t *testing.T, source, destination string) *model.FileWrite {
				target := filepath.Join(filepath.Dir(destination), "target")
				if err := os.WriteFile(target, []byte("default configuration\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, destination); err != nil {
					t.Fatal(err)
				}
				return &model.FileWrite{Source: source, Destination: destination, SourceHash: hashFile(t, source), ExpectedMissing: true}
			},
			verify: func(t *testing.T, destination string) {
				info, err := os.Lstat(destination)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("destination mode=%v err=%v", info.Mode(), err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source, destination := filepath.Join(dir, "saved"), filepath.Join(dir, "config.lua")
			if err := os.WriteFile(source, []byte("saved configuration\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			journal, err := NewJournal(t.TempDir(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			plan := model.RestorePlan{Operations: []model.Operation{{ID: "config.write", File: tt.setup(t, source, destination)}}}
			result, err := Execute(context.Background(), delayedRunner{}, plan, journal, time.Now, time.Second, nil)
			if err != nil || len(result.Failed) != 1 {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			tt.verify(t, destination)
		})
	}
}

func TestExecuteFileWriteFailureBlocksDependentsButNotIndependentOperations(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "saved")
	if err := os.WriteFile(source, []byte("saved configuration\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	runner := &failingRunner{}
	plan := model.RestorePlan{Operations: []model.Operation{
		{ID: "config.write", File: &model.FileWrite{Source: source, Destination: filepath.Join(dir, "config.lua"), SourceHash: "invalid", ExpectedMissing: true}},
		{ID: "hypr.reload", Command: []string{"reload"}, DependsOn: []string{"config.write"}},
		{ID: "independent", Command: []string{"other"}},
	}}
	result, err := Execute(context.Background(), runner, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 1 || len(result.Blocked) != 1 || len(result.Completed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(runner.calls, []string{"other"}) {
		t.Fatalf("calls=%#v", runner.calls)
	}
}

func TestExecuteRejectsOperationsWithMultipleActions(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "copy")
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	runner := &failingRunner{}
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "invalid", Command: []string{"other"}, Copy: &model.Copy{Source: t.TempDir(), Destination: destination},
	}}}
	result, err := Execute(context.Background(), runner, plan, journal, time.Now, time.Second, nil)
	if err == nil || len(result.Failed) != 0 || len(result.Completed) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner invoked for invalid operation: %#v", runner.calls)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("copy action ran for invalid operation: %v", err)
	}
}

func TestExecuteOperationActionExclusivityIncludesDirectoryAndSymlink(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		op   model.Operation
		want bool
	}{
		{
			name: "directory only",
			op:   model.Operation{ID: "directory", Directory: &model.DirectoryCreate{Path: filepath.Join(root, "directory"), Mode: 0o755}},
			want: true,
		},
		{
			name: "symlink only",
			op:   model.Operation{ID: "symlink", Symlink: &model.SymlinkWrite{Destination: filepath.Join(root, "symlink"), Target: "target", ExpectedMissing: true}},
			want: true,
		},
		{
			name: "delete only",
			op:   model.Operation{ID: "delete", Delete: &model.FileDelete{Destination: filepath.Join(root, "missing"), ExpectedMissing: true}},
			want: true,
		},
		{
			name: "file and symlink",
			op:   model.Operation{ID: "invalid", File: &model.FileWrite{}, Symlink: &model.SymlinkWrite{}},
		},
		{
			name: "file and delete",
			op:   model.Operation{ID: "invalid", File: &model.FileWrite{}, Delete: &model.FileDelete{}},
		},
		{
			name: "copy and directory",
			op:   model.Operation{ID: "invalid", Copy: &model.Copy{}, Directory: &model.DirectoryCreate{}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := executeOperation(context.Background(), delayedRunner{}, tt.op, nil, time.Now)
			if (err == nil) != tt.want {
				t.Fatalf("err=%v want success=%t", err, tt.want)
			}
		})
	}
}

func TestExecuteFileDeleteBacksUpRegularFile(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(destination, []byte("saved configuration\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	precondition := filesystemPreconditionForTest(t, destination)
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{ID: "config.delete", Delete: &model.FileDelete{Destination: destination, ExpectedExisting: &precondition, Backup: true, RejectSymlinkParents: true}}}}
	result, err := Execute(context.Background(), delayedRunner{}, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Completed) != 1 || !result.Completed[0].Reversible {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination remains: %v", err)
	}
	backup := siblingBackupForTest(t, dir, filepath.Base(destination))
	if content, err := os.ReadFile(backup); err != nil || string(content) != "saved configuration\n" {
		t.Fatalf("backup=%q err=%v", content, err)
	}
	if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode=%v err=%v", info.Mode(), err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadFile(journal.Path)
	if err != nil || !strings.Contains(string(entries), "BACKUP_CREATED") || !strings.Contains(string(entries), backup) {
		t.Fatalf("journal=%q err=%v", entries, err)
	}
}

func TestExecuteFileDeletePreservesChangedDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(destination, []byte("A"), 0o600); err != nil {
		t.Fatal(err)
	}
	precondition := filesystemPreconditionForTest(t, destination)
	if err := os.WriteFile(destination, []byte("B"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	result, err := Execute(context.Background(), delayedRunner{}, model.RestorePlan{Operations: []model.Operation{{ID: "config.delete", Delete: &model.FileDelete{Destination: destination, ExpectedExisting: &precondition, Backup: true}}}}, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if content, err := os.ReadFile(destination); err != nil || string(content) != "B" {
		t.Fatalf("destination=%q err=%v", content, err)
	}
	if entries, err := os.ReadDir(filepath.Dir(destination)); err != nil || len(entries) != 1 {
		t.Fatalf("unexpected backup: entries=%v err=%v", entries, err)
	}
}

func TestFileDeleteBacksUpDirectoriesAndSymlinksWithoutFollowing(t *testing.T) {
	for _, setup := range []struct {
		name  string
		make  func(t *testing.T, destination string)
		check func(t *testing.T, backup string)
	}{
		{"directory", func(t *testing.T, destination string) {
			if err := os.Mkdir(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(destination, "content"), []byte("saved"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, backup string) {
			if content, err := os.ReadFile(filepath.Join(backup, "content")); err != nil || string(content) != "saved" {
				t.Fatalf("backup content=%q err=%v", content, err)
			}
		}},
		{"symlink", func(t *testing.T, destination string) {
			if err := os.Symlink("external-target", destination); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, backup string) {
			if target, err := os.Readlink(backup); err != nil || target != "external-target" {
				t.Fatalf("backup target=%q err=%v", target, err)
			}
		}},
	} {
		t.Run(setup.name, func(t *testing.T) {
			dir, destination := t.TempDir(), ""
			destination = filepath.Join(dir, "config")
			setup.make(t, destination)
			precondition := filesystemPreconditionForTest(t, destination)
			journal, err := NewJournal(t.TempDir(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			if err := executeFileDeleteWithJournal("config.delete", model.FileDelete{Destination: destination, ExpectedExisting: &precondition, Backup: true}, journal, time.Now); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatalf("destination remains: %v", err)
			}
			setup.check(t, siblingBackupForTest(t, dir, "config"))
		})
	}
}

func TestFileDeleteRejectsParentSymlinkWithoutMovingObject(t *testing.T) {
	root, external := t.TempDir(), t.TempDir()
	parent := filepath.Join(root, "parent")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "config")
	if err := os.WriteFile(destination, []byte("approved"), 0o600); err != nil {
		t.Fatal(err)
	}
	precondition := filesystemPreconditionForTest(t, destination)
	movedParent := filepath.Join(root, "moved-parent")
	if err := os.Rename(parent, movedParent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, parent); err != nil {
		t.Fatal(err)
	}
	if err := executeFileDeleteWithJournal("config.delete", model.FileDelete{Destination: destination, ExpectedExisting: &precondition, Backup: true, RejectSymlinkParents: true}, nil, time.Now); err == nil || !strings.Contains(err.Error(), "parent is a symlink") {
		t.Fatalf("err=%v", err)
	}
	if content, err := os.ReadFile(filepath.Join(movedParent, "config")); err != nil || string(content) != "approved" {
		t.Fatalf("approved object=%q err=%v", content, err)
	}
	if _, err := os.Lstat(filepath.Join(external, "config")); !os.IsNotExist(err) {
		t.Fatalf("external object was touched: %v", err)
	}
}

func TestFileDeleteKeepsRecreatedDestinationWhenRollbackBlocked(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "config")
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	precondition := filesystemPreconditionForTest(t, destination)
	old := fileDeleteCompleter
	defer func() { fileDeleteCompleter = old }()
	fileDeleteCompleter = func() error {
		if err := os.WriteFile(destination, []byte("new user work"), 0o600); err != nil {
			return err
		}
		return errors.New("completion failed")
	}
	err := executeFileDeleteWithJournal("config.delete", model.FileDelete{Destination: destination, ExpectedExisting: &precondition, Backup: true}, nil, time.Now)
	if err == nil || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("err=%v", err)
	}
	if content, err := os.ReadFile(destination); err != nil || string(content) != "new user work" {
		t.Fatalf("destination=%q err=%v", content, err)
	}
	backup := siblingBackupForTest(t, dir, "config")
	if content, err := os.ReadFile(backup); err != nil || string(content) != "original" {
		t.Fatalf("backup=%q err=%v", content, err)
	}
}

func filesystemPreconditionForTest(t *testing.T, path string) model.FilesystemPrecondition {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	precondition := model.FilesystemPrecondition{Mode: uint32(info.Mode().Perm())}
	if info.Mode()&os.ModeSymlink != 0 {
		precondition.Type = "symlink"
		precondition.Target, err = os.Readlink(path)
	} else if info.IsDir() {
		precondition.Type = "directory"
		precondition.Hash, err = content.HashFilesystemObject(path)
	} else {
		precondition.Type = "file"
		precondition.Hash, err = content.HashFilesystemObject(path)
	}
	if err != nil {
		t.Fatal(err)
	}
	return precondition
}

func siblingBackupForTest(t *testing.T, dir, base string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "." + base + ".omarchy-blueprint-backup-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			return filepath.Join(dir, entry.Name())
		}
	}
	t.Fatalf("backup with prefix %q not found", prefix)
	return ""
}

func TestDirectoryCreateRejectsSymlinkParent(t *testing.T) {
	external := t.TempDir()
	root := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	action := model.DirectoryCreate{Path: filepath.Join(root, "linked", "Projects"), Mode: 0o755, RejectSymlinkParents: true}
	err := executeDirectoryCreate(action)
	if err == nil || !strings.Contains(err.Error(), "parent is a symlink") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(external, "Projects")); !os.IsNotExist(err) {
		t.Fatalf("external directory was created: %v", err)
	}
}

func TestDirectoryCreateIsIdempotentForRealDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Projects")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := executeDirectoryCreate(model.DirectoryCreate{Path: path, Mode: 0o755, RejectSymlinkParents: true}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("info=%v err=%v", info, err)
	}
}

func TestSymlinkWriteCreatesOnlyMissingDestination(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, ".config", "nvim")
	action := model.SymlinkWrite{Destination: destination, Target: "../../dotfiles/nvim", ExpectedMissing: true, RejectSymlinkParents: true}
	if err := executeSymlinkWrite(action); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(destination); err != nil || target != action.Target {
		t.Fatalf("target=%q err=%v", target, err)
	}
	for _, setup := range []func(string) error{
		func(path string) error { return os.WriteFile(path, []byte("existing"), 0o644) },
		func(path string) error { return os.Symlink("different", path) },
	} {
		path := filepath.Join(t.TempDir(), "destination")
		if err := setup(path); err != nil {
			t.Fatal(err)
		}
		if err := executeSymlinkWrite(model.SymlinkWrite{Destination: path, Target: "target", ExpectedMissing: true}); err == nil {
			t.Fatal("existing destination unexpectedly replaced")
		}
	}
}

func TestSymlinkWriteRejectsSymlinkParent(t *testing.T) {
	external := t.TempDir()
	root := t.TempDir()
	parent := filepath.Join(root, "linked")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "config")
	action := model.SymlinkWrite{Destination: destination, Target: "target", ExpectedMissing: true, RejectSymlinkParents: true}
	// Model the parent changing after planning but before execution.
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, parent); err != nil {
		t.Fatal(err)
	}
	err := executeSymlinkWrite(action)
	if err == nil || !strings.Contains(err.Error(), "parent is a symlink") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(external, "config")); !os.IsNotExist(err) {
		t.Fatalf("external destination was written: %v", err)
	}
}

func TestForcedSymlinkWriteRejectsChangedDestination(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "nvim")
	if err := os.WriteFile(destination, []byte("A"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(destination)
	hash, _ := content.HashFilesystemObject(destination)
	action := model.SymlinkWrite{Destination: destination, Target: "dotfiles/nvim", ReplaceExisting: true, Backup: true, ExpectedExisting: &model.FilesystemPrecondition{Type: "file", Mode: uint32(info.Mode().Perm()), Hash: hash}}
	if err := os.WriteFile(destination, []byte("B"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executeSymlinkWrite(action); err == nil {
		t.Fatal("changed destination was replaced")
	}
	got, _ := os.ReadFile(destination)
	if string(got) != "B" {
		t.Fatalf("destination=%q", got)
	}
}

func TestForcedSymlinkWriteKeepsRecreatedDestinationWhenRollbackBlocked(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "nvim")
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(destination)
	hash, _ := content.HashFilesystemObject(destination)
	action := model.SymlinkWrite{Destination: destination, Target: "dotfiles/nvim", ReplaceExisting: true, Backup: true, ExpectedExisting: &model.FilesystemPrecondition{Type: "file", Mode: uint32(info.Mode().Perm()), Hash: hash}}
	old := symlinkInstaller
	defer func() { symlinkInstaller = old }()
	symlinkInstaller = func(model.SymlinkWrite) error {
		if err := os.WriteFile(destination, []byte("new user work"), 0o600); err != nil {
			return err
		}
		return errors.New("install failed")
	}
	if err := executeSymlinkWrite(action); err == nil || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("err=%v", err)
	}
	got, _ := os.ReadFile(destination)
	if string(got) != "new user work" {
		t.Fatalf("destination=%q", got)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 2 {
		t.Fatalf("entries=%v", entries)
	}
}

func TestCopySourceHashRejectsMutationBeforeDestinationCreation(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "content"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := content.HashRegularTree(source)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "copy")
	if err := os.WriteFile(filepath.Join(source, "content"), []byte("mutated"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = copyTreeExclusive(model.Copy{Source: source, Destination: destination, SourceHash: hash})
	if err == nil || !strings.Contains(err.Error(), "source hash mismatch") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination was created: %v", err)
	}
}

func TestCopyRejectsSymlinkParent(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "content"), []byte("snapshot"), 0o644); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	root := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "linked", "copy")
	err := copyTreeExclusive(model.Copy{Source: source, Destination: destination, RejectSymlinkParents: true})
	if err == nil || !strings.Contains(err.Error(), "parent is a symlink") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(external, "copy")); !os.IsNotExist(err) {
		t.Fatalf("external destination was created: %v", err)
	}
}

func TestCopyTreePreservesModesDespiteUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	source := t.TempDir()
	if err := os.Chmod(source, 0o775); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{"file": 0o664, "script": 0o775} {
		full := filepath.Join(source, path)
		if err := os.WriteFile(full, []byte(path), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(full, mode); err != nil {
			t.Fatal(err)
		}
	}
	destination := filepath.Join(t.TempDir(), "copy")
	if err := copyTreeExclusive(model.Copy{Source: source, Destination: destination}); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{destination: 0o775, filepath.Join(destination, "file"): 0o664, filepath.Join(destination, "script"): 0o775} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("path=%s mode=%o err=%v", path, info.Mode().Perm(), err)
		}
	}
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	hash, err := content.HashRegularFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestExecutePreCanceledContextRunsNoOperations(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "saved")
	destination := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(source, []byte("saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceHash := hashFile(t, source)
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "config.write.hypr.bindings", File: &model.FileWrite{Source: source, Destination: destination, SourceHash: sourceHash, ExpectedMissing: true},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Execute(ctx, delayedRunner{}, plan, journal, time.Now, time.Second, nil)
	if err != context.Canceled {
		t.Fatalf("err = %v want context.Canceled", err)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("destination must not exist after pre-cancel, statErr=%v", statErr)
	}
}

func TestExecuteWaitsForInFlightOperationBeforeReturning(t *testing.T) {
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "validate.slow", Command: []string{"slow"},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := Execute(ctx, delayedStartedRunner{started: started, delay: 200 * time.Millisecond}, plan, journal, time.Now, 10*time.Millisecond, nil)
		done <- outcome{result, err}
	}()
	<-started
	startedAt := time.Now()
	cancel()
	_, err = func() (Result, error) { received := <-done; return received.result, received.err }()
	elapsed := time.Since(startedAt)
	if err != context.Canceled {
		t.Fatalf("err = %v want context.Canceled", err)
	}
	if elapsed < 190*time.Millisecond {
		t.Fatalf("Execute returned in %v; it must wait for the in-flight operation to finish", elapsed)
	}
}

type delayedStartedRunner struct {
	started chan<- struct{}
	delay   time.Duration
}

func (r delayedStartedRunner) Run(_ context.Context, _ string, _ ...string) (string, error) {
	r.started <- struct{}{}
	time.Sleep(r.delay)
	return "", nil
}

type contextAwareRunner struct{ started chan<- struct{} }

func (r contextAwareRunner) Run(ctx context.Context, _ string, _ ...string) (string, error) {
	r.started <- struct{}{}
	<-ctx.Done()
	// Give the executor's wait loop a moment to observe the cancellation
	// before the result races with the Done channel.
	time.Sleep(10 * time.Millisecond)
	return "", ctx.Err()
}

type completeAfterCancelRunner struct {
	started chan<- struct{}
}

func (r completeAfterCancelRunner) Run(ctx context.Context, _ string, _ ...string) (string, error) {
	// Signal that Execute has started this operation before the test cancels.
	// A timer can otherwise fire before the executor reaches the operation loop.
	r.started <- struct{}{}
	<-ctx.Done()
	return "", nil
}

func journalEvents(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event.Type)
	}
	return events
}

func TestExecuteRecordsCompletedOperationDespiteCancellation(t *testing.T) {
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{
		// Emulates a local mutation that is already past its safe
		// cancellation point when the context is cancelled: it finishes
		// successfully during the cancellation wait.
		ID: "config.write.hypr.bindings", Command: []string{"finish-after-cancel"},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := Execute(ctx, completeAfterCancelRunner{started: started}, plan, journal, time.Now, 10*time.Millisecond, nil)
		done <- outcome{result: result, err: err}
	}()
	<-started
	cancel()
	received := <-done
	result, err := received.result, received.err
	if err != context.Canceled {
		t.Fatalf("err = %v want context.Canceled", err)
	}
	if len(result.Completed) != 1 || result.Completed[0].ID != "config.write.hypr.bindings" {
		t.Fatalf("operation completed during cancellation must be recorded: %#v", result)
	}
	events := journalEvents(t, journal.Path)
	found := false
	for _, eventType := range events {
		if eventType == "OPERATION_COMPLETED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("journal must record OPERATION_COMPLETED, got %v", events)
	}
}

func TestExecuteJournalsCancelledCommand(t *testing.T) {
	journal, err := NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID: "slow.command", Command: []string{"slow"},
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan struct {
		result Result
		err    error
	}, 1)
	go func() {
		result, err := Execute(ctx, contextAwareRunner{started: started}, plan, journal, time.Now, 10*time.Millisecond, nil)
		done <- struct {
			result Result
			err    error
		}{result, err}
	}()
	<-started
	cancel()
	received := <-done
	result, err := received.result, received.err
	if err != context.Canceled {
		t.Fatalf("err = %v want context.Canceled", err)
	}
	if len(result.Completed) != 0 {
		t.Fatalf("aborted command must not be completed: %#v", result)
	}
	events := journalEvents(t, journal.Path)
	hasCancelled, hasCancelling := false, false
	for _, eventType := range events {
		if eventType == "OPERATION_CANCELLED" {
			hasCancelled = true
		}
		if eventType == "OPERATION_CANCELLING" {
			hasCancelling = true
		}
	}
	if !hasCancelled || !hasCancelling {
		t.Fatalf("journal must record OPERATION_CANCELLING then OPERATION_CANCELLED, got %v", events)
	}
}
