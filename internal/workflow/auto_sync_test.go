package workflow

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/autosync"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

var autoSyncTestHost = strings.Repeat("d", 64)

// autoSyncFixtureRegistry delivers one entry so the whole pipeline can be
// exercised; the production registry delivers nothing in Phase 11 PR A.
func autoSyncFixtureRegistry() autosync.Registry {
	return autosync.Registry{Version: autosync.CapabilityVersion, Entries: []autosync.Entry{{
		Family: "packages.fixture", Class: autosync.ClassConservative, Changes: []model.EffectChange{model.EffectAdd}, Payloads: []model.EffectPayload{model.PayloadData}, Delivered: true, Increment: "I1",
	}}}
}

func autoSyncFixtureEffect() *model.Effect {
	return &model.Effect{Family: "packages.fixture", Change: model.EffectAdd, Payload: model.PayloadData, Source: model.SourceArtifact, Activation: model.ActivationNone, Privilege: model.PrivilegeUser, Interaction: model.InteractionNone, Recovery: model.RecoveryNone, Verifiable: true}
}

func autoSyncGrant(t *testing.T, s *Session, preset autosync.Preset, categories ...string) autosync.Grant {
	t.Helper()
	info, err := s.profileGit.CurrentRevisionInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	root, err := machine.CanonicalProfileRoot(s.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	g := autosync.DefaultGrant()
	g.Setting, g.Preset = autosync.SettingOn, preset
	g.Identity = autosync.Identity{ProfileRoot: root, HostID: autoSyncTestHost, Machine: "test-machine", Branch: info.Branch, Upstream: info.Upstream, RemoteFingerprint: info.OriginFingerprint}
	g.Categories = autosync.PresetScope(categories)
	g.Approved = g.AuthorityDigest()
	return g
}

// remoteChange publishes a package addition from the peer clone.
func remoteChange(t *testing.T, d profile.Data, peer string) {
	t.Helper()
	d.Packages.Official = append(d.Packages.Official, "remote")
	if err := profile.Save(peer, d); err != nil {
		t.Fatal(err)
	}
	syncGit(t, peer, "add", ".")
	syncGit(t, peer, "commit", "-m", "remote")
	syncGit(t, peer, "push")
}

type autoSyncSnapshot struct {
	tree, head, upstream string
	cursor               []byte
	live                 []string
	stateEntries         []string
}

func takeAutoSyncSnapshot(t *testing.T, s *Session, p *executionSyncProvider) autoSyncSnapshot {
	t.Helper()
	state, _ := s.deps.StateHome()
	dir, _ := machine.ProfileStateDir(state, s.ProfileDir())
	cursor, _ := os.ReadFile(filepath.Join(dir, "sync.toml"))
	var entries []string
	list, _ := os.ReadDir(dir)
	for _, e := range list {
		entries = append(entries, e.Name())
	}
	return autoSyncSnapshot{
		tree: syncFixtureTreeHash(t, s.ProfileDir()), head: syncGit(t, s.ProfileDir(), "rev-parse", "HEAD"),
		upstream: syncGit(t, s.ProfileDir(), "rev-parse", "@{u}"), cursor: cursor,
		live: append([]string(nil), p.live.Packages.Official...), stateEntries: entries,
	}
}

func TestAutoSyncPreviewIsReadOnly(t *testing.T) {
	s, p, _, d, peer := executionSyncFixture(t)
	remoteChange(t, d, peer)
	before := takeAutoSyncSnapshot(t, s, p)
	grant := autoSyncGrant(t, s, autosync.PresetHandsOff, autosync.Categories...)

	preview, err := s.PreviewAutoSync(context.Background(), AutoSyncPreviewOptions{Grant: grant, HostID: autoSyncTestHost})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Fetched || preview.Eligible || !reflect.DeepEqual(before, takeAutoSyncSnapshot(t, s, p)) {
		t.Fatalf("no-fetch preview changed state or claimed eligibility: %+v", preview)
	}

	preview, err = s.PreviewAutoSync(context.Background(), AutoSyncPreviewOptions{Fetch: true, Grant: grant, HostID: autoSyncTestHost})
	if err != nil {
		t.Fatal(err)
	}
	after := takeAutoSyncSnapshot(t, s, p)
	if !preview.Fetched || after.upstream == before.upstream {
		t.Fatal("fetch was not performed when asked")
	}
	// Only the remote-tracking ref may move.
	after.upstream = before.upstream
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("preview wrote state:\nbefore %+v\nafter  %+v", before, after)
	}
	for _, stage := range *p.ledger {
		if stage == "restore-apply" || stage == "push" || stage == "capture" {
			t.Fatalf("preview mutated: %v", *p.ledger)
		}
	}
	// A pending fast-forward needs snapshot planning (Phase 11 PR C).
	if preview.Eligible || !containsReason(preview.Reasons, AutoSyncPlanUnavailable) {
		t.Fatalf("behind plan: %+v", preview)
	}
}

