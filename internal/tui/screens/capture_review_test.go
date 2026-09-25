package screens

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// reviewProvider is a Capture-capable category whose inventory the test
// controls; Capture records the context it was actually run with.
type reviewProvider struct {
	id       string
	targets  *[]workflow.TargetInspection
	captured *[]workflow.CaptureContext
}

func (p reviewProvider) ID() string               { return p.id }
func (reviewProvider) Captured(profile.Data) bool { return true }
func (p reviewProvider) InspectTargets(context.Context, profile.Data) ([]workflow.TargetInspection, error) {
	return *p.targets, nil
}
func (p reviewProvider) Capture(_ context.Context, _ *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	*p.captured = append(*p.captured, capCtx)
	return nil, nil, nil
}
func (reviewProvider) Diff(context.Context, profile.Data) ([]model.Change, error) { return nil, nil }

type reviewFixture struct {
	session  *workflow.Session
	screen   *Capture
	targets  []workflow.TargetInspection
	captured []workflow.CaptureContext
}

func newReviewFixture(t *testing.T, targets ...workflow.TargetInspection) *reviewFixture {
	t.Helper()
	return newReviewFixtureOn(t, "", targets...)
}

// newReviewFixtureOn opens the fixture on machine, when one is named.
func newReviewFixtureOn(t *testing.T, machine string, targets ...workflow.TargetInspection) *reviewFixture {
	t.Helper()
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	if machine != "" {
		data.Machines.Items = []profile.Machine{{Name: machine}}
	}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(workflow.Dependencies{Runner: restoreErrorRunner{}, Now: func() time.Time { return time.Unix(1, 0) }, StateHome: func() (string, error) { return stateHome, nil }}, workflow.Options{ProfileDir: profileDir, ExplicitMachine: machine})
	if err != nil {
		t.Fatal(err)
	}
	f := &reviewFixture{session: session, targets: targets}
	if err := session.SetProviders([]workflow.Provider{reviewProvider{id: "packages", targets: &f.targets, captured: &f.captured}}); err != nil {
		t.Fatal(err)
	}
	f.screen = NewCaptureContext(context.Background(), session)
	f.screen.SetSize(100, 30)
	f.screen.statuses = []workflow.ProviderStatus{{ID: "packages"}}
	return f
}

// run executes cmd and feeds every resulting message back into the screen
// until the chain ends, returning the last message that left the screen
// (a modal request or a completion notice).
func (f *reviewFixture) run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	var last tea.Msg
	for i := 0; cmd != nil && i < 20; i++ {
		msg := cmd()
		switch msg.(type) {
		case components.ModalRequest, CaptureComplete, AuthorityChanged:
			last = msg
			if _, notice := msg.(AuthorityChanged); !notice {
				return last
			}
			cmd = nil
			continue
		}
		cmd = f.screen.Update(msg)
	}
	return last
}

func (f *reviewFixture) press(t *testing.T, key tea.KeyPressMsg) tea.Msg {
	t.Helper()
	return f.run(t, f.screen.Update(key))
}

func (f *reviewFixture) openReview(t *testing.T) {
	t.Helper()
	f.press(t, tea.KeyPressMsg{Code: tea.KeySpace})
	if msg := f.press(t, tea.KeyPressMsg{Code: 'c'}); msg != nil {
		if _, ok := msg.(components.ModalRequest); ok {
			t.Fatalf("c must open the review before any approval prompt, got %#v", msg)
		}
	}
	if !f.screen.Reviewing() {
		t.Fatalf("c did not open the Capture review:\n%s", f.screen.View())
	}
}

func rowContaining(t *testing.T, view, needle string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no row contains %q:\n%s", needle, view)
	return ""
}

func sectionOf(view, needle string) string {
	section := ""
	for _, line := range strings.Split(view, "\n") {
		for _, group := range []string{"Changes", "Preserved by policy", "Blocked"} {
			if strings.HasPrefix(strings.TrimSpace(line), group+" ") || strings.TrimSpace(line) == group {
				section = group
			}
		}
		if section != "" && strings.Contains(line, needle) {
			return section
		}
	}
	return ""
}

