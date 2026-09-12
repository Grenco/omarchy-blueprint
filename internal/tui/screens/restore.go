package screens

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Restore presents a cached normal-versus-forced comparison. Applying always
// asks workflow to create a fresh plan, never the previewed one.
type Restore struct {
	session                 *workflow.Session
	width, height, selected int
	comparison              workflow.RestoreComparison
	mode                    workflow.RestoreMode
	diff                    *components.DiffViewer
	confirm                 bool
	busy                    bool
	err                     error
}

type restoreComparisonMsg struct {
	comparison workflow.RestoreComparison
	err        error
}
type restoreAppliedMsg struct {
	result workflow.RestoreResult
	err    error
}

func NewRestore(session *workflow.Session) *Restore {
	return &Restore{session: session, mode: workflow.RestoreNormal}
}
func (s *Restore) SetSize(width, height int) {
	s.width, s.height = width, height
	if s.diff != nil {
		s.diff.SetSize(width, height)
	}
}
func (s *Restore) Init() tea.Cmd         { return s.compare() }
func (s *Restore) TransientActive() bool { return s.confirm }

func (s *Restore) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case restoreComparisonMsg:
		s.comparison, s.err = msg.comparison, msg.err
		if s.selected >= len(s.comparison.Consequences) {
			s.selected = max(0, len(s.comparison.Consequences)-1)
		}
		return nil
	case restoreAppliedMsg:
		s.err, s.confirm, s.busy = msg.err, false, false
		if msg.err == nil {
			return s.compare()
		}
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if s.confirm {
		switch key.String() {
		case "esc":
			s.confirm = false
		case "enter":
			if !s.busy {
				s.confirm, s.busy = false, true
				return s.apply()
			}
		}
		return nil
	}
	if s.diff != nil {
		if key.String() == "esc" {
			s.diff = nil
			return nil
		}
		return s.diff.Update(msg)
	}
	if s.busy {
		return nil
	}
	switch key.String() {
	case "j", "down":
		if s.selected < len(s.comparison.Consequences)-1 {
			s.selected++
		}
	case "k", "up":
		if s.selected > 0 {
			s.selected--
		}
	case "f":
		if s.mode == workflow.RestoreNormal {
			s.mode = workflow.RestoreForced
		} else {
			s.mode = workflow.RestoreNormal
		}
	case "d":
		if item := s.selectedConsequence(); item.Diff != nil {
			viewer := components.NewDiffViewer(*item.Diff)
			viewer.SetSize(s.width, s.height)
			s.diff = &viewer
		}
	case "enter":
		if len(s.plan().Operations) > 0 {
			s.confirm = true
		}
	}
	return nil
}

func (s *Restore) View() string {
	if s.err != nil {
		return "Restore\n\nUnable to prepare restore: " + s.err.Error()
	}
	if s.confirm {
		return components.Confirm(s.confirmation())
	}
	if s.busy {
		return "Applying restore..."
	}
	if s.diff != nil {
		return "Restore diff\n" + s.diff.View()
	}
	mode := "Normal"
	if s.mode == workflow.RestoreForced {
		mode = "Forced"
	}
	counts := outcomeCounts(s.plan())
	lines := []string{fmt.Sprintf("Restore [%s]", mode), fmt.Sprintf("create:%d modify:%d replace:%d delete:%d commands:%d", counts.create, counts.modify, counts.replace, counts.delete, counts.commands), "f toggle force  d detail/diff  Enter apply"}
	for i, item := range s.comparison.Consequences {
		marker := " "
		if i == s.selected {
			marker = ">"
		}
		outcome := item.Normal
		if s.mode == workflow.RestoreForced {
			outcome = item.Forced
		}
		changed := ""
		if item.Normal != item.Forced {
			changed = " *"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s  %s%s", marker, item.Provider, item.Resource, outcome, changed))
	}
	if len(s.comparison.Consequences) == 0 {
		lines = append(lines, "No restore operations required.")
	}
	if item := s.selectedConsequence(); item.Resource != "" {
		lines = append(lines, "", item.Difference)
		if item.Diff != nil && item.Diff.Kind != inspection.DiffText {
			lines = append(lines, "Detail is metadata-only: "+string(item.Diff.Kind))
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Restore) compare() tea.Cmd {
	return func() tea.Msg {
		comparison, err := s.session.CompareRestore(context.Background(), "")
		return restoreComparisonMsg{comparison, err}
	}
}
func (s *Restore) apply() tea.Cmd {
	mode := s.mode
	return func() tea.Msg {
		result, err := s.session.ApplyRestore(context.Background(), "", mode)
		return restoreAppliedMsg{result, err}
	}
}
func (s *Restore) plan() model.RestorePlan {
	if s.mode == workflow.RestoreForced {
		return s.comparison.Forced
	}
	return s.comparison.Normal
}
func (s *Restore) selectedConsequence() workflow.Consequence {
	if s.selected >= 0 && s.selected < len(s.comparison.Consequences) {
		return s.comparison.Consequences[s.selected]
	}
	return workflow.Consequence{}
}

type restoreCounts struct{ create, modify, replace, delete, commands int }

func outcomeCounts(plan model.RestorePlan) restoreCounts {
	var counts restoreCounts
	for _, op := range plan.Operations {
		switch operationOutcome(op) {
		case workflow.OutcomeCreate:
			counts.create++
		case workflow.OutcomeModify:
			counts.modify++
		case workflow.OutcomeReplace:
			counts.replace++
		case workflow.OutcomeDelete:
			counts.delete++
		}
		if len(op.Command) > 0 {
			counts.commands++
		}
	}
	return counts
}
func operationOutcome(op model.Operation) workflow.Outcome {
	if op.Delete != nil {
		return workflow.OutcomeDelete
	}
	if op.File != nil {
		if op.File.ReplaceExisting {
			return workflow.OutcomeReplace
		}
		if op.File.ExpectedMissing {
			return workflow.OutcomeCreate
		}
		return workflow.OutcomeModify
	}
	if op.Symlink != nil {
		if op.Symlink.ReplaceExisting {
			return workflow.OutcomeReplace
		}
		if op.Symlink.ExpectedMissing {
			return workflow.OutcomeCreate
		}
		return workflow.OutcomeModify
	}
	if op.Directory != nil {
		return workflow.OutcomeCreate
	}
	return workflow.OutcomeModify
}
func (s *Restore) confirmation() string {
	counts := outcomeCounts(s.plan())
	high := 0
	for _, op := range s.plan().Operations {
		if op.Risk == model.RiskHigh {
			high++
		}
	}
	return fmt.Sprintf("Apply %s restore? create:%d modify:%d replace:%d delete:%d commands:%d high-risk:%d", s.mode, counts.create, counts.modify, counts.replace, counts.delete, counts.commands, high)
}
