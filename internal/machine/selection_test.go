package machine

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestSuggestName(t *testing.T) {
	for _, test := range []struct {
		hostname string
		existing []profile.Machine
		want     string
	}{
		{hostname: "Framework", want: "framework"},
		{hostname: "work laptop", want: "work-laptop"},
		{hostname: "HOST.local", want: "host.local"},
		{hostname: "localhost", want: "machine"},
		{hostname: "", want: "machine"},
		{hostname: "Framework", existing: []profile.Machine{{Name: "framework"}}, want: "framework-2"},
		{hostname: "Framework", existing: []profile.Machine{{Name: "framework"}, {Name: "framework-2"}}, want: "framework-3"},
	} {
		if got := SuggestName(test.hostname, test.existing); got != test.want {
			t.Errorf("SuggestName(%q, %v) = %q, want %q", test.hostname, test.existing, got, test.want)
		}
	}
}

func TestSelect(t *testing.T) {
	machines := []profile.Machine{{Name: "desktop"}, {Name: "framework"}}
	for _, test := range []struct {
		name                 string
		explicit, bound      string
		wantName, wantSource string
		wantError            string
	}{
		{name: "explicit wins", explicit: "desktop", bound: "framework", wantName: "desktop", wantSource: "explicit"},
		{name: "binding", bound: "framework", wantName: "framework", wantSource: "binding"},
		{name: "default", wantSource: "default"},
		{name: "missing explicit", explicit: "missing", wantError: `machine "missing" does not exist`},
		{name: "missing bound", bound: "old-laptop", wantError: `bound machine "old-laptop" no longer exists; run ` + "`machine use <name>` or `machine clear`"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Select(test.explicit, test.bound, machines)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Select(%q, %q) error = %v, want %q", test.explicit, test.bound, err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != test.wantName || got.Source != test.wantSource {
				t.Errorf("Select(%q, %q) = %+v, want name %q source %q", test.explicit, test.bound, got, test.wantName, test.wantSource)
			}
			if got.Name == "" && got.Machine != nil {
				t.Errorf("default selection machine = %+v, want nil", got.Machine)
			}
			if got.Name != "" && (got.Machine == nil || got.Machine.Name != got.Name) {
				t.Errorf("selection machine = %+v, want %q", got.Machine, got.Name)
			}
		})
	}
}
