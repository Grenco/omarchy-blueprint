package screens

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"github.com/charmbracelet/x/ansi"
)

func crowdedCaptureReview(targets int) *captureReview {
	section := workflow.CaptureReviewSection{Group: workflow.CaptureReviewChanges}
	for i := range targets {
		section.Targets = append(section.Targets, workflow.CaptureTarget{Category: "packages", Inspection: workflow.TargetInspection{Key: fmt.Sprintf("official:pkg-%03d", i), Label: fmt.Sprintf("pkg-%03d", i)}, Outcome: workflow.CaptureOutcomeUpdate})
	}
	return &captureReview{
		scope:    workflow.PolicyScope{Machine: "workstation-" + strings.Repeat("with-a-very-long-name-", 5)},
		err:      errors.New("inspect packages: " + strings.Repeat("pacman reported a long failure ", 6)),
		sections: []workflow.CaptureReviewSection{section},
		cursor:   targets - 1,
	}
}

func TestCaptureReviewFitsWithALongHeaderAndKeepsWhatMatters(t *testing.T) {
	for _, size := range workspaceSizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			screen := &Capture{width: size.width, height: size.height, review: crowdedCaptureReview(60)}
			view := screen.View()
			assertFits(t, "Capture review", view, size.width, size.height)
			plain := ansi.Strip(view)
			for _, want := range []string{"pkg-059", "Policy scope", "Unable to review Capture"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("the review lost %q:\n%s", want, plain)
				}
			}
		})
	}
}

func TestCaptureReviewLoadingAndEmptyStatesFit(t *testing.T) {
	for _, size := range workspaceSizes {
		loading := crowdedCaptureReview(1)
		loading.loading, loading.err, loading.categories = true, nil, []string{"packages", "themes", "plugins", "config", "defaults", "shell", "hooks", "services", "resources"}
		assertFits(t, "Capture review loading", (&Capture{width: size.width, height: size.height, review: loading}).View(), size.width, size.height)
		empty := crowdedCaptureReview(1)
		empty.sections = nil
		assertFits(t, "Capture review empty", (&Capture{width: size.width, height: size.height, review: empty}).View(), size.width, size.height)
	}
}