func TestCaptureReviewPreviewsFreshProfileCandidates(t *testing.T) {
	f := newReviewFixture(t,
		workflow.TargetInspection{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown},
		workflow.TargetInspection{Key: "official:git", Label: "git", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetPresent},
	)
	f.openReview(t)
	view := f.screen.View()
	for _, want := range []string{"Review Capture", "Profile defaults", "Changes"} {
		if !strings.Contains(view, want) {
			t.Fatalf("review missing %q:\n%s", want, view)
		}
	}
	if row := rowContaining(t, view, "firefox"); !strings.Contains(row, "Add") || !strings.Contains(row, "Packages") {
		t.Fatalf("fresh candidate row = %q, want category and Add", row)
	}
	if len(f.captured) != 0 {
		t.Fatal("opening the review must not capture anything")
	}
}

func TestCaptureReviewIgnoreRecalculatesToPreserve(t *testing.T) {
	f := newReviewFixture(t, workflow.TargetInspection{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown})
	f.openReview(t)
	if section := sectionOf(f.screen.View(), "firefox"); section != "Changes" {
		t.Fatalf("before: firefox in %q, want Changes", section)
	}

	f.press(t, tea.KeyPressMsg{Code: tea.KeySpace})
	view := f.screen.View()
	if section := sectionOf(view, "firefox"); section != "Preserved by policy" {
		t.Fatalf("after Ignore: firefox in %q, want Preserved by policy:\n%s", section, view)
	}
	if row := rowContaining(t, view, "firefox"); !strings.Contains(row, "Preserve") {
		t.Fatalf("after Ignore row = %q, want Preserve", row)
	}
	effective, err := f.session.EffectivePolicy(context.Background(), workflow.PolicyScope{}, "packages", workflow.TargetInspection{Key: "official:firefox", CaptureEligible: true})
	if err != nil {
		t.Fatal(err)
	}
	if effective.Capture.Enabled || !effective.Capture.Explicit {
		t.Fatalf("Ignore did not persist an explicit Capture Disabled rule: %#v", effective.Capture)
	}

	f.press(t, tea.KeyPressMsg{Code: 'x'})
	if section := sectionOf(f.screen.View(), "firefox"); section != "Changes" {
		t.Fatalf("after reset: firefox in %q, want Changes again", section)
	}
}

func TestCaptureReviewBlockedSafetyCannotBeOverridden(t *testing.T) {
	f := newReviewFixture(t, workflow.TargetInspection{Key: "official:secret", Label: "secret", CaptureEligible: false, SafetyReason: "contains credentials", Current: workflow.TargetPresent, Desired: workflow.TargetUnknown})
	f.openReview(t)
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeySpace}, {Code: 'x'}} {
		f.press(t, key)
		view := f.screen.View()
		if section := sectionOf(view, "secret"); section != "Blocked" {
			t.Fatalf("%s moved a blocked target to %q:\n%s", key, section, view)
		}
	}
	if len(f.session.Profile().Policy.Capture) != 0 {
		t.Fatalf("a blocked target must not receive a policy override: %#v", f.session.Profile().Policy.Capture)
	}
	if detail := f.screen.DetailView(); !strings.Contains(detail, "contains credentials") {
		t.Fatalf("Details must explain the safety block: %q", detail)
	}
}

