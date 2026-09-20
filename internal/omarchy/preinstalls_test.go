package omarchy

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

type preinstallFixtureRunner struct {
	removedAll bool
	installed  map[string]bool
}

func (r preinstallFixtureRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	switch key {
	case "sh -c command -v omarchy-remove-preinstalls":
		return "/usr/bin/omarchy-remove-preinstalls\n", nil
	case "cat /usr/bin/omarchy-remove-preinstalls":
		return `#!/bin/bash
omarchy-webapp-remove-all
omarchy-pkg-drop \
  aether \
  libreoffice-fresh \
  omawrite
`, nil
	case `sh -c [ -f "$HOME/.local/state/omarchy/preinstalls-removed" ]`:
		if r.removedAll {
			return "", nil
		}
		return "", &command.RunError{Name: "sh", ExitCode: 1, Err: errors.New("exit status 1")}
	}
	if name == "pacman" && len(args) == 2 && args[0] == "-Q" {
		if r.installed[args[1]] {
			return args[1] + " 1.0-1\n", nil
		}
		return "", &command.RunError{Name: name, Args: args, ExitCode: 1, Err: errors.New("exit status 1")}
	}
	return "", errors.New("unexpected command: " + key)
}

func TestDetectPreinstallsReadsInstalledCatalogueMarkerAndPresence(t *testing.T) {
	state, err := DetectPreinstalls(context.Background(), preinstallFixtureRunner{
		removedAll: true,
		installed:  map[string]bool{"aether": true, "omawrite": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !state.RemovedAll {
		t.Fatal("RemovedAll = false, want marker presence to be preserved")
	}
	want := map[string]bool{"aether": true, "libreoffice-fresh": false, "omawrite": true}
	if !reflect.DeepEqual(state.Items, want) {
		t.Fatalf("Items = %#v, want %#v", state.Items, want)
	}
}

func TestParsePreinstallCatalogueRejectsMissingPackageBlock(t *testing.T) {
	if _, err := parsePreinstallCatalogue("#!/bin/bash\nomarchy-webapp-remove-all\n"); err == nil {
		t.Fatal("expected missing omarchy-pkg-drop catalogue to fail closed")
	}
}
