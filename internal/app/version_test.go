package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/buildinfo"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/tui"
)

type panickingRunner struct{ t *testing.T }

func (r panickingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.t.Helper()
	panic("--version ran a command: " + name + " " + strings.Join(args, " "))
}

// TestVersionFlagReportsBuildVersionWithoutRuntimeInitialization pins that
// --version is a pure build-identity query: every runtime dependency panics
// if touched, so a profile load, Omarchy detection, TTY probe or TUI start
// fails the test.
func TestVersionFlagReportsBuildVersionWithoutRuntimeInitialization(t *testing.T) {
	previous := buildinfo.Version
	buildinfo.Version = "0.1.0"
	t.Cleanup(func() { buildinfo.Version = previous })

	touched := func(what string) { panic("--version touched " + what) }
	var out, errOut bytes.Buffer
	deps := Dependencies{
		Runner: panickingRunner{t}, In: strings.NewReader(""), Out: &out, Err: &errOut,
		Now:               func() time.Time { touched("the clock"); return time.Time{} },
		StateHome:         func() (string, error) { touched("the state home"); return "", nil },
		ThemeDirs:         func() (string, string, error) { touched("theme dirs"); return "", "", nil },
		PluginDir:         func() (string, error) { touched("the plugin dir"); return "", nil },
		ConfigDirs:        func() (string, string, error) { touched("config dirs"); return "", "", nil },
		BaselineHistory:   func() configprovider.BaselineHistory { touched("baseline history"); return nil },
		ShellPaths:        func() (string, string, error) { touched("shell paths"); return "", "", nil },
		HooksDir:          func() (string, error) { touched("the hooks dir"); return "", nil },
		MiseGlobalConfig:  func() (string, error) { touched("mise config"); return "", nil },
		HomeDir:           func() (string, error) { touched("the home dir"); return "", nil },
		Hostname:          func() (string, error) { touched("the hostname"); return "", nil },
		ResourceLinkRoots: func(string) []resourcesprovider.LinkSearchRoot { touched("resource link roots"); return nil },
		IsTTY:             func() bool { touched("the TTY check"); return false },
		RunTUI: func(context.Context, tui.Options, tui.Dependencies) error {
			touched("the TUI")
			return nil
		},
	}

	code := Execute(context.Background(), []string{"--version"}, deps)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if got := out.String(); got != "omarchy-blueprint version 0.1.0\n" {
		t.Fatalf("stdout = %q", got)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestDevelopmentBuildsReportDev(t *testing.T) {
	if buildinfo.Version != "dev" {
		t.Fatalf("default buildinfo.Version = %q, want dev", buildinfo.Version)
	}
}
