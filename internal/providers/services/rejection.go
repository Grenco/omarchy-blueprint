package services

import (
	"errors"
	"fmt"
	"strings"
)

// rejectedUnitSummaries explains, per selected unit, why systemd refused the
// proposed set. systemd validates the set as a whole, so a unit it did not
// name is blocked by the units it did (ADR 0027).
func rejectedUnitSummaries(err error, units []string) map[string]string {
	summaries := make(map[string]string, len(units))
	var rejected *UnitSetRejectedError
	if !errors.As(err, &rejected) {
		for _, unit := range units {
			summaries[unit] = "systemd could not validate the proposed services: " + err.Error()
		}
		return summaries
	}
	byUnit, general := rejected.Reasons(units)
	var named []string
	for _, unit := range units {
		if len(byUnit[unit]) > 0 {
			named = append(named, unit)
		}
	}
	for _, unit := range units {
		switch {
		case len(byUnit[unit]) > 0:
			summaries[unit] = "systemd rejected this service: " + explainSystemdReason(byUnit[unit])
		case len(named) > 0:
			summaries[unit] = "blocked because systemd validates services together and rejected " + strings.Join(named, ", ")
		case len(general) > 0:
			summaries[unit] = "systemd rejected the proposed services: " + explainSystemdReason(general)
		default:
			summaries[unit] = "systemd rejected the proposed services without giving a reason; run `systemd-analyze --user verify` on the unit to see why"
		}
	}
	return summaries
}

func explainSystemdReason(reasons []string) string {
	reason := reasons[0]
	if strings.Contains(reason, "is not executable") {
		reason += " (the program it runs isn't installed on this machine yet)"
	}
	if len(reasons) > 1 {
		reason += fmt.Sprintf(" (+%d more)", len(reasons)-1)
	}
	return reason
}
