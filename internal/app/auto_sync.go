package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/autosync"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"github.com/spf13/cobra"
)

// checkTimeout bounds a whole check; the fetch has its own shorter bound.
const checkTimeout = 10 * time.Minute

var errAutoApplyUnavailable = errors.New("applying changes automatically is not available yet in this version. Use omarchy-blueprint auto-sync check to see what has changed, and Blueprint → Sync to apply it")

// autoSyncCommand is the read-only Auto Sync surface of Phase 11 PR B:
// status and history never fetch; check and preview fetch (labelled) under
// the profile lock; nothing applies changes. Turning Auto Sync on and
// changing permissions arrive with the TUI and CLI controls (Phase 11 PR E).
func autoSyncCommand(deps Dependencies, opt *options) *cobra.Command {
	root := &cobra.Command{Use: "auto-sync", Short: "Check the shared profile in the background (read-only for now)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	}}
	root.Long = "Auto Sync can check your shared setup in the background and, in a later version, automatically handle changes you allow on this computer. Anything that needs a decision waits for you.\n\nAutomatic checking is different from automatic application: this version only checks."
	root.AddCommand(
		&cobra.Command{Use: "status", Short: "Show Auto Sync's setting, last check and last completed Sync (never fetches)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			return autoSyncStatus(cmd.Context(), deps, opt)
		}},
		autoSyncHistoryCommand(deps, opt),
		&cobra.Command{Use: "check", Short: "Fetch the shared profile and check what has changed; applies nothing", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			return autoSyncCheck(cmd.Context(), deps, opt)
		}},
		autoSyncPreviewCommand(deps, opt),
		&cobra.Command{Use: "run", Short: "Apply allowed changes automatically (not available yet)", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			return errAutoApplyUnavailable
		}},
		&cobra.Command{Use: "scheduled-check", Short: "Run one scheduled check (used by the systemd user timer)", Hidden: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			return autoSyncScheduledCheck(cmd.Context(), deps, opt)
		}},
	)
	return root
}

// background gives Auto Sync's Git and provider reads a runner that can
// never prompt or borrow the terminal.
func background(deps Dependencies) Dependencies {
	if runner, ok := deps.Runner.(command.SystemRunner); ok {
		runner.Noninteractive = true
		deps.Runner = runner
	}
	return deps
}

type autoSyncStores struct {
	grant  autosync.Store
	status autosync.StatusStore
	leases autosync.LeaseStore
	cursor syncengine.CursorStore
}

func autoSyncStoresFor(deps Dependencies, profileDir string) (autoSyncStores, error) {
	state, err := deps.StateHome()
	if err != nil {
		return autoSyncStores{}, err
	}
	return autoSyncStores{
		grant:  autosync.Store{StateHome: state, ProfileRoot: profileDir},
		status: autosync.StatusStore{StateHome: state, ProfileRoot: profileDir},
		leases: autosync.LeaseStore{StateHome: state, ProfileRoot: profileDir},
		cursor: syncengine.CursorStore{StateHome: state, ProfileRoot: profileDir},
	}, nil
}

func autoSyncScheduler(deps Dependencies) (autosync.Scheduler, error) {
	config, err := deps.ConfigHome()
	if err != nil {
		return autosync.Scheduler{}, err
	}
	return autosync.Scheduler{UnitDir: autoSyncUnitDir(config), Manager: deps.UserManager}, nil
}

// autoSyncCycle builds a cycle for profileDir. Check opens a session with a
// background runner when it runs, so Off and Paused cost nothing.
func autoSyncCycle(deps Dependencies, opt *options, profileDir string, scheduled bool) (autosync.Cycle, error) {
	stores, err := autoSyncStoresFor(deps, profileDir)
	if err != nil {
		return autosync.Cycle{}, err
	}
	cycle := autosync.Cycle{Grants: stores.grant, Leases: stores.leases, Status: stores.status, Now: deps.Now, Scheduled: scheduled}
	if scheduled {
		cycle.Notifier = deps.Notifier
	}
	cycle.Check = func(ctx context.Context) (autosync.Report, error) {
		grant, _, err := stores.grant.Load()
		if err != nil {
			return autosync.Report{}, err
		}
		host, err := deps.HostID()
		if err != nil {
			return autosync.Report{Outcome: autosync.OutcomeNeedsReview, Reasons: []string{"host-identity-unavailable"}}, nil
		}
		local := *opt
		local.profileDir = profileDir
		session, err := openWorkflow(background(deps), &local)
		if err != nil {
			return autosync.Report{}, err
		}
		return session.CheckAutoSync(ctx, workflow.AutoSyncCheckOptions{Grant: grant, HostID: host})
	}
	return cycle, nil
}

