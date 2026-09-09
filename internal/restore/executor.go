package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
)

type ProgressType string

const (
	ProgressStarted   ProgressType = "started"
	ProgressHeartbeat ProgressType = "heartbeat"
	ProgressCompleted ProgressType = "completed"
	ProgressFailed    ProgressType = "failed"
)

type Progress struct {
	Type      ProgressType
	Operation model.Operation
	Elapsed   time.Duration
}

type ProgressFunc func(Progress)

type Failure struct {
	Operation model.Operation `json:"operation"`
	Error     string          `json:"error"`
}

type Result struct {
	Completed []model.Operation `json:"completed"`
	Failed    []Failure         `json:"failed"`
	Blocked   []Blocked         `json:"blocked,omitempty"`
}

type Blocked struct {
	Operation  model.Operation `json:"operation"`
	Dependency string          `json:"dependency"`
}

func Execute(ctx context.Context, runner command.Runner, plan model.RestorePlan, journal *Journal, now func() time.Time, heartbeat time.Duration, progress ProgressFunc) (Result, error) {
	var execution Result
	if err := ValidatePlan(plan); err != nil {
		return execution, err
	}
	failed := map[string]bool{}
	if err := journal.Write(Event{Time: now().UTC(), Type: "PLAN_CREATED", Message: fmt.Sprintf("%d operations", len(plan.Operations))}); err != nil {
		return execution, err
	}
	for _, op := range plan.Operations {
		if err := ctx.Err(); err != nil {
			return execution, err
		}
		blocked := ""
		for _, dependency := range op.DependsOn {
			if failed[dependency] {
				blocked = dependency
				break
			}
		}
		if blocked != "" {
			message := "dependency failed: " + blocked
			_ = journal.Write(Event{Time: now().UTC(), Type: "OPERATION_BLOCKED", Operation: op.ID, Message: message})
			execution.Blocked = append(execution.Blocked, Blocked{Operation: op, Dependency: blocked})
			failed[op.ID] = true
			continue
		}
		started := time.Now()
		if err := journal.Write(Event{Time: now().UTC(), Type: "OPERATION_STARTED", Operation: op.ID}); err != nil {
			return execution, err
		}
		notify(progress, Progress{Type: ProgressStarted, Operation: op})
		result := make(chan error, 1)
		go func() {
			result <- executeOperation(ctx, runner, op, journal, now)
		}()
		ticker := time.NewTicker(heartbeat)
		var err error
		// Never abandon an in-flight mutation: commands honor the context
		// and abort quickly, while local file writes finish their small
		// atomic mutation before the journal closes. The outcome is
		// classified by ctx.Err() after the operation finishes, because a
		// simultaneous cancel-and-complete race may deliver either select
		// case first.
		cancelCh := ctx.Done()
	wait:
		for {
			select {
			case err = <-result:
				break wait
			case <-ticker.C:
				elapsed := time.Since(started).Round(time.Second)
				_ = journal.Write(Event{Time: now().UTC(), Type: "OPERATION_PROGRESS", Operation: op.ID, Message: fmt.Sprintf("elapsed=%s", elapsed)})
				notify(progress, Progress{Type: ProgressHeartbeat, Operation: op, Elapsed: elapsed})
			case <-cancelCh:
				// Done stays readable forever after cancellation; nil the
				// channel so this case never spins while waiting.
				cancelCh = nil
				ticker.Stop()
				_ = journal.Write(Event{Time: now().UTC(), Type: "OPERATION_CANCELLING", Operation: op.ID, Message: "waiting for in-flight operation"})
			}
		}
		ticker.Stop()
		if err != nil {
			failed[op.ID] = true
			eventType := "OPERATION_FAILED"
			if ctx.Err() != nil {
				eventType = "OPERATION_CANCELLED"
			}
			_ = journal.Write(Event{Time: now().UTC(), Type: eventType, Operation: op.ID, Message: err.Error()})
			notify(progress, Progress{Type: ProgressFailed, Operation: op, Elapsed: time.Since(started).Round(time.Second)})
			if ctx.Err() != nil {
				return execution, ctx.Err()
			}
			execution.Failed = append(execution.Failed, Failure{Operation: op, Error: summarizeError(err)})
			continue
		}
		// The operation finished successfully — including one that was
		// already past its safe cancellation point when the context was
		// cancelled. Journal its real outcome before reporting the
		// cancellation.
		if err := journal.Write(Event{Time: now().UTC(), Type: "OPERATION_COMPLETED", Operation: op.ID}); err != nil {
			return execution, err
		}
		completed := op
		if (completed.File != nil && completed.File.Backup) || (completed.Delete != nil && completed.Delete.Backup) || (completed.Symlink != nil && completed.Symlink.Backup) {
			completed.Reversible = true
		}
		execution.Completed = append(execution.Completed, completed)
		notify(progress, Progress{Type: ProgressCompleted, Operation: op, Elapsed: time.Since(started).Round(time.Second)})
		if ctx.Err() != nil {
			return execution, ctx.Err()
		}
	}
	return execution, nil
}

