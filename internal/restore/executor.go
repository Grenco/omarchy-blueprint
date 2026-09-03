package restore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
}

func Execute(ctx context.Context, runner command.Runner, plan model.RestorePlan, journal *Journal, now func() time.Time, heartbeat time.Duration, progress ProgressFunc) (Result, error) {
	var execution Result
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
			_ = journal.Write(Event{Time: now().UTC(), Type: "OPERATION_FAILED", Operation: op.ID, Message: message})
			execution.Failed = append(execution.Failed, Failure{Operation: op, Error: message})
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
		cancelled := false
	wait:
		for {
			select {
			case err = <-result:
				break wait
			case <-ticker.C:
				elapsed := time.Since(started).Round(time.Second)
				_ = journal.Write(Event{Time: now().UTC(), Type: "OPERATION_PROGRESS", Operation: op.ID, Message: fmt.Sprintf("elapsed=%s", elapsed)})
				notify(progress, Progress{Type: ProgressHeartbeat, Operation: op, Elapsed: elapsed})
			case <-ctx.Done():
				// Never abandon an in-flight mutation: commands honor the
				// context and abort quickly, while local file writes finish
				// their small atomic mutation before the journal closes.
				if !cancelled {
					cancelled = true
					ticker.Stop()
					_ = journal.Write(Event{Time: now().UTC(), Type: "OPERATION_CANCELLING", Operation: op.ID, Message: "waiting for in-flight operation"})
				}
			}
		}
		ticker.Stop()
		if cancelled {
			notify(progress, Progress{Type: ProgressFailed, Operation: op, Elapsed: time.Since(started).Round(time.Second)})
			return execution, ctx.Err()
		}
		if err != nil {
			failed[op.ID] = true
			_ = journal.Write(Event{Time: now().UTC(), Type: "OPERATION_FAILED", Operation: op.ID, Message: err.Error()})
			notify(progress, Progress{Type: ProgressFailed, Operation: op, Elapsed: time.Since(started).Round(time.Second)})
			if ctx.Err() != nil {
				return execution, ctx.Err()
			}
			execution.Failed = append(execution.Failed, Failure{Operation: op, Error: summarizeError(err)})
			continue
		}
		if err := journal.Write(Event{Time: now().UTC(), Type: "OPERATION_COMPLETED", Operation: op.ID}); err != nil {
			return execution, err
		}
		completed := op
		if completed.File != nil && completed.File.Backup {
			completed.Reversible = true
		}
		execution.Completed = append(execution.Completed, completed)
		notify(progress, Progress{Type: ProgressCompleted, Operation: op, Elapsed: time.Since(started).Round(time.Second)})
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
	if actions != 1 {
		return fmt.Errorf("operation %s must contain exactly one command, copy, or file action", op.ID)
	}
	if op.Copy != nil {
		return copyTreeExclusive(op.Copy.Source, op.Copy.Destination)
	}
	if op.File != nil {
		return writeFileAtomic(op.ID, *op.File, journal, now)
	}
	_, err := runner.Run(ctx, op.Command[0], op.Command[1:]...)
	return err
}

func writeFileAtomic(operation string, action model.FileWrite, journal *Journal, now func() time.Time) error {
	if action.SourceHash == "" {
		return fmt.Errorf("file write source hash is required: %s", operation)
	}
	if action.ExpectedMissing == (action.ExpectedHash != "") {
		return fmt.Errorf("file write requires exactly one destination precondition: %s", operation)
	}
	source, sourceInfo, err := content.OpenRegularFile(action.Source)
	if err != nil {
		return fmt.Errorf("validate file write source: %w", err)
	}
	defer source.Close()
	sourceHash, err := content.HashOpenFile(source)
	if err != nil {
		return fmt.Errorf("hash file write source: %w", err)
	}
	if sourceHash != action.SourceHash {
		return fmt.Errorf("file write source hash mismatch: %s", action.Source)
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return err
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
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(parent, ".omarchy-blueprint-file-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	mode := sourceInfo.Mode().Perm()
	if destinationInfo != nil {
		mode = destinationInfo.Mode().Perm()
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := io.Copy(temp, source); err != nil {
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
	if err := os.Rename(tempPath, action.Destination); err != nil {
		return err
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
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
	return info, nil
}

func copyTreeExclusive(source, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("destination already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(parent, ".omarchy-blueprint-copy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if err := copyTreeContents(source, temp); err != nil {
		return err
	}
	return os.Rename(temp, destination)
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
			return os.MkdirAll(target, info.Mode().Perm())
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
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	message := strings.TrimSpace(lines[len(lines)-1])
	if len(message) > 300 {
		message = message[:297] + "..."
	}
	return message
}
