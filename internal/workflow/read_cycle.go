package workflow

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/diagnostics"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
)

// ReadCycle owns a single private desired-state snapshot and lazy live facts.
// Close before any human pause. No mutation providers/contexts escape it.
type ReadCycle struct {
	observations      *observation.Cycle
	profile           profile.Data
	machine           machine.Selection
	providers         []ReadProvider
	profileGit        profilegit.Service
	info              *observation.Slot[omarchy.Info]
	finalize          func(context.Context, profile.Data, []string, *model.RestorePlan, policy.RestoreOptions) error
	finishDiagnostics func()
}

func (s *Session) BeginRead(ctx context.Context) (*ReadCycle, error) {
	s.configMu.Lock()
	cfg := readConfig{deps: s.deps, opts: s.opts, providers: append([]Provider(nil), s.providers...), profileGit: s.profileGit, finalize: s.finalizeReadRestore}
	s.configMu.Unlock()
	data, selection, err := loadReadSnapshot(ctx, cfg)
	if err != nil {
		return nil, err
	}
	ctx, finish := diagnostics.WithObservation(ctx, cfg.deps.DiagnosticWriter)
	cycle := observation.New(ctx)
	r := &ReadCycle{observations: cycle, profile: data, machine: selection, profileGit: cfg.profileGit, finalize: cfg.finalize}
	r.finishDiagnostics = finish
	for _, p := range cfg.providers {
		if binder, ok := p.(ReadCycleBinder); ok {
			r.providers = append(r.providers, binder.BindReadCycle(cycle))
		} else {
			r.providers = append(r.providers, narrowReadProvider(p))
		}
	}
	r.info = observation.NewSlot(cycle, func(ctx context.Context) (omarchy.Info, error) { return omarchy.Detect(ctx, cfg.deps.Runner) }, func(v omarchy.Info) omarchy.Info { return v })
	if e := r.check(ctx); e != nil {
		r.Close()
		return nil, e
	}
	return r, nil
}
func (r *ReadCycle) Close() { r.observations.Close(); r.finishDiagnostics() }

// Forwarded providers without slots must also receive cycle cancellation.
// Caller cancellation remains local to this projection, not shared loaders.
func (r *ReadCycle) withContext(ctx context.Context) (context.Context, func()) {
	ctx = diagnostics.WithCollectorFrom(ctx, r.observations.Context())
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.observations.Context(), cancel)
	return linked, func() { stop(); cancel() }
}
func (r *ReadCycle) check(ctx context.Context) error {
	if e := r.observations.Err(); e != nil {
		return e
	}
	return ctx.Err()
}
func (r *ReadCycle) ProfileGitStatus(ctx context.Context) (profilegit.Status, error) {
	ctx, stop := r.withContext(ctx)
	defer stop()
	if e := r.check(ctx); e != nil {
		return profilegit.Status{}, e
	}
	return r.profileGit.Status(ctx)
}

func (s *Session) Status(ctx context.Context, only string) (StatusReport, error) {
	r, e := s.BeginRead(ctx)
	if e != nil {
		return StatusReport{}, e
	}
	defer r.Close()
	return r.Status(ctx, only)
}
func (s *Session) CaptureStatus(ctx context.Context) (StatusReport, error) {
	r, e := s.BeginRead(ctx)
	if e != nil {
		return StatusReport{}, e
	}
	defer r.Close()
	return r.CaptureStatus(ctx)
}
func (s *Session) Overview(ctx context.Context) (Overview, error) {
	r, e := s.BeginRead(ctx)
	if e != nil {
		return Overview{}, e
	}
	defer r.Close()
	return r.Overview(ctx)
}
func (s *Session) PolicyTargets(ctx context.Context, id string) ([]TargetInspection, error) {
	r, e := s.BeginRead(ctx)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	return r.PolicyTargets(ctx, id)
}
func (r *ReadCycle) PolicyTargets(ctx context.Context, id string) ([]TargetInspection, error) {
	ctx, stop := r.withContext(ctx)
	defer stop()
	if e := r.check(ctx); e != nil {
		return nil, e
	}
	p, ok := ProviderByID(r.providers, id)
	if !ok {
		return nil, fmt.Errorf("workflow: unknown policy category %q", id)
	}
	targets, e := p.InspectTargets(ctx, profile.CloneData(r.profile))
	if e != nil {
		return nil, e
	}
	if e := r.check(ctx); e != nil {
		return nil, e
	}
	return cloneTargets(targets), nil
}

func (s *Session) InspectCapture(ctx context.Context, only string) (CaptureInspection, error) {
	r, e := s.BeginRead(ctx)
	if e != nil {
		return CaptureInspection{}, e
	}
	defer r.Close()
	return r.InspectCapture(ctx, only)
}
func (s *Session) InspectCaptureMany(ctx context.Context, ids []string) (CaptureInspection, error) {
	r, e := s.BeginRead(ctx)
	if e != nil {
		return CaptureInspection{}, e
	}
	defer r.Close()
	return r.InspectCaptureMany(ctx, ids)
}
func (r *ReadCycle) InspectCapture(ctx context.Context, only string) (CaptureInspection, error) {
	ctx, stop := r.withContext(ctx)
	defer stop()
	if e := r.check(ctx); e != nil {
		return CaptureInspection{}, e
	}
	providers := r.providers
	if only != "" {
		p, ok := ProviderByID(providers, only)
		if !ok {
			return CaptureInspection{}, fmt.Errorf("unknown category %s", only)
		}
		providers = []ReadProvider{p}
	}
	categories := map[string][]CaptureTarget{}
	for _, p := range providers {
		if only == "" {
			if enabled, ok := readCategoryState(p); ok && !enabled {
				continue
			}
		}
		items, e := inspectProviderCapture(ctx, p, profile.CloneData(r.profile), r.resolveCaptureTarget)
		if e != nil {
			return CaptureInspection{}, e
		}
		if len(items) > 0 {
			categories[p.ID()] = items
		}
	}
	return CaptureInspection{Categories: categories}, nil
}
func (r *ReadCycle) InspectCaptureMany(ctx context.Context, ids []string) (CaptureInspection, error) {
	merged := CaptureInspection{Categories: map[string][]CaptureTarget{}}
	for _, id := range ids {
		part, e := r.InspectCapture(ctx, id)
		if e != nil {
			return CaptureInspection{}, e
		}
		for category, targets := range part.Categories {
			merged.Categories[category] = targets
		}
	}
	return merged, nil
}