func autoSyncCheck(ctx context.Context, deps Dependencies, opt *options) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	cycle, err := autoSyncCycle(deps, opt, opt.profileDir, false)
	if err != nil {
		return err
	}
	result, err := cycle.Run(ctx)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("Auto Sync check — read-only; nothing was applied.\n")
	writeReport(&b, "Result", result.Report)
	return emit(deps.Out, opt.json, "auto-sync check", true, map[string]any{"check": result}, b.String())
}

// autoSyncScheduledCheck is the systemd entry point. It checks the profile
// selected in private state (never --profile), honours the setting and any
// open review, and notifies. No selection or an unusable one does nothing.
func autoSyncScheduledCheck(ctx context.Context, deps Dependencies, opt *options) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	state, err := deps.StateHome()
	if err != nil {
		return err
	}
	selected, ok, err := (autosync.SelectionStore{StateHome: state}).Load()
	if err != nil {
		fmt.Fprintln(deps.Out, "Auto Sync: the selected profile cannot be read safely; nothing was checked.")
		return nil
	}
	if !ok {
		fmt.Fprintln(deps.Out, "Auto Sync: no profile is selected; nothing was checked.")
		return nil
	}
	local := *opt
	local.machine = ""
	cycle, err := autoSyncCycle(deps, &local, selected, true)
	if err != nil {
		return err
	}
	result, err := cycle.Run(ctx)
	if err != nil {
		return err
	}
	// The journal gets typed codes only.
	if !result.Ran {
		fmt.Fprintf(deps.Out, "Auto Sync: %s; nothing was checked.\n", result.Setting)
		return nil
	}
	fmt.Fprintf(deps.Out, "Auto Sync check: %s %s\n", result.Report.Outcome, strings.Join(result.Report.Reasons, ","))
	return nil
}

func autoSyncPreviewCommand(deps Dependencies, opt *options) *cobra.Command {
	noFetch := false
	cmd := &cobra.Command{Use: "preview", Short: "Show what Auto Sync would do with the current plan; applies nothing", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "use the last fetched shared profile without network access")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), checkTimeout)
		defer cancel()
		stores, err := autoSyncStoresFor(deps, opt.profileDir)
		if err != nil {
			return err
		}
		grant, _, err := stores.grant.Load()
		if err != nil {
			return err
		}
		host, err := deps.HostID()
		if err != nil {
			return err
		}
		session, err := openWorkflow(background(deps), opt)
		if err != nil {
			return err
		}
		preview, err := session.PreviewAutoSync(ctx, workflow.AutoSyncPreviewOptions{Fetch: !noFetch, Grant: grant, HostID: host})
		if errors.Is(err, syncengine.ErrProfileBusy) {
			return errors.New("a Sync is using this profile right now; try again when it finishes")
		}
		if err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "auto-sync preview", true, map[string]any{"preview": preview}, formatAutoSyncPreview(grant, preview))
	}
	return cmd
}

