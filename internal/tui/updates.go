package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/updates"
)

// UpdateCheck reports whether a newer Blueprint release exists. It must be
// silent on every failure; the TUI only ever shows its notice.
type UpdateCheck func(context.Context) (updates.Notice, bool)

// updateCheckTimeout bounds one background check.
const updateCheckTimeout = 10 * time.Second

// updateTracker is the TUI's only update state. It is shared by pointer
// with a model rebuilt after creating a profile, so the check runs once per
// launch and a result still in flight lands in the current model. It never
// touches the profile, session or any screen except Overview's notice.
type updateTracker struct {
	check     UpdateCheck
	started   bool
	requestID uint64
	notice    *updates.Notice
}

type updateCheckedMsg struct {
	requestID uint64
	notice    updates.Notice
	ok        bool
}

// startUpdateCheck returns the background check once per launch. Bubble Tea
// runs it off the event loop, so it never delays startup or the first frame.
func (m model) startUpdateCheck() tea.Cmd {
	tracker := m.updates
	if tracker == nil || tracker.check == nil || tracker.started {
		return nil
	}
	tracker.started = true
	tracker.requestID++
	requestID, check, parent := tracker.requestID, tracker.check, m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, updateCheckTimeout)
		defer cancel()
		notice, ok := check(ctx)
		return updateCheckedMsg{requestID: requestID, notice: notice, ok: ok}
	}
}

// handleUpdateChecked accepts only the current check's result and passes
// it to Overview; a stale or empty result changes nothing.
func (m model) handleUpdateChecked(msg updateCheckedMsg) model {
	if m.updates == nil || msg.requestID != m.updates.requestID || !msg.ok {
		return m
	}
	notice := msg.notice
	m.updates.notice = &notice
	m.applyUpdateNotice()
	return m
}

func (m model) applyUpdateNotice() {
	if m.updates == nil || m.updates.notice == nil {
		return
	}
	if overview, ok := m.screens[ScreenOverview].(*overviewScreen); ok {
		overview.SetUpdateNotice(m.updates.notice)
	}
}
