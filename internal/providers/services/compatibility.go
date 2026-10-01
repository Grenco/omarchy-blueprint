package services

import (
	"github.com/Grenco/omarchy-blueprint/internal/compatibility"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"strings"
)

type serviceAssessment struct {
	evidence []model.CompatibilityEvidence
	findings []model.CompatibilityFinding
}

func (a *serviceAssessment) finding(target, code, summary string, blocked bool) {
	state, authority := model.CompatibilityUnknown, model.CompatibilityReduced
	if blocked {
		state, authority = model.CompatibilityIncompatible, model.CompatibilityBlocked
		switch code {
		case "services.base.unestablished", "services.link.unavailable":
			// Inspection cannot establish safe source content; blocking does
			// not turn absent evidence into an affirmative incompatibility.
			state = model.CompatibilityUnknown
		}
	}
	for n := range a.findings {
		if a.findings[n].Target == target && a.findings[n].Code == code {
			if !strings.Contains(a.findings[n].Summary, summary) {
				a.findings[n].Summary += "; " + summary
			}
			return
		}
	}
	a.findings = append(a.findings, model.CompatibilityFinding{Target: target, Code: code, Summary: summary, State: state, Authority: authority})
}
func (a serviceAssessment) category(applies bool) (model.CompatibilityCategory, error) {
	return compatibility.BuildCategory("services", applies, a.evidence, a.findings)
}
