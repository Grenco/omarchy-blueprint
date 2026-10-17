package profile

import "testing"

// A profile can never carry Blueprint's own Auto Sync units: Restore would
// otherwise install a scheduler from downloaded content.
func TestServicesRejectBlueprintAutoSyncUnits(t *testing.T) {
	for _, name := range []string{"omarchy-blueprint-auto-sync.service", "omarchy-blueprint-auto-sync.timer"} {
		if !IsReservedServiceUnit(name) {
			t.Fatalf("%s not reserved", name)
		}
		if err := ValidateServiceUnitName(name); err == nil {
			t.Fatalf("%s accepted in a profile", name)
		}
	}
	if IsReservedServiceUnit("omarchy-blueprint-auto-sync-x.service") || ValidateServiceUnitName("syncthing.service") != nil {
		t.Fatal("reserved too much")
	}
}