// approve presses Enter for the summary and Enter again to approve it.
func (f *reviewFixture) approve(t *testing.T) tea.Msg {
	t.Helper()
	request, ok := f.press(t, tea.KeyPressMsg{Code: tea.KeyEnter}).(components.ModalRequest)
	if !ok || !strings.Contains(request.Content, "nothing is written") {
		t.Fatalf("Enter should ask for approval and say what happens if the review changed: %#v", request)
	}
	return f.press(t, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// A change between review and approval must not be captured on the strength
// of the old approval: nothing is written, the review is recalculated, and
// the user approves the new one.
func TestCaptureApprovalRefusesAReviewThatChangedBeforeApproval(t *testing.T) {
	firefox := workflow.TargetInspection{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown}
	for _, test := range []struct {
		name    string
		change  func(t *testing.T, f *reviewFixture)
		row     string
		section string
	}{
		{
			name: "policy changed elsewhere",
			change: func(t *testing.T, f *reviewFixture) {
				if err := f.session.SetPolicy(workflow.PolicyScope{}, policy.AxisCapture, "packages", "official:firefox", policy.SettingDisabled); err != nil {
					t.Fatal(err)
				}
			},
			row: "firefox", section: "Preserved by policy",
		},
		{
			name: "system changed: a new package was installed",
			change: func(t *testing.T, f *reviewFixture) {
				f.targets = append(f.targets, workflow.TargetInspection{Key: "official:vlc", Label: "vlc", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown})
			},
			row: "vlc", section: "Changes",
		},
		{
			name: "system changed: the reviewed package was removed",
			change: func(t *testing.T, f *reviewFixture) {
				f.targets = []workflow.TargetInspection{{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: workflow.TargetAbsent, Desired: workflow.TargetUnknown}}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newReviewFixture(t, firefox)
			f.openReview(t)
			test.change(t, f)
			before := f.session.Profile()

			if msg := f.approve(t); msg != nil {
				if _, done := msg.(CaptureComplete); done {
					t.Fatal("a changed review must not complete Capture")
				}
			}
			if len(f.captured) != 0 {
				t.Fatalf("Capture ran %d times against a changed review, want zero mutation", len(f.captured))
			}
			if after := f.session.Profile(); !reflect.DeepEqual(after, before) {
				t.Fatal("a changed review must leave the profile untouched")
			}
			if !f.screen.Reviewing() {
				t.Fatal("a changed review must stay open for re-approval")
			}
			view := f.screen.View()
			if !strings.Contains(view, "Nothing was captured") || !strings.Contains(view, "changed") {
				t.Fatalf("the review must explain that it changed:\n%s", view)
			}
			if test.row != "" {
				if section := sectionOf(view, test.row); section != test.section {
					t.Fatalf("recalculated review put %s in %q, want %q:\n%s", test.row, section, test.section, view)
				}
			}

			// Approving the recalculated review now captures it.
			if _, ok := f.approve(t).(CaptureComplete); !ok {
				t.Fatal("approving the recalculated review did not complete Capture")
			}
			if len(f.captured) != 1 {
				t.Fatalf("Capture ran %d times, want 1", len(f.captured))
			}
		})
	}
}

func TestCaptureApprovalCapturesAnUnchangedReview(t *testing.T) {
	f := newReviewFixture(t, workflow.TargetInspection{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown})
	f.openReview(t)
	if _, ok := f.approve(t).(CaptureComplete); !ok {
		t.Fatal("approving an unchanged review did not complete Capture")
	}
	decision, ok := f.captured[0].Lookup("official:firefox")
	if len(f.captured) != 1 || !ok || !decision.Capture {
		t.Fatalf("Capture did not run with the approved Include decision: %#v", f.captured)
	}
	if f.screen.Reviewing() {
		t.Fatal("a completed Capture should leave the review")
	}
}

// The review shows outcomes for the machine running Capture, but space and
// x edit policy at the review's scope, so the CAPTURE setting shown and
// toggled must be the one at that scope.
func TestCaptureReviewEditsThePolicyAtItsScopeNotTheActiveMachine(t *testing.T) {
	f := newReviewFixtureOn(t, "desktop", workflow.TargetInspection{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown})
	profileScope, desktop := workflow.PolicyScope{}, workflow.PolicyScope{Machine: "desktop"}
	if err := f.session.SetPolicy(profileScope, policy.AxisCapture, "packages", "official:firefox", policy.SettingEnabled); err != nil {
		t.Fatal(err)
	}
	if err := f.session.SetPolicy(desktop, policy.AxisCapture, "packages", "official:firefox", policy.SettingDisabled); err != nil {
		t.Fatal(err)
	}
	f.openReview(t)
	if row := rowContaining(t, f.screen.View(), "firefox"); !strings.Contains(row, "Preserve") || strings.Contains(row, "↳") {
		t.Fatalf("at desktop scope the row should show desktop's explicit Preserve: %q", row)
	}

	f.press(t, tea.KeyPressMsg{Code: 'p'})
	view := f.screen.View()
	if !strings.Contains(view, "Profile defaults") || !strings.Contains(view, "outcomes are for this machine (desktop)") {
		t.Fatalf("the profile scope must say outcomes stay those of this machine:\n%s", view)
	}
	row := rowContaining(t, view, "firefox")
	if sectionOf(view, "firefox") != "Preserved by policy" || !strings.Contains(row, "Preserve") {
		t.Fatalf("the outcome must stay desktop's Preserve: %q", row)
	}
	if !strings.Contains(row, "Include") || strings.Contains(row, "↳") {
		t.Fatalf("CAPTURE must show the profile's own explicit Include rule: %q", row)
	}
	detail := f.screen.DetailView()
	for _, want := range []string{"Capture policy at Profile defaults: Include (explicit, Profile)", "Applied on this machine (desktop): Preserve (explicit, This machine)"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("details missing %q:\n%s", want, detail)
		}
	}

	// space flips the profile's Include, not desktop's Preserve.
	f.press(t, tea.KeyPressMsg{Code: tea.KeySpace})
	atProfile, err := f.session.EffectivePolicy(context.Background(), profileScope, "packages", workflow.TargetInspection{Key: "official:firefox", CaptureEligible: true})
	if err != nil {
		t.Fatal(err)
	}
	if atProfile.Capture.Enabled || !atProfile.Capture.Explicit {
		t.Fatalf("space should have switched the profile rule to Preserve: %#v", atProfile.Capture)
	}
	atDesktop, err := f.session.EffectivePolicy(context.Background(), desktop, "packages", workflow.TargetInspection{Key: "official:firefox", CaptureEligible: true})
	if err != nil {
		t.Fatal(err)
	}
	if atDesktop.Capture.Enabled || atDesktop.Capture.Source.Kind != policy.SourceMachineTarget {
		t.Fatalf("desktop's own rule must be untouched: %#v", atDesktop.Capture)
	}
	if row := rowContaining(t, f.screen.View(), "firefox"); !strings.Contains(row, "Preserve") {
		t.Fatalf("after space the row should show the profile's new Preserve: %q", row)
	}
}

func TestCaptureReviewEscReturnsToCategories(t *testing.T) {
	f := newReviewFixture(t, workflow.TargetInspection{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown})
	f.openReview(t)
	f.press(t, tea.KeyPressMsg{Code: tea.KeyEsc})
	if f.screen.Reviewing() || !strings.Contains(f.screen.View(), "CATEGORY") {
		t.Fatalf("Esc should return to category selection:\n%s", f.screen.View())
	}
	if len(f.captured) != 0 {
		t.Fatal("leaving the review must not capture")
	}
}

func TestCaptureReviewSummarisesUnchangedTargetsInsteadOfListingThem(t *testing.T) {
	f := newReviewFixture(t,
		workflow.TargetInspection{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown},
		workflow.TargetInspection{Key: "official:gone", Label: "gone", CaptureEligible: true, Current: workflow.TargetAbsent, Desired: workflow.TargetUnknown},
	)
	f.openReview(t)
	view := f.screen.View()
	if !strings.Contains(view, "1 no action needed") {
		t.Fatalf("review should count unchanged targets:\n%s", view)
	}
	if strings.Contains(view, "gone") {
		t.Fatalf("unchanged targets should not be listed as rows:\n%s", view)
	}
}

func TestCaptureReviewFitsItsWorkspaceWhenTheSummaryWraps(t *testing.T) {
	targets := []workflow.TargetInspection{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		targets = append(targets, workflow.TargetInspection{Key: "official:" + name, Label: name, CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown})
	}
	f := newReviewFixture(t, targets...)
	f.screen.SetSize(40, 10)
	f.openReview(t)
	lines := strings.Split(f.screen.View(), "\n")
	if len(lines) > 10 {
		t.Fatalf("review emitted %d lines for a 10-line workspace:\n%s", len(lines), f.screen.View())
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("review line is %d columns in a 40-column workspace: %q", width, line)
		}
	}
}
