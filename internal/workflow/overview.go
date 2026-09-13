package workflow

import (
	"context"
	"fmt"
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
)

type AttentionSeverity string

const (
	AttentionDecision AttentionSeverity = "decision"
	AttentionWarning  AttentionSeverity = "warning"
	AttentionDrift    AttentionSeverity = "drift"
	AttentionInfo     AttentionSeverity = "info"
)

type AttentionItem struct {
	Severity AttentionSeverity
	Provider string
	Kind     string
	Ref      string
	Summary  string
	Target   string
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
		if provider.ConfigScan != nil {
			appendConfigAttention(&overview, *provider.ConfigScan)
		}
		for _, change := range provider.Changes {
			overview.Items = append(overview.Items, AttentionItem{Severity: AttentionDrift, Provider: provider.ID, Kind: change.Kind, Ref: change.Name, Summary: change.Summary, Target: provider.ID})
		}
		for id, state := range provider.ResourceGit {
			if state.StagedTracked > 0 || state.UnstagedTracked > 0 || len(state.Untracked) > 0 {
				overview.Items = append(overview.Items, AttentionItem{Severity: AttentionInfo, Provider: provider.ID, Kind: "git", Ref: id, Summary: fmt.Sprintf("resource %s has uncaptured local Git state", id), Target: "resources"})
			}
		}
		if len(overview.Items) == before {
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

func appendConfigAttention(overview *Overview, scan configprovider.ScanSummary) {
	for _, candidate := range scan.Candidates {
		severity := AttentionDrift
		if candidate.Classification == configprovider.ConfigAmbiguousBaseline || candidate.Classification == configprovider.ConfigAmbiguousDeletion {
			severity = AttentionDecision
		}
		if severity == AttentionDecision || candidate.Classification == configprovider.ConfigModifiedBaseline || candidate.Classification == configprovider.ConfigDeletedBaseline || candidate.Classification == configprovider.ConfigAdded {
			overview.Items = append(overview.Items, AttentionItem{Severity: severity, Provider: "config", Kind: string(candidate.Classification), Ref: candidate.Path, Summary: candidate.Path + " " + string(candidate.Classification), Target: "config"})
		}
	}
}

func attentionRank(severity AttentionSeverity) int {
	switch severity {
	case AttentionDecision:
		return 0
	case AttentionWarning:
		return 1
	case AttentionDrift:
		return 2
	default:
		return 3
	}
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
