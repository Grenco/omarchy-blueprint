package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
)

type RestoreMode string

const (
	RestoreNormal RestoreMode = "normal"
	RestoreForced RestoreMode = "forced"
)

type Outcome string

const (
	OutcomeCreate  Outcome = "create"
	OutcomeModify  Outcome = "modify"
	OutcomeReplace Outcome = "replace"
	OutcomeDelete  Outcome = "delete"
	OutcomeSkip    Outcome = "skip"
	OutcomeNoop    Outcome = "noop"
)

type Consequence struct {
	Provider    string
	Resource    string
	OperationID string
	Normal      Outcome
	Forced      Outcome
	Difference  string
	Risk        model.Risk
	Diff        *inspection.DiffDocument
}

type RestoreComparison struct {
	Normal       model.RestorePlan
	Forced       model.RestorePlan
	Consequences []Consequence
}

type RestoreResult struct {
	Mode         RestoreMode
	Plan         model.RestorePlan
	Execution    restore.Result
	Verification model.VerificationResult
	Journal      string
	Applied      bool
}

func (s *Session) CompareRestore(ctx context.Context, onlyProvider string) (RestoreComparison, error) {
	normal, _, err := s.restorePlan(ctx, onlyProvider, RestoreNormal)
	if err != nil {
		return RestoreComparison{}, err
	}
	forced, _, err := s.restorePlan(ctx, onlyProvider, RestoreForced)
	if err != nil {
		return RestoreComparison{}, err
	}
	return RestoreComparison{Normal: normal, Forced: forced, Consequences: restoreConsequences(normal, forced)}, nil
}

// PlanRestore returns the same validated plan used by comparison and apply.
func (s *Session) PlanRestore(ctx context.Context, onlyProvider string, mode RestoreMode) (model.RestorePlan, error) {
	plan, _, err := s.restorePlan(ctx, onlyProvider, mode)
	return plan, err
}

// ApplyRestore always replans after approval, so the executor validates current
// filesystem preconditions rather than relying on a preview-time plan.
func (s *Session) ApplyRestore(ctx context.Context, onlyProvider string, mode RestoreMode) (RestoreResult, error) {
	plan, providers, err := s.restorePlan(ctx, onlyProvider, mode)
	if err != nil {
		return RestoreResult{Mode: mode}, err
	}
	result := RestoreResult{Mode: mode, Plan: plan}
	if len(plan.Operations) == 0 {
		result.Verification, err = verifyRestoreProviders(ctx, s.profile, providers)
		return result, err
	}
	stateHome, err := s.deps.StateHome()
	if err != nil {
		return result, err
	}
	journal, err := restore.NewJournal(stateHome, s.deps.Now())
	if err != nil {
		return result, fmt.Errorf("create restore journal: %w", err)
	}
	defer journal.Close()
	result.Journal, result.Applied = journal.Path, true
	result.Execution, err = restore.Execute(ctx, s.deps.Runner, plan, journal, s.deps.Now, 5_000_000_000, nil)
	if err != nil {
		return result, err
	}
	result.Verification, err = verifyRestoreProviders(ctx, s.profile, providers)
	if err != nil {
		return result, err
	}
	_ = journal.Write(restore.Event{Time: s.deps.Now().UTC(), Type: "VERIFY_COMPLETED", Message: fmt.Sprintf("ok=%t", result.Verification.OK)})
	if len(result.Execution.Failed) > 0 {
		return result, fmt.Errorf("restore completed with %d failed operation(s)", len(result.Execution.Failed))
	}
	return result, nil
}

func (s *Session) restorePlan(ctx context.Context, only string, mode RestoreMode) (model.RestorePlan, []RestoreProvider, error) {
	if mode != RestoreNormal && mode != RestoreForced {
		return model.RestorePlan{}, nil, fmt.Errorf("unknown restore mode %q", mode)
	}
	if err := s.Reload(); err != nil {
		return model.RestorePlan{}, nil, err
	}
	selected := capturedProviders(s.providers, s.profile)
	if only != "" {
		provider, ok := ProviderByID(s.providers, only)
		if !ok {
			return model.RestorePlan{}, nil, fmt.Errorf("unknown category %s", only)
		}
		if !provider.Captured(s.profile) {
			return model.RestorePlan{}, nil, CaptureRequiredError(provider.ID())
		}
		selected = []Provider{provider}
	}
	providers := make([]RestoreProvider, 0, len(selected))
	for _, provider := range selected {
		restoreProvider, ok := provider.(RestoreProvider)
		if !ok {
			return model.RestorePlan{}, nil, fmt.Errorf("provider %s does not support restore", provider.ID())
		}
		providers = append(providers, restoreProvider)
	}
	info, err := omarchy.Detect(ctx, s.deps.Runner)
	if err != nil {
		return model.RestorePlan{}, nil, err
	}
	plan := model.RestorePlan{ProfileVersion: s.profile.Manifest.Schema, OmarchyFrom: s.profile.Manifest.Omarchy.CapturedVersion, OmarchyTo: info.Version}
	for _, provider := range providers {
		part, err := provider.Plan(ctx, s.profile, info, mode)
		if err != nil {
			return model.RestorePlan{}, nil, fmt.Errorf("plan %s restore: %w", provider.ID(), err)
		}
		plan.Operations, plan.Skipped = append(plan.Operations, part.Operations...), append(plan.Skipped, part.Skipped...)
	}
	if s.finalizeRestore != nil {
		if err := s.finalizeRestore(ctx, s.profile, selected, &plan, mode); err != nil {
			return model.RestorePlan{}, nil, fmt.Errorf("finalize restore plan: %w", err)
		}
	} else if err := restore.ValidatePlan(plan); err != nil {
		return model.RestorePlan{}, nil, err
	}
	return plan, providers, nil
}

