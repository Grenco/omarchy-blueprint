package screens

import (
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// User-facing Sync copy. It is written for someone who knows computers but
// not Git or Blueprint internals; technical identifiers stay in the
// "Technical details" section, CLI output and tests.

const (
	reconcileCheckFailed    = "Blueprint couldn't finish checking for updates (it was stopped, or something went wrong). Nothing was changed. Press r to check again."
	reconcileCancelledRun   = "Sync was not run. Your decisions in this review were cleared and nothing was changed."
	reconcileSetupCancelled = "First-time Sync setup cancelled. Nothing was changed."
	reconcileChoiceRefused  = "That choice isn't available for this setting. The Details panel lists the choices you can make."
	reconcileCompleted      = "Sync completed. Blueprint checked the results and recorded the saved profile version this computer now matches."
	reconcileStaleReview    = "New changes arrived from your shared profile while you were reviewing. Your decisions are kept where they still apply. Press r to refresh."
	reconcileIncomplete     = "Sync did not finish. Some work may still need attention, so Blueprint has not marked this computer as up to date."
)

// Groups describe what running Sync would do, in review order.
const (
	reconcileAttention     = "Needs attention before Sync can run"
	reconcileNeedsDecision = "Needs your decision"
	reconcileDeferred      = "Left for later"
	reconcileDecided       = "Decided"
	reconcileMultiple      = "Multiple steps to review"
	reconcileComputer      = "Will change this computer"
	reconcileSave          = "Will save changes from this computer"
	reconcileProtected     = "Protected by your settings"
	reconcileMatches       = "Already matches"
	reconcileNothing       = "Nothing to change"

	// First-time setup lists what stops a starting choice.
	reconcileSetupProtected = "Settings protected from Capture"
	reconcileSetupUnshared  = "Unshared changes in your saved profile"
)

var reconcileGroups = []string{reconcileAttention, reconcileNeedsDecision, reconcileDeferred, reconcileDecided, reconcileMultiple, reconcileComputer, reconcileSave, reconcileProtected, reconcileMatches, reconcileNothing}

var reconcileGroupAbout = map[string]string{
	reconcileAttention:      "Sync can't continue until these problems are fixed. Select one to see what happened and what to do next.",
	reconcileNeedsDecision:  "Sync found settings where you need to choose what to keep. Select one to see your options. Blueprint will not decide for you.",
	reconcileDeferred:       "You chose to decide these later. Sync can't be completed until you decide.",
	reconcileDecided:        "Settings you made a decision about in this review. You can change a decision until you run Sync.",
	reconcileMultiple:       "Running Sync would both save these settings to your shared profile and change them on this computer. Select one to see each step.",
	reconcileComputer:       "Running Sync would change these settings on this computer to match your saved profile. This may replace or remove settings on this computer.",
	reconcileSave:           "Running Sync would save these changes to your shared profile so your other computers can receive them.",
	reconcileProtected:      "These settings are different, but your Blueprint settings say to leave them alone. Sync will not change them.",
	reconcileMatches:        "These settings already match your saved profile. Nothing will change.",
	reconcileNothing:        "Blueprint checked these settings and has nothing to do for them.",
	reconcileSetupProtected: "These settings differ on this computer, but Blueprint is set not to save them. That is why \"Start with this computer's setup\" isn't available.",
	reconcileSetupUnshared:  "Your saved profile on this computer has changes to these settings that haven't been sent to the shared profile yet, and they differ from it.",
}

type reconcileChoice struct{ key, label, meaning string }

// History conflicts and live differences have different consequences, so
// they keep separate decisions.
var reconcileChoiceInfo = map[string]reconcileChoice{
	string(syncengine.ResolutionKeepLocal):           {"L", "Keep the version saved on this computer", "the version in your saved profile here is kept and sent to the shared profile."},
	string(syncengine.ResolutionAcceptRemote):        {"a", "Use the shared saved version", "the shared version is kept. If this computer differs from it, Sync can apply it here."},
	string(syncengine.ResolutionDefer):               {"d", "Decide later", "nothing happens to this setting now, and Sync can't be completed until you decide."},
	string(syncengine.MachineResolutionKeepMachine):  {"m", "Save this computer's current setting", "this computer's setting is saved into your profile and shared."},
	string(syncengine.MachineResolutionKeepProfile):  {"p", "Apply the saved setting to this computer", "the saved setting is applied to this computer, if your Restore settings allow it."},
	string(syncengine.MachineResolutionDefer) + "/m": {"d", "Decide later", "this computer is left as it is for now, and Sync can't be completed until you decide."},
}

// reconcileBlockerCopy explains a blocker's actual cause and the next step,
// in that order. The raw code and message stay in Technical details.
func reconcileBlockerCopy(code string) (problem, next string) {
	switch code {
	case "dirty-profile":
		return "Your saved profile has changes that have not been committed.", "Finish saving or discard those changes before starting Sync. You can do this in Profile Git."
	case "remote-unavailable":
		return "Blueprint could not check the shared profile for updates.", "Make sure the shared profile's location is reachable (for example your network connection, or the drive it is stored on), then press r to check for updates again."
	case "remote-revision":
		return "Blueprint doesn't have a copy of the shared profile yet.", "Press r to check for updates."
	case "git-identity":
		return "Your saved profile isn't connected to a shared profile.", "Sync needs your profile to be a Git repository on a branch that is linked to a shared copy. Set this up in Profile Git or with Git, then check for updates."
	case "git-status":
		return "Blueprint could not read the state of your saved profile's Git repository.", "Open Profile Git to check it, then press r to check for updates."
	case "machine-required":
		return "Blueprint doesn't know which computer this is.", "Open Machines and select this computer, then come back to Sync."
	case "cursor-identity":
		return "Blueprint's record of the last completed Sync belongs to a different computer, branch or shared profile.", "Nothing has been changed. Sync has to be set up again on this computer. Blueprint does not have a reset command yet; see \"Resetting Sync on a computer\" in the Blueprint guide (docs/guide.md)."
	case "cursor-invalid":
		return "Blueprint's record of the last completed Sync on this computer can't be read.", "Nothing has been changed. Sync has to be set up again on this computer. Blueprint does not have a reset command yet; see \"Resetting Sync on a computer\" in the Blueprint guide (docs/guide.md)."
	case "cursor-lineage":
		return "Blueprint cannot safely match the history on this computer with the shared profile. Nothing has been changed.", "Review the profile's Git history in Profile Git before trying again."
	case "non-managed-history":
		return "Both copies of your profile changed files that Blueprint doesn't manage, so Blueprint can't combine them safely.", "Combine those changes with Git first (Profile Git can show them), then press r to check for updates."
	case "control-conflict", "bootstrap-control-difference":
		return "Your choices about which settings to save and apply were changed differently in two copies of your profile.", "Make them the same in both copies (on each category's Capture and Restore tabs, or Machines), then press r to check for updates."
	case "control-reinspection":
		return "The rules for which config files or resource folders Blueprint looks at differ between versions of your profile, so it can't compare them reliably.", "Make those rules the same on the Config or Resources screens, then press r to check for updates."
	case "live-observation", "live-target-missing", "semantic-identity":
		return "Blueprint could not finish reading this computer's current settings.", "Press r to check again. If it keeps happening, open the category screens to see which settings can't be read."
	case "policy-invalid":
		return "Blueprint could not read the choices about which settings to save and apply in the shared profile.", "Check the Machines screen and the category Capture and Restore tabs, then press r to check for updates."
	case "historical-profile":
		return "Blueprint could not safely read one of the saved versions of your profile, so it can't compare it. Nothing has been changed.", "The technical details below say which part could not be read and how to repair it."
	case "capture-ineligible":
		return "Blueprint's safety checks won't save this setting from this computer.", "Open this setting's category screen to see why."
	case "restore-ineligible":
		return "Blueprint's safety checks won't apply this setting to this computer.", "Open this setting's category screen to see why."
	case "target-review-required":
		return "This setting has to be reviewed in Capture before Sync can include it.", "Open Capture to review it."
	case "restore-evidence-missing":
		return "Blueprint can't confirm it is safe to apply this setting to this computer.", "Open Restore to review this setting."
	case "bootstrap-required":
		return "Sync hasn't been set up on this computer yet.", "Choose how to start."
	}
	return "Sync found a problem it can't work around.", "The technical details below describe it."
}

var reconcileBlockerTitles = map[string]string{
	"dirty-profile":                "Uncommitted profile changes",
	"remote-unavailable":           "Can't reach the shared profile",
	"remote-revision":              "No copy of the shared profile yet",
	"git-identity":                 "Profile isn't linked to a shared copy",
	"git-status":                   "Can't read the profile's Git state",
	"machine-required":             "This computer isn't selected",
	"cursor-identity":              "Sync record belongs elsewhere",
	"cursor-invalid":               "Sync record can't be read",
	"cursor-lineage":               "Profile history doesn't match",
	"non-managed-history":          "Other files changed in both copies",
	"control-conflict":             "Save and apply choices differ",
	"bootstrap-control-difference": "Save and apply choices differ",
	"control-reinspection":         "File-checking rules differ",
	"live-observation":             "Couldn't read this computer",
	"live-target-missing":          "Couldn't read this computer",
	"semantic-identity":            "Couldn't read this computer",
	"policy-invalid":               "Can't read shared save and apply choices",
	"historical-profile":           "Can't read a saved profile version",
	"capture-ineligible":           "Safety check won't save this",
	"restore-ineligible":           "Safety check won't apply this",
	"target-review-required":       "Needs review in Capture",
	"restore-evidence-missing":     "Can't confirm it's safe to apply",
	"bootstrap-required":           "Sync isn't set up yet",
}

func reconcileBlockerTitle(code string) string {
	if title, ok := reconcileBlockerTitles[code]; ok {
		return title
	}
	return "Sync problem"
}

// reconcileSetupCause explains a refusal that applies to both starting
// choices, naming the actual cause.
func reconcileSetupCause(cause workflow.SyncBootstrapUnavailableCause) (problem, next string) {
	switch cause {
	case workflow.SyncBootstrapUncommitted:
		return "Your saved profile has changes that have not been committed.", "Finish saving or discard those changes before starting Sync. You can do this in Profile Git."
	case workflow.SyncBootstrapDiverged:
		return "Blueprint cannot safely match the history on this computer with the shared profile. Nothing has been changed.", "Your saved profile and the shared profile both have changes the other doesn't. Review the profile's Git history in Profile Git and combine them before trying again."
	default:
		return "Sync can't start yet.", "The problems listed below explain why."
	}
}

func reconcilePolicyText(policy syncengine.EffectivePolicy) (capture, restore string) {
	capture, restore = "No", "No"
	if policy.Capture {
		capture = "Yes"
	}
	if policy.Restore {
		restore = "Yes"
	}
	return capture, restore
}

// plural chooses between a singular and a plural sentence.
func reconcileCount(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf(one, n)
	}
	return fmt.Sprintf(many, n)
}
