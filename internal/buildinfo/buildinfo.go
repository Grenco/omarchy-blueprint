// Package buildinfo holds the application's build identity. Release and
// package builds set Version with:
//
//	-ldflags "-X github.com/Grenco/omarchy-blueprint/internal/buildinfo.Version=<version>"
package buildinfo

// Version is the application version without a leading "v"; "dev" for
// builds that did not inject one.
var Version = "dev"