func executeOperation(ctx context.Context, runner command.Runner, op model.Operation, journal *Journal, now func() time.Time) error {
	actions := 0
	if len(op.Command) > 0 {
		actions++
	}
	if op.Copy != nil {
		actions++
	}
	if op.File != nil {
		actions++
	}
	if op.Delete != nil {
		actions++
	}
	if op.Directory != nil {
		actions++
	}
	if op.Symlink != nil {
		actions++
	}
	if actions != 1 {
		return fmt.Errorf("operation %s must contain exactly one command, copy, file, delete, directory, or symlink action", op.ID)
	}
	if op.Copy != nil {
		return copyTreeExclusive(*op.Copy)
	}
	if op.File != nil {
		return writeFileAtomic(op.ID, *op.File, journal, now)
	}
	if op.Delete != nil {
		return executeFileDeleteWithJournal(op.ID, *op.Delete, journal, now)
	}
	if op.Directory != nil {
		return executeDirectoryCreate(*op.Directory)
	}
	if op.Symlink != nil {
		return executeSymlinkWriteWithJournal(op.ID, *op.Symlink, journal, now)
	}
	_, err := runner.Run(ctx, op.Command[0], op.Command[1:]...)
	return err
}

func executeFileDeleteWithJournal(operation string, action model.FileDelete, journal *Journal, now func() time.Time) error {
	if err := validateFileDelete(operation, action); err != nil {
		return err
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if action.ExpectedMissing {
		if _, err := os.Lstat(action.Destination); err == nil {
			return fmt.Errorf("file delete destination already exists: %s", action.Destination)
		} else if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := validateFilesystemPrecondition(action.Destination, *action.ExpectedExisting); err != nil {
		return err
	}
	if !action.Backup {
		if action.RejectSymlinkParents {
			if err := validateSymlinkParents(action.Destination); err != nil {
				return err
			}
		}
		if err := validateFilesystemPrecondition(action.Destination, *action.ExpectedExisting); err != nil {
			return err
		}
		return os.Remove(action.Destination)
	}
	backup, err := reserveSiblingBackupPath(action.Destination)
	if err != nil {
		return err
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if err := validateFilesystemPrecondition(action.Destination, *action.ExpectedExisting); err != nil {
		return err
	}
	if err := renameNoReplace(action.Destination, backup); err != nil {
		return err
	}
	rollback := func(cause error) error {
		if err := renameNoReplace(backup, action.Destination); err != nil {
			return fmt.Errorf("%v; rollback failed: %w", cause, err)
		}
		return cause
	}
	if journal != nil {
		if err := journal.Write(Event{Time: now().UTC(), Type: "BACKUP_CREATED", Operation: operation, Message: backup}); err != nil {
			return rollback(err)
		}
	}
	if err := fileDeleteCompleter(); err != nil {
		return rollback(err)
	}
	return nil
}

var fileDeleteCompleter = func() error { return nil }

func writeFileAtomic(operation string, action model.FileWrite, journal *Journal, now func() time.Time) error {
	if action.SourceHash == "" {
		return fmt.Errorf("file write source hash is required: %s", operation)
	}
	if action.Mode != nil && *action.Mode > 0o777 {
		return fmt.Errorf("file write mode is invalid: %04o", *action.Mode)
	}
	if action.ExpectedMode != nil && *action.ExpectedMode > 0o777 {
		return fmt.Errorf("file write expected mode is invalid: %04o", *action.ExpectedMode)
	}
	if action.ReplaceExisting {
		if action.ExpectedMissing || action.ExpectedExisting == nil || !action.Backup {
			return fmt.Errorf("file replacement requires existing precondition and backup: %s", operation)
		}
	} else if action.ExpectedMissing == (action.ExpectedHash != "") {
		return fmt.Errorf("file write requires exactly one destination precondition: %s", operation)
	}
	if action.ExpectedMissing && action.ExpectedMode != nil {
		return fmt.Errorf("file write expected mode requires an existing destination: %s", operation)
	}
	source, err := openFileWriteSource(action)
	if err != nil {
		return err
	}
	defer source.Close()
	sourceHash, err := hashFileWriteSource(source.Reader)
	if err != nil {
		return fmt.Errorf("hash file write source: %w", err)
	}
	if sourceHash != action.SourceHash {
		canonical, err := content.HashRegularTree(action.Source)
		if err != nil || canonical != action.SourceHash {
			return fmt.Errorf("file write source hash mismatch: %s", action.Source)
		}
	}
	if _, err := source.Reader.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if action.ReplaceExisting {
		return replaceFileWriteWithJournal(operation, action, journal, now)
	}

	destinationInfo, err := validateDestination(action)
	if err != nil {
		return err
	}
	if destinationInfo != nil && action.Backup {
		backup, err := journal.CreateBackup(operation, action.Destination)
		if err != nil {
			return fmt.Errorf("create file backup: %w", err)
		}
		if err := journal.Write(Event{Time: now().UTC(), Type: "BACKUP_CREATED", Operation: operation, Message: backup}); err != nil {
			return err
		}
	}

	parent := filepath.Dir(action.Destination)
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	temp, err := os.CreateTemp(parent, ".omarchy-blueprint-file-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	mode := source.Mode.Perm()
	if destinationInfo != nil {
		mode = destinationInfo.Mode().Perm()
	}
	if action.Mode != nil {
		mode = os.FileMode(*action.Mode)
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := io.Copy(temp, source.Reader); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	// Recheck the target at the final mutation boundary. Preparing a backup and
	// temporary file can take long enough for another process to change it.
	if _, err := validateDestination(action); err != nil {
		return err
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if action.ExpectedMissing {
		if err := fileWriteNoReplace(tempPath, action.Destination); err != nil {
			return err
		}
	} else if err := os.Rename(tempPath, action.Destination); err != nil {
		return err
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// fileWriteNoReplace is a test seam for the final expected-missing install.
var fileWriteNoReplace = renameNoReplace

func replaceFileWriteWithJournal(operation string, action model.FileWrite, journal *Journal, now func() time.Time) error {
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if err := validateFilesystemPrecondition(action.Destination, *action.ExpectedExisting); err != nil {
		return err
	}
	if journal != nil && action.ExpectedExisting.Type == "file" {
		// Preserve the journal backup contract as well as the sibling backup
		// used for an atomic rollback-safe replacement below.
		backupCopy, err := journal.CreateBackup(operation, action.Destination)
		if err != nil {
			return fmt.Errorf("create file backup: %w", err)
		}
		if err := journal.Write(Event{Time: now().UTC(), Type: "BACKUP_CREATED", Operation: operation, Message: backupCopy}); err != nil {
			return err
		}
	}
	backup, err := reserveSiblingBackupPath(action.Destination)
	if err != nil {
		return err
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if err := validateFilesystemPrecondition(action.Destination, *action.ExpectedExisting); err != nil {
		return err
	}
	if err := renameNoReplace(action.Destination, backup); err != nil {
		return err
	}
	rollback := func(cause error) error {
		if err := renameNoReplace(backup, action.Destination); err != nil {
			return fmt.Errorf("%v; rollback failed: %w", cause, err)
		}
		return cause
	}
	if journal != nil && action.ExpectedExisting.Type != "file" {
		if err := journal.Write(Event{Time: now().UTC(), Type: "BACKUP_CREATED", Operation: operation, Message: backup}); err != nil {
			return rollback(err)
		}
	}
	install := action
	install.ReplaceExisting = false
	install.ExpectedExisting = nil
	install.ExpectedHash = ""
	install.ExpectedMode = nil
	install.ExpectedMissing = true
	install.Backup = false
	if err := writeFileAtomic(operation, install, journal, now); err != nil {
		if _, statErr := os.Lstat(action.Destination); os.IsNotExist(statErr) {
			return rollback(err)
		}
		return fmt.Errorf("%v; backup retained at %s", err, backup)
	}
	return nil
}

func validateSymlinkParents(destination string) error {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	root := filepath.VolumeName(abs) + string(os.PathSeparator)
	relative, err := filepath.Rel(root, filepath.Dir(abs))
	if err != nil {
		return err
	}
	parent := root
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}
		parent = filepath.Join(parent, part)
		info, err := os.Lstat(parent)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("file write destination parent is a symlink: %s", parent)
		}
		if !info.IsDir() {
			return fmt.Errorf("file write destination parent is not a directory: %s", parent)
		}
	}
	return nil
}

func hashFileWriteSource(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type fileWriteSource struct {
	Reader io.ReadSeeker
	Mode   os.FileMode
	Close  func() error
}

func openFileWriteSource(action model.FileWrite) (fileWriteSource, error) {
	if action.Generated {
		if action.Source != "" || action.Content == nil {
			return fileWriteSource{}, fmt.Errorf("generated file write requires content and no source path")
		}
		return fileWriteSource{Reader: bytes.NewReader(action.Content), Mode: 0o644, Close: func() error { return nil }}, nil
	}
	if action.Source == "" || action.Content != nil {
		return fileWriteSource{}, fmt.Errorf("file write requires a source path and no generated content")
	}
	file, info, err := content.OpenRegularFile(action.Source)
	if err != nil {
		return fileWriteSource{}, fmt.Errorf("validate file write source: %w", err)
	}
	return fileWriteSource{Reader: file, Mode: info.Mode(), Close: file.Close}, nil
}

func validateDestination(action model.FileWrite) (os.FileInfo, error) {
	info, err := os.Lstat(action.Destination)
	if os.IsNotExist(err) {
		if action.ExpectedMissing {
			return nil, nil
		}
		return nil, fmt.Errorf("file write destination is missing: %s", action.Destination)
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("file write destination is a symlink: %s", action.Destination)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("file write destination is not a regular file: %s", action.Destination)
	}
	if action.ExpectedMissing {
		return nil, fmt.Errorf("file write destination already exists: %s", action.Destination)
	}
	hash, err := content.HashRegularFile(action.Destination)
	if err != nil {
		return nil, fmt.Errorf("hash file write destination: %w", err)
	}
	if hash != action.ExpectedHash {
		return nil, fmt.Errorf("file write destination hash mismatch: %s", action.Destination)
	}
	if action.ExpectedMode != nil && uint32(info.Mode().Perm()) != *action.ExpectedMode {
		return nil, fmt.Errorf("file write destination mode mismatch: %s", action.Destination)
	}
	return info, nil
}

func executeDirectoryCreate(action model.DirectoryCreate) error {
	if action.Mode > 0o777 {
		return fmt.Errorf("directory create mode is invalid: %04o", action.Mode)
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Path); err != nil {
			return err
		}
	}
	info, err := os.Lstat(action.Path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("directory create destination is not a directory: %s", action.Path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(action.Path, os.FileMode(action.Mode)); err != nil {
		return err
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Path); err != nil {
			return err
		}
	}
	return os.Chmod(action.Path, os.FileMode(action.Mode))
}

func executeSymlinkWrite(action model.SymlinkWrite) error {
	return executeSymlinkWriteWithJournal("", action, nil, time.Now)
}

func executeSymlinkWriteWithJournal(operation string, action model.SymlinkWrite, journal *Journal, now func() time.Time) error {
	if action.Target == "" {
		return fmt.Errorf("symlink target is required")
	}
	if action.ExpectedMissing == action.ReplaceExisting {
		return fmt.Errorf("symlink write requires exactly one destination precondition: %s", action.Destination)
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if action.ReplaceExisting {
		if action.ExpectedExisting == nil {
			return fmt.Errorf("symlink replacement requires existing precondition: %s", action.Destination)
		}
		if err := validateFilesystemPrecondition(action.Destination, *action.ExpectedExisting); err != nil {
			return err
		}
		backup, err := reserveSiblingBackupPath(action.Destination)
		if err != nil {
			return err
		}
		if action.RejectSymlinkParents {
			if err := validateSymlinkParents(action.Destination); err != nil {
				return err
			}
		}
		if err := validateFilesystemPrecondition(action.Destination, *action.ExpectedExisting); err != nil {
			return err
		}
		if err := renameNoReplace(action.Destination, backup); err != nil {
			return err
		}
		if journal != nil {
			if err := journal.Write(Event{Time: now().UTC(), Type: "BACKUP_CREATED", Operation: operation, Message: backup}); err != nil {
				if rollback := renameNoReplace(backup, action.Destination); rollback != nil {
					return fmt.Errorf("%v; rollback failed: %w", err, rollback)
				}
				return err
			}
		}
		if err := symlinkInstaller(action); err != nil {
			if rollback := renameNoReplace(backup, action.Destination); rollback != nil {
				return fmt.Errorf("%v; rollback failed: %w", err, rollback)
			}
			return err
		}
		return nil
	}
	if _, err := os.Lstat(action.Destination); err == nil {
		return fmt.Errorf("symlink destination already exists: %s", action.Destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(action.Destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(action.Destination); err == nil {
		return fmt.Errorf("symlink destination already exists: %s", action.Destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(action.Target, action.Destination)
}

var symlinkInstaller = installSymlinkAtomic

func renameNoReplace(old, new string) error {
	oldp, err := syscall.BytePtrFromString(old)
	if err != nil {
		return err
	}
	newp, err := syscall.BytePtrFromString(new)
	if err != nil {
		return err
	}
	// renameat2 is syscall 316 on Linux amd64; Omarchy only supports Linux.
	_, _, errno := syscall.Syscall6(316, uintptr(^uint(99)), uintptr(unsafe.Pointer(oldp)), uintptr(^uint(99)), uintptr(unsafe.Pointer(newp)), 1, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func validateFilesystemPrecondition(path string, expected model.FilesystemPrecondition) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("filesystem precondition failed: %w", err)
	}
	actual := model.FilesystemPrecondition{Mode: uint32(info.Mode().Perm())}
	if info.Mode()&os.ModeSymlink != 0 {
		actual.Type = "symlink"
		actual.Target, err = os.Readlink(path)
	} else if info.IsDir() {
		actual.Type = "directory"
		actual.Hash, err = content.HashFilesystemObject(path)
	} else if info.Mode().IsRegular() {
		actual.Type = "file"
		actual.Hash, err = content.HashFilesystemObject(path)
	} else {
		return fmt.Errorf("filesystem precondition failed: unsupported object")
	}
	if err != nil || actual != expected {
		return fmt.Errorf("filesystem precondition failed: destination changed")
	}
	return nil
}

func reserveSiblingBackupPath(destination string) (string, error) {
	dir, base := filepath.Dir(destination), filepath.Base(destination)
	for n := 0; n < 10000; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf(".%s.omarchy-blueprint-backup-%d", base, n))
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("unable to reserve backup path for %s", destination)
}

func installSymlinkAtomic(action model.SymlinkWrite) error {
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	temp, err := reserveSiblingBackupPath(action.Destination + ".tmp")
	if err != nil {
		return err
	}
	if err := os.Symlink(action.Target, temp); err != nil {
		return err
	}
	defer os.Remove(temp)
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(action.Destination); !os.IsNotExist(err) {
		return fmt.Errorf("symlink destination changed during replacement: %s", action.Destination)
	}
	return os.Rename(temp, action.Destination)
}

func copyTreeExclusive(action model.Copy) error {
	if action.SourceHash != "" {
		hash, err := content.HashRegularTree(action.Source)
		if err != nil {
			return fmt.Errorf("validate copy source: %w", err)
		}
		if hash != action.SourceHash {
			return fmt.Errorf("copy source hash mismatch: %s", action.Source)
		}
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(action.Destination); err == nil {
		return fmt.Errorf("destination already exists: %s", action.Destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(action.Destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(action.Destination); err == nil {
		return fmt.Errorf("destination already exists: %s", action.Destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	temp, err := os.MkdirTemp(parent, ".omarchy-blueprint-copy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	info, err := os.Lstat(action.Source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.Chmod(temp, info.Mode().Perm()); err != nil {
			return err
		}
	}
	if err := copyTreeContents(action.Source, temp); err != nil {
		return err
	}
	if action.RejectSymlinkParents {
		if err := validateSymlinkParents(action.Destination); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(action.Destination); err == nil {
		return fmt.Errorf("destination already exists: %s", action.Destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temp, action.Destination)
}

func copyTreeContents(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("snapshot contains unsupported symlink: %s", relative)
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
				return err
			}
			return os.Chmod(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("snapshot contains unsupported file: %s", relative)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			in.Close()
			return err
		}
		if err := out.Chmod(info.Mode().Perm()); err != nil {
			in.Close()
			out.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		inErr := in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inErr != nil {
			return inErr
		}
		return closeErr
	})
}

func notify(progress ProgressFunc, event Progress) {
	if progress != nil {
		progress(event)
	}
}

func summarizeError(err error) string {
	var lines []string
	for _, line := range strings.Split(err.Error(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	message := strings.Join(lines, "\n")
	if len(message) > 600 {
		return message[len(message)-597:] + "..."
	}
	return message
}
