// Package buildinfo holds the application's build identity. Release and
// package builds set Version with:
//
//	-ldflags "-X github.com/Grenco/omarchy-blueprint/internal/buildinfo.Version=<version>"
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version is the application version without a leading "v"; "dev" for
// builds that did not inject one.
var Version = "dev"

// Current is the running version: the injected one, else the Go module
// version (set by `go install …@vX.Y.Z`), else "dev".
func Current() string {
	return current(Version, debug.ReadBuildInfo)
}

func current(injected string, read func() (*debug.BuildInfo, bool)) string {
	if injected != "" && injected != "dev" {
		return injected
	}
	if info, ok := read(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}
