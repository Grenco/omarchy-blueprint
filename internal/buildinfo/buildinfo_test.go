package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestCurrentPrefersTheInjectedVersionThenTheModuleVersion(t *testing.T) {
	module := func(version string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{Main: debug.Module{Version: version}}, true }
	}
	for _, tc := range []struct {
		injected, module, want string
	}{
		{"0.1.1", "v9.9.9", "0.1.1"},
		{"dev", "v0.1.2", "0.1.2"},
		{"", "v0.1.2", "0.1.2"},
		{"dev", "(devel)", "dev"},
		{"dev", "", "dev"},
	} {
		if got := current(tc.injected, module(tc.module)); got != tc.want {
			t.Fatalf("current(%q, module %q) = %q, want %q", tc.injected, tc.module, got, tc.want)
		}
	}
	if got := current("dev", func() (*debug.BuildInfo, bool) { return nil, false }); got != "dev" {
		t.Fatalf("without build info: %q", got)
	}
}
