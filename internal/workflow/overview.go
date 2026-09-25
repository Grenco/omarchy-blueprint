package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
)

type AttentionSeverity string

const (
	AttentionDecision AttentionSeverity = "decision"
	AttentionWarning  AttentionSeverity = "warning"
	AttentionDrift    AttentionSeverity = "drift"
	AttentionInfo     AttentionSeverity = "info"
	// AttentionIntentional is a real difference that current policy makes
	// deliberately non-actionable on this machine. It is not a warning.
	AttentionIntentional AttentionSeverity = "intentional"
)

type AttentionItem struct {
	Severity AttentionSeverity
	Provider string
	Kind     string
	Ref      string
	Summary  string
	Target   string
	// Reason explains, for an intentional difference, which policy makes
	// it intentional.
	Reason string
}

type Overview struct {
	ProfileName string
	MachineName string
	Items       []AttentionItem
	Healthy     []string
	ProfileGit  *profilegit.Status
}

// Overview combines local, already-available state. Profile Git Status does
// not fetch, so opening the decision inbox never refreshes remote state.
func (s *Session) Overview(ctx context.Context) (Overview, error) {
	report, err := s.Status(ctx, "")
	if err != nil {
		return Overview{}, err
	}
	git, err := s.ProfileGitStatus(ctx)
	if err != nil {
		return Overview{}, err
	}
	overview := Overview{ProfileName: report.Profile.Manifest.Profile.Name, MachineName: report.Machine.Name, ProfileGit: &git}
	for _, provider := range report.Providers {
		before := len(overview.Items)
		differences := s.newDifferenceClassifier(ctx, provider.ID)
		if provider.ConfigScan != nil {
			for _, item := range configAttention(*provider.ConfigScan, report.Profile.Config) {
				if item.Severity == AttentionDrift {
					differences.classify(&item, item.Ref, true)
				}
				overview.Items = append(overview.Items, item)
			}
		}
		for _, change := range provider.Changes {
			item := AttentionItem{Severity: AttentionDrift, Provider: provider.ID, Kind: change.Kind, Ref: change.Name, Summary: change.Summary, Target: provider.ID}
			key, attributed := differences.key(change)
			differences.classify(&item, key, attributed)
			overview.Items = append(overview.Items, item)
		}
		for id, state := range provider.ResourceGit {
			if state.StagedTracked > 0 || state.UnstagedTracked > 0 || len(state.Untracked) > 0 {
				overview.Items = append(overview.Items, AttentionItem{Severity: AttentionInfo, Provider: provider.ID, Kind: "git", Ref: id, Summary: fmt.Sprintf("resource %s has uncaptured local Git state", id), Target: "resources"})
			}
		}
		if !hasActionable(overview.Items[before:]) {
			overview.Healthy = append(overview.Healthy, provider.ID)
		}
	}
	for _, item := range report.Profile.Machines.Items {
		for _, mapping := range item.ResourcePaths {
			if !hasResource(report.Profile.Resources.Items, mapping.Resource) {
				overview.Items = append(overview.Items, AttentionItem{Severity: AttentionWarning, Provider: "machines", Kind: "mapping", Ref: item.Name + ":" + mapping.Resource, Summary: fmt.Sprintf("%s mapping for removed resource %s is dormant", item.Name, mapping.Resource), Target: "machines"})
			}
		}
	}
	if hasManagedProfileChanges(git) {
		overview.Items = append(overview.Items, AttentionItem{Severity: AttentionInfo, Provider: "profile-git", Kind: "changes", Summary: fmt.Sprintf("%d managed profile changes", managedProfileChanges(git)), Target: "sync"})
	}
	sort.Slice(overview.Items, func(i, j int) bool {
		left, right := overview.Items[i], overview.Items[j]
		if attentionRank(left.Severity) != attentionRank(right.Severity) {
			return attentionRank(left.Severity) < attentionRank(right.Severity)
		}
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		if left.Ref != right.Ref {
			return left.Ref < right.Ref
		}
		return left.Summary < right.Summary
	})
	sort.Strings(overview.Healthy)
	return overview, nil
}

// configAttention reports the Config scan findings Config's own Diff does
// not: ambiguous paths needing a decision, and deleted Omarchy defaults
// Capture would newly record. Added and modified paths are left to the
// profile-aware Diff, which already omits ones captured and still in sync.
func configAttention(scan configprovider.ScanSummary, saved profile.Configs) []AttentionItem {
	tracked := map[string]bool{}
	for _, file := range saved.Files {
		tracked[file.Path] = true
	}
	for _, del := range saved.Deletes {
		tracked[del.Path] = true
	}
	items := []AttentionItem{}
	for _, candidate := range scan.Candidates {
		severity := AttentionDrift
		if candidate.Classification == configprovider.ConfigAmbiguousBaseline || candidate.Classification == configprovider.ConfigAmbiguousDeletion {
			severity = AttentionDecision
		}
		newDeletion := candidate.Classification == configprovider.ConfigDeletedBaseline && !tracked[candidate.Path]
		if severity == AttentionDecision || newDeletion {
			items = append(items, AttentionItem{Severity: severity, Provider: "config", Kind: string(candidate.Classification), Ref: candidate.Path, Summary: candidate.Path + " " + string(candidate.Classification), Target: "config"})
		}
	}
	return items
}

func attentionRank(severity AttentionSeverity) int {
	switch severity {
	case AttentionDecision:
		return 0
	case AttentionWarning:
		return 1
	case AttentionDrift:
		return 2
	case AttentionIntentional:
		return 4
	default:
		return 3
	}
}

