package updates

import "testing"

func TestCompareFollowsSemverPrecedence(t *testing.T) {
	// semver.org §11's example order, plus build metadata being ignored.
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0", "10.0.0"}
	for i := range ordered {
		for j := range ordered {
			a, err := ParseVersion(ordered[i])
			if err != nil {
				t.Fatal(err)
			}
			b, err := ParseVersion("v" + ordered[j] + "+build.7")
			if err != nil {
				t.Fatal(err)
			}
			want := cmpUint(uint64(i), uint64(j))
			if got := a.Compare(b); got != want {
				t.Fatalf("Compare(%s, %s) = %d, want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}

func TestParseVersionRejectsMalformedVersions(t *testing.T) {
	for _, bad := range []string{"", "dev", "1", "1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.3-", "1.2.3-01", "1.2.3-a..b", "1.2.3+", "1.2.3-a_b", "v", "-1.2.3", "1.2.x", "18446744073709551616.0.0"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) accepted a malformed version", bad)
		}
	}
	if v, err := ParseVersion(" v0.1.10 "); err != nil || v.String() != "0.1.10" {
		t.Fatalf("ParseVersion(v0.1.10) = %v, %v", v, err)
	}
}
