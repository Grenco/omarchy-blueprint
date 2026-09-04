package hooks

import (
	"os"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestValidatePathAcceptsRuntimeShapes(t *testing.T) {
	for _, path := range []string{"post-boot", "future-event", "foo.d", "post-update.d/update-rust", ".private-event"} {
		if err := ValidatePath(path); err != nil {
			t.Fatalf("%q rejected: %v", path, err)
		}
	}
}

func TestValidatePathRejectsNonRuntimeShapes(t *testing.T) {
	for _, path := range []string{"", ".", "..", "/absolute", "../outside", "event.d/../outside", `event\name`, "event.d/", ".d/hook", "event.d/.hidden", "event.d/example.sample", "event.d/nested/hook"} {
		if err := ValidatePath(path); err == nil {
			t.Fatalf("%q must be rejected", path)
		}
	}
}

func TestParseAndFormatMode(t *testing.T) {
	for raw, want := range map[string]uint32{"0000": 0, "0644": 0o644, "0755": 0o755, "0777": 0o777} {
		got, err := ParseMode(raw)
		if err != nil || got != want {
			t.Fatalf("%q => %o, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"755", "07550", "0888", "-755", "0x1ff"} {
		if _, err := ParseMode(raw); err == nil {
			t.Fatalf("%q must be rejected", raw)
		}
	}
	if got, err := FormatMode(0o755); err != nil || got != "0755" {
		t.Fatalf("format = %q, %v", got, err)
	}
	if _, err := FormatMode(os.ModeSetuid | 0o755); err == nil {
		t.Fatal("special bits must be rejected")
	}
}

func TestValidateMetadataRejectsBadHashesAndDuplicates(t *testing.T) {
	valid := profile.Hook{Path: "post-boot", Hash: strings.Repeat("a", 64), Mode: "0644"}
	if err := ValidateMetadata([]profile.Hook{valid}); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}
	upper := valid
	upper.Hash = strings.Repeat("A", 64)
	if err := ValidateMetadata([]profile.Hook{upper}); err == nil {
		t.Fatal("uppercase hash must fail")
	}
	if err := ValidateMetadata([]profile.Hook{valid, valid}); err == nil {
		t.Fatal("duplicate path must fail")
	}
}