func autoSyncHistoryCommand(deps Dependencies, opt *options) *cobra.Command {
	limit := 10
	cmd := &cobra.Command{Use: "history", Short: "Show recent Auto Sync checks (never fetches)", Args: cobra.NoArgs}
	cmd.Flags().IntVar(&limit, "limit", 10, "number of checks to show")
	cmd.RunE = func(*cobra.Command, []string) error {
		stores, err := autoSyncStoresFor(deps, opt.profileDir)
		if err != nil {
			return err
		}
		st, err := stores.status.Load()
		if err != nil {
			return err
		}
		history := st.History
		if limit >= 0 && len(history) > limit {
			history = history[:limit]
		}
		var b strings.Builder
		if len(history) == 0 {
			b.WriteString("No checks yet.\n")
		}
		for _, r := range history {
			fmt.Fprintf(&b, "%s  %s", r.At.Local().Format("2006-01-02 15:04"), outcomeText(r.Outcome))
			if r.Scheduled {
				b.WriteString(" (scheduled)")
			}
			b.WriteByte('\n')
		}
		if st.Recovered {
			b.WriteString("Earlier history could not be read and was set aside.\n")
		}
		return emit(deps.Out, opt.json, "auto-sync history", true, map[string]any{"history": history, "recovered": st.Recovered}, b.String())
	}
	return cmd
}

// autoSyncStatusView is the status JSON: the setting and the last check are
// separate, and a check is never the last completed Sync.
type autoSyncStatusView struct {
	Setting           autosync.Setting     `json:"setting"`
	Preset            autosync.Preset      `json:"preset,omitempty"`
	Suspended         bool                 `json:"suspended"`
	SettingsReadable  bool                 `json:"settings_readable"`
	Profile           string               `json:"profile"`
	Machine           string               `json:"machine,omitempty"`
	Selected          bool                 `json:"scheduled_for_this_profile"`
	Timer             *autosync.TimerState `json:"timer,omitempty"`
	TimerUnavailable  bool                 `json:"timer_unavailable,omitempty"`
	LastCheck         *autosync.Report     `json:"last_check,omitempty"`
	LastCompletedSync *completedSync       `json:"last_completed_sync,omitempty"`
	HistoryRecovered  bool                 `json:"history_recovered,omitempty"`
}

type completedSync struct {
	Revision string    `json:"revision"`
	At       time.Time `json:"at"`
}

func autoSyncStatus(ctx context.Context, deps Dependencies, opt *options) error {
	stores, err := autoSyncStoresFor(deps, opt.profileDir)
	if err != nil {
		return err
	}
	view := autoSyncStatusView{Profile: opt.profileDir, Setting: autosync.SettingOff, SettingsReadable: true}
	if grant, _, err := stores.grant.Load(); err != nil {
		view.SettingsReadable = false
	} else {
		view.Setting, view.Suspended, view.Machine = grant.Setting, grant.Suspended != nil, grant.Identity.Machine
		if grant.Setting != autosync.SettingOff {
			view.Preset = grant.Preset
		}
	}
	if view.Machine == "" {
		if state, err := deps.StateHome(); err == nil {
			if name, err := (machine.BindingStore{StateHome: state}).Load(opt.profileDir); err == nil {
				view.Machine = name
			}
		}
	}
	if state, err := deps.StateHome(); err == nil {
		selected, ok, err := (autosync.SelectionStore{StateHome: state}).Load()
		view.Selected = err == nil && ok && selected == opt.profileDir
	}
	if view.Selected {
		if scheduler, err := autoSyncScheduler(deps); err == nil {
			if timer, err := scheduler.State(ctx); err == nil {
				view.Timer = &timer
			} else {
				view.TimerUnavailable = true
			}
		}
	}
	st, err := stores.status.Load()
	if err != nil {
		return err
	}
	view.LastCheck, view.HistoryRecovered = st.LastCheck, st.Recovered
	if cursor, at, ok, err := stores.cursor.CompletedAt(); err == nil && ok {
		view.LastCompletedSync = &completedSync{Revision: cursor.Revision, At: at.UTC().Truncate(time.Second)}
	}
	return emit(deps.Out, opt.json, "auto-sync status", true, map[string]any{"auto_sync": view}, formatAutoSyncStatus(view))
}

func presetText(p autosync.Preset) string {
	switch p {
	case autosync.PresetNotifyOnly:
		return "Notify only"
	case autosync.PresetConservative:
		return "Conservative"
	case autosync.PresetBalanced:
		return "Balanced"
	case autosync.PresetHandsOff:
		return "Hands-off"
	case autosync.PresetCustom:
		return "Custom"
	}
	return string(p)
}