func hasActionable(items []AttentionItem) bool {
	for _, item := range items {
		if item.Severity != AttentionIntentional {
			return true
		}
	}
	return false
}

// differenceClassifier decides whether a factual difference is an
// intentional machine difference: neither Capture nor Restore would act on
// it for the active machine, and intent (policy, or the machine's Restore
// options) rather than merely a safety block is why. Capture candidacy
// follows the same outcome the Capture review shows; Restore candidacy
// comes from the real Restore plan under the machine's effective options.
// Anything it cannot attribute, resolve, or plan stays actionable, so a
// real difference is never hidden by accident. Targets are inspected, and
// Restore planned, lazily and only for categories that need it.
type differenceClassifier struct {
	ctx      context.Context
	session  *Session
	category string
	provider Provider
	targets  map[string]TargetInspection
	loaded   bool

	planned bool
	// restoreAll is set when the plan cannot be attributed target by
	// target (an unattributable operation, or a key outside the inspected
	// inventory), so every difference is conservatively a Restore
	// candidate.
	restoreAll     bool
	restoreKeys    map[string]bool
	restoreOptions policy.RestoreOptions
}

func (s *Session) newDifferenceClassifier(ctx context.Context, category string) *differenceClassifier {
	provider, _ := ProviderByID(s.providers, category)
	return &differenceClassifier{ctx: ctx, session: s, category: category, provider: provider}
}

func (c *differenceClassifier) key(change model.Change) (string, bool) {
	resolver, ok := c.provider.(ChangeTargetResolver)
	if !ok {
		return "", false
	}
	key, attributed := resolver.ChangeTargetKey(change)
	return key, attributed && key != ""
}

func (c *differenceClassifier) target(key string) (TargetInspection, bool) {
	if !c.loaded {
		c.loaded = true
		if c.provider == nil {
			return TargetInspection{}, false
		}
		targets, err := c.provider.InspectTargets(c.ctx, c.session.profile)
		if err != nil {
			return TargetInspection{}, false
		}
		c.targets = make(map[string]TargetInspection, len(targets))
		for _, target := range targets {
			c.targets[target.Key] = target
		}
	}
	target, ok := c.targets[key]
	return target, ok
}

// restoreCandidate reports whether this category's Restore plan, under the
// machine's effective Restore options, has an operation acting on key.
func (c *differenceClassifier) restoreCandidate(key string) bool {
	if !c.planned {
		c.planned = true
		resolver, ok := c.provider.(RestoreTargetResolver)
		plan, _, _, options, err := c.session.restorePlan(c.ctx, c.category, nil)
		if !ok || err != nil {
			c.restoreAll = true
			return true
		}
		c.restoreOptions, c.restoreKeys = options, map[string]bool{}
		for _, op := range plan.Operations {
			if op.Provider != c.category {
				continue
			}
			keys, ok := resolver.RestoreOperationTargetKeys(op)
			if !ok {
				c.restoreAll = true
				break
			}
			for _, key := range keys {
				// A key outside the inspected inventory means attribution
				// is wrong somewhere; never let it hide the real target.
				if _, known := c.target(key); !known {
					c.restoreAll = true
					break
				}
				c.restoreKeys[key] = true
			}
			if c.restoreAll {
				break
			}
		}
	}
	return c.restoreAll || c.restoreKeys[key]
}

func (c *differenceClassifier) classify(item *AttentionItem, key string, attributed bool) {
	if !attributed {
		return
	}
	target, ok := c.target(key)
	if !ok {
		return
	}
	_, capture, err := c.session.resolveCaptureTarget(c.ctx, c.category, target)
	if err != nil {
		return
	}
	_, restore, err := c.session.resolveRestoreTarget(c.ctx, c.category, target)
	if err != nil {
		return
	}
	// The change itself says this target differs from saved state.
	if (CaptureTarget{Outcome: captureOutcomeFor(target, capture, true), Decision: capture}).ReviewGroup() == CaptureReviewChanges {
		return
	}
	restoreWanted := target.RestoreEligible && restore.Restore
	if restoreWanted && c.restoreCandidate(key) {
		return
	}
	captureByPolicy := target.CaptureEligible && !capture.Capture
	restoreByPolicy := target.RestoreEligible && !restore.Restore
	if !captureByPolicy && !restoreByPolicy && !restoreWanted {
		return
	}
	captureReason := "Capture has nothing to record"
	switch {
	case !target.CaptureEligible:
		captureReason = "Capture cannot change it for safety reasons"
	case captureByPolicy:
		captureReason = "Capture preserves the profile's saved state"
	}
	restoreReason := "Restore cannot apply it for safety reasons"
	switch {
	case restoreByPolicy:
		restoreReason = "Restore skips it"
	case restoreWanted:
		restoreReason = "Restore leaves it as is with " + restoreOptionsLabel(c.restoreOptions) + " options"
	}
	where := "under Profile defaults"
	if machine := c.session.machine.Name; machine != "" {
		where = "on " + machine
	}
	item.Severity = AttentionIntentional
	item.Reason = captureReason + " and " + restoreReason + " " + where + "."
}

func restoreOptionsLabel(options policy.RestoreOptions) string {
	title := func(value string) string {
		if value == "" {
			return value
		}
		return strings.ToUpper(value[:1]) + value[1:]
	}
	return title(string(options.Conflicts)) + " + " + title(string(options.Convergence))
}

func managedProfileChanges(status profilegit.Status) int {
	count := 0
	for _, change := range status.Changes {
		if change.Managed {
			count++
		}
	}
	return count
}
func hasManagedProfileChanges(status profilegit.Status) bool {
	return managedProfileChanges(status) > 0
}
