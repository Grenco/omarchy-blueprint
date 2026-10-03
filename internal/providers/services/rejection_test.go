package services

import (
	"errors"
	"strings"
	"testing"
)

func TestRejectedUnitSummariesNameSystemdsReasonPerUnit(t *testing.T) {
	err := &UnitSetRejectedError{Err: errors.New("exit status 1"), Output: strings.Join([]string{
		"ok.service:5: Unknown key 'BogusKey' in section [Service], ignoring.",
		"gpg-agent.socket: Cannot add dependency job, ignoring: Unit gpg-agent.socket has a bad unit file setting.",
		"waybar-extra.service: Command /usr/bin/waybar-extra is not executable: No such file or directory",
		"",
	}, "\n")}
	summaries := rejectedUnitSummaries(err, []string{"waybar-extra.service", "ok.service"})
	if got := summaries["waybar-extra.service"]; !strings.Contains(got, "Command /usr/bin/waybar-extra is not executable") || !strings.Contains(got, "isn't installed on this machine yet") {
		t.Fatalf("rejected unit summary = %q", got)
	}
	if got := summaries["ok.service"]; !strings.Contains(got, "rejected waybar-extra.service") || strings.Contains(got, "BogusKey") {
		t.Fatalf("unnamed unit summary = %q; want it blocked by the rejected unit, without ignored warnings", got)
	}
}

func TestRejectedUnitSummariesAttributeDropInsAndFallBack(t *testing.T) {
	dropIn := &UnitSetRejectedError{Err: errors.New("exit status 1"), Output: "a.service.d/override.conf:3: Invalid section header '[Servce'\n"}
	if got := rejectedUnitSummaries(dropIn, []string{"a.service"})["a.service"]; !strings.Contains(got, "Invalid section header") {
		t.Fatalf("drop-in message not attributed to its unit: %q", got)
	}
	general := &UnitSetRejectedError{Err: errors.New("exit status 1"), Output: "Failed to load something global\n"}
	if got := rejectedUnitSummaries(general, []string{"a.service"})["a.service"]; !strings.Contains(got, "Failed to load something global") {
		t.Fatalf("general message lost: %q", got)
	}
	silent := &UnitSetRejectedError{Err: errors.New("exit status 1")}
	if got := rejectedUnitSummaries(silent, []string{"a.service"})["a.service"]; !strings.Contains(got, "systemd-analyze --user verify") {
		t.Fatalf("reasonless rejection gives no next step: %q", got)
	}
	plain := errors.New("proposed Services drop-ins require a base definition for validation")
	if got := rejectedUnitSummaries(plain, []string{"a.service"})["a.service"]; !strings.Contains(got, "base definition") {
		t.Fatalf("non-systemd validation error lost: %q", got)
	}
}