func outcomeText(o autosync.Outcome) string {
	switch o {
	case autosync.OutcomeUpToDate:
		return "Up to date"
	case autosync.OutcomeApplied:
		return "Applied and verified"
	case autosync.OutcomeNeedsReview:
		return "Needs review"
	case autosync.OutcomeBusy:
		return "Busy (skipped)"
	case autosync.OutcomeOffline:
		return "Offline"
	case autosync.OutcomeSignIn:
		return "Sign-in needed"
	case autosync.OutcomeIncomplete:
		return "Incomplete"
	case autosync.OutcomeError:
		return "Error"
	}
	return "Unknown"
}

// reasonText explains typed reason codes in plain English; codes without
// an explanation are listed only in JSON.
var reasonText = map[string]string{
	autosync.ReasonReviewOpen:         "A Sync review was open in Blueprint, so the check was skipped.",
	autosync.ReasonProfileInUse:       "A Sync was using this profile, so the check was skipped.",
	autosync.ReasonSettingsUnreadable: "Auto Sync settings on this computer cannot be read safely, so Auto Sync stays off until they are set again.",
	autosync.ReasonCheckFailed:        "The check could not finish.",
	workflow.ReasonFetchTimedOut:      "The shared profile took too long to answer.",
	workflow.ReasonRemoteUnreachable:  "The shared profile could not be reached.",
	workflow.ReasonSignInNeeded:       "The shared profile needs you to sign in. Run Sync in Blueprint to sign in.",
	workflow.ReasonApplyUnavailable:   "Applying changes automatically is not available yet.",
	"host-identity-unavailable":       "This computer's identity is unavailable, so Auto Sync cannot be bound to it.",
	// Inspection blockers.
	"machine-required":                          "Choose which machine this computer is in Blueprint first.",
	"git-identity":                              "This profile is not set up for Sync yet: it needs a Git repository on a branch that tracks your shared profile.",
	"bootstrap-required":                        "This computer has not completed its first Sync yet.",
	"dirty-profile":                             "The profile has unsaved Git changes; commit or discard them first.",
	"cursor-identity":                           "The machine, branch or shared profile changed since the last Sync; run Sync again in Blueprint.",
	"cursor-lineage":                            "The shared profile's history changed since the last Sync; run Sync in Blueprint.",
	"cursor-invalid":                            "The record of the last Sync cannot be read; run Sync in Blueprint.",
	string(workflow.AutoSyncBlocker):            "Something needs attention before Sync can continue.",
	string(workflow.AutoSyncManualSyncRequired): "This computer needs a manual Sync first.",
	string(workflow.AutoSyncDiverged):           "This computer and the shared profile both have changes.",
	string(workflow.AutoSyncUnpublishedLocal):   "This computer has profile changes that are not shared yet.",
	string(workflow.AutoSyncLocalChanges):       "Changes on this computer need to be captured first.",
	string(workflow.AutoSyncNeedsDecision):      "Some changes need your decision.",
	string(workflow.AutoSyncDeferred):           "Some changes were put off for later.",
	string(workflow.AutoSyncPolicyChanged):      "Capture or Restore settings changed in the shared profile; they are never adopted automatically.",
	string(workflow.AutoSyncPlanUnavailable):    "New shared changes arrived; automatic handling of them is not available yet.",
	string(workflow.AutoSyncRestoreCannotApply): "Some changes cannot be restored on this computer.",
	string(workflow.AutoSyncRequirement):        "Some changes have requirements to check first.",
	string(workflow.AutoSyncCompatibility):      "Some changes are not supported on this computer's Omarchy version.",
	string(workflow.AutoSyncActivationReview):   "Some changes need you to review how they start.",
	string(workflow.AutoSyncOperationsReview):   "Some changes are outside what Auto Sync may do.",
}

