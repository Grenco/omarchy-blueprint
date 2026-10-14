package screens

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestReconcileAndExecutionConfirmationFitWorkspace(t *testing.T) {
	for _, size := range [][2]int{{68, 9}, {78, 15}, {64, 31}} {
		s := NewReconcileContext(context.Background(), &reconcileFixture{plan: conflictSyncPlan(t)})
		s.SetSize(size[0], size[1])
		s.Update(s.Init()())
		for _, confirm := range []bool{false, true} {
			if confirm {
				s.Update(tea.KeyPressMsg{Code: 'L'})
				s.Update(tea.KeyPressMsg{Code: 's'})
			}
			lines := strings.Split(s.View(), "\n")
			if len(lines) > size[1] {
				t.Fatalf("%v confirmation=%t overflows height", size, confirm)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("%v confirmation=%t overflows width", size, confirm)
				}
			}
		}
	}
}

func TestReconcileRepairWarningSurvivesRefreshingSmallWorkspace(t *testing.T) {
	s := NewReconcileContext(context.Background(), &reconcileFixture{plan: conflictSyncPlan(t)})
	s.SetSize(68, 9)
	s.Update(SyncExecutionComplete{Err: profilegit.ErrReconciliationPostCommitVerificationFailed})
	if !strings.Contains(s.View(), "Do not run Sync again") || !strings.Contains(s.DetailView(), "HEAD, index and worktree") {
		t.Fatal("fresh inspection hid repository repair guidance")
	}
}
