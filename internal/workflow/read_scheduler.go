package workflow

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Selected by the serial/2/4 live spike and approved by the human. This is
// coarse read-provider concurrency, never per command/unit/file concurrency.
const readProviderConcurrency = 4

func observationCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// observeProviders joins every launched worker and keeps results in input
// order. An error returns no partial report. Only narrow read views may enter.
// Independent meaningful failures are selected in provider order after join;
// secondary cancellation never replaces the initiating meaningful failure.
func observeProviders(ctx context.Context, providers []ReadProvider, limit int, observe func(context.Context, ReadProvider) (ProviderStatus, error)) ([]ProviderStatus, error) {
	if limit < 1 {
		return nil, fmt.Errorf("read provider concurrency must be positive")
	}
	for _, p := range providers {
		if _, authority := p.(Provider); authority {
			return nil, fmt.Errorf("cannot schedule authoritative provider %s", p.ID())
		}
		if _, authority := p.(interface {
			Verify(context.Context, profile.Data, RestoreContext) (model.VerificationResult, error)
		}); authority {
			return nil, fmt.Errorf("cannot schedule verification provider %s", p.ID())
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]ProviderStatus, len(providers))
	if len(providers) == 0 {
		return results, nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make([]error, len(providers))
	var mu sync.Mutex
	next := 0
	failed := false
	worker := func() {
		for {
			mu.Lock()
			if failed || workCtx.Err() != nil || next == len(providers) {
				mu.Unlock()
				return
			}
			i := next
			next++
			mu.Unlock()
			results[i], errs[i] = observe(workCtx, providers[i])
			if errs[i] != nil {
				// CommandContext reports killed/unstarted commands through RunError
				// rather than wrapping ctx.Err. Do not let a canceled subprocess
				// at an earlier provider index replace the failure that canceled it.
				var runErr *command.RunError
				var exitErr *exec.ExitError
				if workCtx.Err() != nil && errors.As(errs[i], &runErr) && runErr.ExitCode < 0 && errors.As(runErr.Err, &exitErr) {
					errs[i] = workCtx.Err()
				}
				mu.Lock()
				failed = true
				cancel()
				mu.Unlock()
				return
			}
		}
	}
	if limit == 1 || len(providers) == 1 {
		worker()
	} else {
		var wg sync.WaitGroup
		for i := 0; i < min(limit, len(providers)); i++ {
			wg.Add(1)
			go func() { defer wg.Done(); worker() }()
		}
		wg.Wait()
	}
	for _, err := range errs {
		if err != nil && !observationCanceled(err) {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}