func writeReport(b *strings.Builder, label string, r autosync.Report) {
	fmt.Fprintf(b, "%s: %s — %s", label, r.At.Local().Format("2006-01-02 15:04"), outcomeText(r.Outcome))
	if r.Fetched {
		b.WriteString(" (fetched the shared profile)")
	}
	b.WriteByte('\n')
	if found := r.Counts.Automatic + r.Counts.NeedsReview; found > 0 {
		fmt.Fprintf(b, "  %d change(s) found; nothing was applied automatically.\n", found)
	}
	reasons := append([]string(nil), r.Reasons...)
	sort.Strings(reasons)
	for _, reason := range reasons {
		if text, ok := reasonText[reason]; ok {
			fmt.Fprintf(b, "  %s\n", text)
		}
	}
	if r.Outcome == autosync.OutcomeNeedsReview {
		b.WriteString("  Open Blueprint → Sync to review them.\n")
	}
}

func formatAutoSyncStatus(v autoSyncStatusView) string {
	var b strings.Builder
	switch {
	case !v.SettingsReadable:
		b.WriteString("Auto Sync: Off — its settings on this computer cannot be read safely.\n")
	case v.Setting == autosync.SettingOn:
		fmt.Fprintf(&b, "Auto Sync: On — %s\n", presetText(v.Preset))
	case v.Setting == autosync.SettingPaused:
		fmt.Fprintf(&b, "Auto Sync: Paused (%s)\n", presetText(v.Preset))
	default:
		b.WriteString("Auto Sync: Off\n  Your computer will not check for or apply shared changes in the background.\n")
	}
	if v.Suspended {
		b.WriteString("  Suspended for safety: nothing is applied until you review it in Blueprint → Sync.\n")
	}
	fmt.Fprintf(&b, "Profile: %s\n", safeDisplay(v.Profile))
	if v.Machine != "" {
		fmt.Fprintf(&b, "Machine: %s\n", safeDisplay(v.Machine))
	}
	switch {
	case v.TimerUnavailable:
		b.WriteString("Scheduled checks: the systemd user manager is not running, so no checks are scheduled.\n")
	case v.Timer != nil && v.Timer.Enabled && v.Timer.Next != nil:
		fmt.Fprintf(&b, "Scheduled checks: next around %s, while you are logged in.\n", v.Timer.Next.Local().Format("2006-01-02 15:04"))
	case v.Timer != nil && v.Timer.Enabled:
		b.WriteString("Scheduled checks: on, while you are logged in.\n")
	default:
		b.WriteString("Scheduled checks: none.\n")
	}
	if v.LastCheck == nil {
		b.WriteString("Last check: never\n")
	} else {
		writeReport(&b, "Last check", *v.LastCheck)
	}
	if v.LastCompletedSync == nil {
		b.WriteString("Last completed Sync: none yet\n")
	} else {
		fmt.Fprintf(&b, "Last completed Sync: %s\n", v.LastCompletedSync.At.Local().Format("2006-01-02 15:04"))
	}
	if v.HistoryRecovered {
		b.WriteString("Earlier check history could not be read and was set aside.\n")
	}
	return b.String()
}

func formatAutoSyncPreview(grant autosync.Grant, p workflow.AutoSyncPreview) string {
	var b strings.Builder
	b.WriteString("What Auto Sync would do\n")
	if p.Fetched {
		b.WriteString("  (fetched the shared profile first; nothing was applied)\n")
	}
	switch {
	case p.UpToDate:
		b.WriteString("Nothing to do: this computer matches the shared profile.\n")
	case p.Eligible:
		b.WriteString("These changes could happen automatically. Everything else will wait for your review.\n")
	default:
		fmt.Fprintf(&b, "Waiting for your review: %d change(s). Could happen automatically: %d.\n", p.Counts.NeedsReview, p.Counts.Automatic)
	}
	reasons := make([]string, 0, len(p.Reasons))
	for _, reason := range p.Reasons {
		reasons = append(reasons, string(reason))
	}
	for _, reason := range reasons {
		if text, ok := reasonText[reason]; ok {
			fmt.Fprintf(&b, "  %s\n", text)
		}
	}
	if grant.Setting != autosync.SettingOn {
		b.WriteString("Auto Sync is not on, so nothing would run automatically.\n")
	}
	b.WriteString("\nWhat each level can do on this version\n")
	for _, a := range p.Presets {
		fmt.Fprintf(&b, "  %s: %d available now, %d not available yet\n", presetText(a.Preset), len(a.AvailableNow), len(a.NotAvailableYet))
	}
	return b.String()
}
