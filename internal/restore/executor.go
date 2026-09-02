package restore

import (
	"context"
	"fmt"
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

func Execute(ctx context.Context, runner command.Runner, plan model.RestorePlan, journal *Journal, now func() time.Time, heartbeat time.Duration, progress ProgressFunc) error {
	if err := journal.Write(Event{Time: now().UTC(), Type: "PLAN_CREATED", Message: fmt.Sprintf("%d operations", len(plan.Operations))}); err != nil {
		return err
	}
	for _, op := range plan.Operations {
		started := time.Now()
		if err := journal.Write(Event{Time: now().UTC(), Type: "OPERATION_STARTED", Operation: op.ID}); err != nil {
			return err
		}
		notify(progress, Progress{Type: ProgressStarted, Operation: op})
		result := make(chan error, 1)
		go func() {
			_, err := runner.Run(ctx, op.Command[0], op.Command[1:]...)
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
			return fmt.Errorf("operation %s failed: %w", op.ID, err)
		}
		if err := journal.Write(Event{Time: now().UTC(), Type: "OPERATION_COMPLETED", Operation: op.ID}); err != nil {
			return err
		}
		notify(progress, Progress{Type: ProgressCompleted, Operation: op, Elapsed: time.Since(started).Round(time.Second)})
	}
	return nil
}

func notify(progress ProgressFunc, event Progress) {
	if progress != nil {
		progress(event)
	}
}
