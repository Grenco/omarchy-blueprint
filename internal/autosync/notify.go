package autosync

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

// Notice is a best-effort desktop notification. It is never the only way to
// find out about a check: status keeps the same information.
type Notice struct {
	Title, Body string
}

// Notifier shows notices. A failure never changes a check's outcome.
type Notifier interface {
	Notify(context.Context, Notice) error
}

// reminderInterval is how long an unchanged state stays quiet.
const reminderInterval = 24 * time.Hour

const reviewRoute = "Open Blueprint → Sync to review them."

// NoticeFor returns the notice for a check, if it deserves one. Only counts
// and fixed text are used: never target names, paths or Git output.
func NoticeFor(r Report) (Notice, bool) {
	switch r.Outcome {
	case OutcomeNeedsReview:
		found := r.Counts.Automatic + r.Counts.NeedsReview
		if found == 0 {
			return Notice{Title: "Blueprint: Changes need review", Body: "Your shared setup has changes that need a manual Sync. Nothing was applied automatically. " + reviewRoute}, true
		}
		noun := "changes were"
		if found == 1 {
			noun = "change was"
		}
		return Notice{Title: "Blueprint: Changes need review", Body: fmt.Sprintf("%d %s found in your shared setup. Nothing was applied automatically. %s", found, noun, reviewRoute)}, true
	case OutcomeSignIn:
		return Notice{Title: "Blueprint: Sign-in needed", Body: "Auto Sync could not check your shared setup because it needs you to sign in; nothing was changed. Run Sync in Blueprint to sign in."}, true
	case OutcomeOffline:
		return Notice{Title: "Blueprint: Shared setup unreachable", Body: "Auto Sync could not reach your shared setup; nothing was changed. It will try again later."}, true
	case OutcomeError, OutcomeIncomplete:
		return Notice{Title: "Blueprint: Auto Sync check failed", Body: "Auto Sync could not finish checking your shared setup; nothing was changed. Run omarchy-blueprint auto-sync status for details."}, true
	}
	return Notice{}, false
}

func noticeKey(r Report) string {
	c := r.Counts
	return fmt.Sprintf("%s|%s|%d/%d", r.Outcome, strings.Join(r.Reasons, ","), c.Automatic, c.NeedsReview)
}

// shouldNotify coalesces an unchanged state until the reminder interval.
func shouldNotify(last *Notified, r Report, now time.Time) bool {
	if _, ok := NoticeFor(r); !ok {
		return false
	}
	return last == nil || last.Key != noticeKey(r) || now.Sub(last.At) >= reminderInterval
}

// DesktopNotifier uses notify-send. Text is passed as arguments (never
// through a shell) with markup escaped.
type DesktopNotifier struct {
	Runner command.Runner
}

func (d DesktopNotifier) Notify(ctx context.Context, n Notice) error {
	_, err := d.Runner.Run(ctx, "notify-send", "--app-name=Omarchy Blueprint", "--urgency=normal", "--", html.EscapeString(n.Title), html.EscapeString(n.Body))
	if err != nil {
		return fmt.Errorf("desktop notification unavailable")
	}
	return nil
}
