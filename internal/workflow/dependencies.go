package workflow

import (
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
)

// Dependencies are the environment-facing services used by shared workflows.
type Dependencies struct {
	Runner            command.Runner
	Now               func() time.Time
	StateHome         func() (string, error)
	ThemeDirs         func() (string, string, error)
	PluginDir         func() (string, error)
	ConfigDirs        func() (string, string, error)
	BaselineHistory   func() configprovider.BaselineHistory
	ShellPaths        func() (string, string, error)
	HooksDir          func() (string, error)
	MiseGlobalConfig  func() (string, error)
	HomeDir           func() (string, error)
	Hostname          func() (string, error)
	ResourceLinkRoots func(string) []resourcesprovider.LinkSearchRoot
}

type Options struct {
	ProfileDir      string
	ExplicitMachine string
}
