package omarchy

import "testing"

func TestRequireMajor(t *testing.T) {
	for _, version := range []string{"4.0.0-1", "v4.2.1", "5.0.0"} {
		if err := requireMajor(version, 4); err != nil {
			t.Errorf("%s: %v", version, err)
		}
	}
	for _, version := range []string{"3.9.0", "unknown", ""} {
		if err := requireMajor(version, 4); err == nil {
			t.Errorf("expected %s to fail", version)
		}
	}
}
