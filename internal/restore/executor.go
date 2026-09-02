package restore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/graeme/omarchy-blueprint/internal/command"
	"github.com/graeme/omarchy-blueprint/internal/model"
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
	if err := journal.Write(Event{Time: now().UTC(), Type: "PLAN_CREATED", Message: fmt.Sprintf("%d operations", len(plan.Operations))}); err != nil {
		return execution, err
	}
	for _, op := range plan.Operations {
		started := time.Now()
		if err := journal.Write(Event{Time: now().UTC(), Type: "OPERATION_STARTED", Operation: op.ID}); err != nil {
			return execution, err
		}
		notify(progress, Progress{Type: ProgressStarted, Operation: op})
		result := make(chan error, 1)
		go func() {
			var err error
			if op.Copy != nil {
				err = copyTreeExclusive(op.Copy.Source, op.Copy.Destination)
			} else if len(op.Command) == 0 {
				err = fmt.Errorf("operation %s has no command or copy action", op.ID)
			} else {
				_, err = runner.Run(ctx, op.Command[0], op.Command[1:]...)
			}
			result <- err
		}()
		ticker := time.NewTicker(heartbeat)
		var err error
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
				err = ctx.Err()
				break wait
			}
		}
		ticker.Stop()
		if err != nil {
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
		execution.Completed = append(execution.Completed, op)
		notify(progress, Progress{Type: ProgressCompleted, Operation: op, Elapsed: time.Since(started).Round(time.Second)})
	}
	return execution, nil
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
	temp, err := os.MkdirTemp(parent, ".omarchy-blueprint-theme-*")
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
			return fmt.Errorf("theme snapshot contains unsupported symlink: %s", relative)
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
			return fmt.Errorf("theme snapshot contains unsupported file: %s", relative)
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