func TestAutoSyncPreviewUpToDateAndManualSyncRequired(t *testing.T) {
	s, _, _, _, _ := executionSyncFixture(t)
	grant := autoSyncGrant(t, s, autosync.PresetBalanced, "packages")
	preview, err := s.PreviewAutoSync(context.Background(), AutoSyncPreviewOptions{Grant: grant, HostID: autoSyncTestHost})
	if err != nil || !preview.UpToDate || preview.Eligible || len(preview.Reasons) != 0 {
		t.Fatalf("settled computer: %+v %v", preview, err)
	}
	state, _ := s.deps.StateHome()
	if err := (syncengine.CursorStore{StateHome: state, ProfileRoot: s.ProfileDir()}).Remove(); err != nil {
		t.Fatal(err)
	}
	preview, err = s.PreviewAutoSync(context.Background(), AutoSyncPreviewOptions{Grant: grant, HostID: autoSyncTestHost})
	if err != nil || preview.UpToDate || preview.Eligible || !containsReason(preview.Reasons, AutoSyncManualSyncRequired) {
		t.Fatalf("no completed Sync: %+v %v", preview, err)
	}
}

func TestAutoSyncPreviewRefusesLocalWork(t *testing.T) {
	s, _, _, d, _ := executionSyncFixture(t)
	d.Packages.Official = append(d.Packages.Official, "local")
	if err := profile.Save(s.ProfileDir(), d); err != nil {
		t.Fatal(err)
	}
	syncGit(t, s.ProfileDir(), "add", ".")
	syncGit(t, s.ProfileDir(), "commit", "-m", "local")
	grant := autoSyncGrant(t, s, autosync.PresetHandsOff, autosync.Categories...)
	preview, err := s.PreviewAutoSync(context.Background(), AutoSyncPreviewOptions{Grant: grant, HostID: autoSyncTestHost})
	if err != nil || preview.Eligible || !containsReason(preview.Reasons, AutoSyncUnpublishedLocal) {
		t.Fatalf("unpublished local change: %+v %v", preview, err)
	}
}

// With nothing left to fast-forward, the preview plans Restore from the
// current checkout and classifies every operation of that fresh plan.
func TestAutoSyncPreviewClassifiesTheFreshRestorePlan(t *testing.T) {
	s, p, _, d, peer := executionSyncFixture(t)
	remoteChange(t, d, peer)
	syncGit(t, s.ProfileDir(), "fetch")
	syncGit(t, s.ProfileDir(), "merge", "--ff-only", "@{u}")
	grant := autoSyncGrant(t, s, autosync.PresetConservative, "packages")
	options := AutoSyncPreviewOptions{Grant: grant, HostID: autoSyncTestHost, Registry: autoSyncFixtureRegistry()}

	// An undescribed operation always needs review.
	preview, err := s.PreviewAutoSync(context.Background(), options)
	if err != nil || preview.Eligible || len(preview.Plan.Decisions) != 1 || preview.Plan.Decisions[0].Reason != autosync.ReasonUndescribed || preview.Counts.NeedsReview != 1 {
		t.Fatalf("undescribed operation: %+v %v", preview, err)
	}
	p.effect = autoSyncFixtureEffect()
	preview, err = s.PreviewAutoSync(context.Background(), options)
	if err != nil || !preview.Eligible || preview.Counts.Automatic != 1 || preview.Plan.Decisions[0].Targets[0] != "official:remote" {
		t.Fatalf("eligible fixture operation: %+v %v", preview, err)
	}
	// The production registry delivers nothing in PR A.
	production := options
	production.Registry = autosync.Registry{}
	if preview, _ := s.PreviewAutoSync(context.Background(), production); preview.Eligible {
		t.Fatal("production registry authorised an operation")
	}
	// A copied grant on another host authorises nothing.
	elsewhere := options
	elsewhere.HostID = strings.Repeat("e", 64)
	if preview, _ := s.PreviewAutoSync(context.Background(), elsewhere); preview.Eligible || preview.Plan.Decisions[0].Reason != autosync.ReasonIdentityMismatch {
		t.Fatalf("other host: %+v", preview.Plan.Decisions)
	}
	// A target exception wins over the category grant.
	denied := options
	denied.Grant.Categories = map[string]autosync.CategoryScope{"packages": {AllTargets: true, DenyTargets: []string{"official:remote"}}}
	denied.Grant.Approved = denied.Grant.AuthorityDigest()
	if preview, _ := s.PreviewAutoSync(context.Background(), denied); preview.Eligible || preview.Plan.Decisions[0].Reason != autosync.ReasonTargetDenied {
		t.Fatalf("denied target: %+v", preview.Plan.Decisions)
	}
	// Paused or off authorises nothing, even with a matching plan.
	paused := options
	paused.Grant.Setting = autosync.SettingPaused
	if preview, _ := s.PreviewAutoSync(context.Background(), paused); preview.Eligible || preview.Plan.Decisions[0].Reason != autosync.ReasonPaused {
		t.Fatalf("paused: %+v", preview.Plan.Decisions)
	}
	for _, stage := range *p.ledger {
		if stage != "restore-plan" {
			t.Fatalf("preview did more than plan: %v", *p.ledger)
		}
	}
}

func containsReason(reasons []AutoSyncPlanReason, want AutoSyncPlanReason) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}