func verifyRestoreProviders(ctx context.Context, data profile.Data, providers []RestoreProvider) (model.VerificationResult, error) {
	result := model.VerificationResult{OK: true}
	for _, provider := range providers {
		verification, err := provider.Verify(ctx, data)
		if err != nil {
			return model.VerificationResult{}, fmt.Errorf("verify %s restore: %w", provider.ID(), err)
		}
		result.OK = result.OK && verification.OK
		result.Missing = append(result.Missing, verification.Missing...)
	}
	return result, nil
}

type restoreEntry struct {
	op   *model.Operation
	skip *model.Skipped
}

func restoreConsequences(normal, forced model.RestorePlan) []Consequence {
	index := func(plan model.RestorePlan) map[string]restoreEntry {
		items := map[string]restoreEntry{}
		for i := range plan.Operations {
			op := &plan.Operations[i]
			items[operationKey(*op)] = restoreEntry{op: op}
		}
		for i := range plan.Skipped {
			skip := &plan.Skipped[i]
			key := skip.Provider + "\x00" + skip.Resource
			if _, exists := items[key]; !exists {
				items[key] = restoreEntry{skip: skip}
			}
		}
		return items
	}
	a, b := index(normal), index(forced)
	keys := make([]string, 0, len(a)+len(b))
	for key := range a {
		keys = append(keys, key)
	}
	for key := range b {
		if _, ok := a[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	items := make([]Consequence, 0, len(keys))
	for _, key := range keys {
		n, f := a[key], b[key]
		consequence := Consequence{Normal: entryOutcome(n), Forced: entryOutcome(f)}
		base := n
		if base.op == nil && f.op != nil {
			base = f
		}
		if base.op != nil {
			consequence.Provider, consequence.Resource, consequence.OperationID, consequence.Risk = base.op.Provider, base.op.Resource, base.op.ID, base.op.Risk
			consequence.Diff = operationDiff(*base.op)
		} else if base.skip != nil {
			consequence.Provider, consequence.Resource = base.skip.Provider, base.skip.Resource
		}
		consequence.Difference = consequenceDescription(consequence.Normal, consequence.Forced)
		items = append(items, consequence)
	}
	return items
}

func operationKey(op model.Operation) string { return op.Provider + "\x00" + op.Resource }
func entryOutcome(entry restoreEntry) Outcome {
	if entry.op == nil {
		if entry.skip != nil {
			return OutcomeSkip
		}
		return OutcomeNoop
	}
	return operationOutcome(*entry.op)
}
func operationOutcome(op model.Operation) Outcome {
	if op.Delete != nil {
		return OutcomeDelete
	}
	if op.File != nil {
		if op.File.ReplaceExisting {
			return OutcomeReplace
		}
		if op.File.ExpectedMissing {
			return OutcomeCreate
		}
		return OutcomeModify
	}
	if op.Symlink != nil {
		if op.Symlink.ReplaceExisting {
			return OutcomeReplace
		}
		if op.Symlink.ExpectedMissing {
			return OutcomeCreate
		}
		return OutcomeModify
	}
	if op.Directory != nil {
		return OutcomeCreate
	}
	if op.Copy != nil || op.GitPatch != nil || len(op.Command) > 0 {
		return OutcomeModify
	}
	return OutcomeNoop
}
func consequenceDescription(normal, forced Outcome) string {
	if normal == forced {
		return "same effect in normal and forced modes"
	}
	return fmt.Sprintf("normal: %s; forced: %s", normal, forced)
}

func operationDiff(op model.Operation) *inspection.DiffDocument {
	if op.File == nil {
		return nil
	}
	newBytes, ok := fileContent(*op.File)
	if !ok {
		return &inspection.DiffDocument{Kind: inspection.DiffUnavailable, Metadata: []inspection.DiffFact{{Key: "reason", Value: "restore content is unavailable"}}}
	}
	oldBytes, ok := boundedFile(op.File.Destination)
	if !ok {
		return &inspection.DiffDocument{Kind: inspection.DiffUnavailable, OldLabel: op.File.Destination, NewLabel: op.Resource, Metadata: []inspection.DiffFact{{Key: "reason", Value: "existing file is unavailable"}}}
	}
	doc := inspection.BuildTextDiff(op.File.Destination, oldBytes, op.Resource, newBytes)
	return &doc
}
func fileContent(file model.FileWrite) ([]byte, bool) {
	if file.Content != nil {
		return file.Content, true
	}
	return boundedFile(file.Source)
}
func boundedFile(path string) ([]byte, bool) {
	if path == "" {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, inspection.MaxPreviewBytes+1))
	return data, err == nil
}
