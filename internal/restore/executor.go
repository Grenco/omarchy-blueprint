package restore

import (
	"context"
	"fmt"
	"time"

	"github.com/graeme/omarchy-blueprint/internal/command"
	"github.com/graeme/omarchy-blueprint/internal/model"
)

func Execute(ctx context.Context, runner command.Runner, plan model.RestorePlan, journal *Journal, now func() time.Time) error {
	if err := journal.Write(Event{Time: now().UTC(), Type: "PLAN_CREATED", Message: fmt.Sprintf("%d operations", len(plan.Operations))}); err != nil {
		return err
	}
	for _, op := range plan.Operations {
		if err := journal.Write(Event{Time: now().UTC(), Type: "OPERATION_STARTED", Operation: op.ID}); err != nil {
			return err
		}
		_, err := runner.Run(ctx, op.Command[0], op.Command[1:]...)
		if err != nil {
			_ = journal.Write(Event{Time: now().UTC(), Type: "OPERATION_FAILED", Operation: op.ID, Message: err.Error()})
			return fmt.Errorf("operation %s failed: %w", op.ID, err)
		}
		if err := journal.Write(Event{Time: now().UTC(), Type: "OPERATION_COMPLETED", Operation: op.ID}); err != nil {
			return err
		}
	}
	return nil
}
