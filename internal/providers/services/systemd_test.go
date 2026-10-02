package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type fixtureRunner struct {
	files, loaded string
	show          map[string]string
	cat           map[string]string
	calls         []string
}

type recordingRunner struct{ calls []string }

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return "", nil
}

func TestVerifyUnitSetStaysReadOnlyAndWithinIsolatedTree(t *testing.T) {
	r := &recordingRunner{}
	s := Systemctl{Runner: r}
	root := t.TempDir()
	if err := s.VerifyUnitSet(context.Background(), ProposedUnitSet{Root: root, Files: []string{filepath.Join(t.TempDir(), "external.service")}}); err == nil || len(r.calls) != 0 {
		t.Fatalf("untrusted validation file reached systemd: calls=%v err=%v", r.calls, err)
	}
	file := filepath.Join(root, "backup.service")
	if err := os.WriteFile(file, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyUnitSet(context.Background(), ProposedUnitSet{Root: root, Files: []string{file}}); err != nil || !reflect.DeepEqual(r.calls, []string{"env SYSTEMD_UNIT_PATH=" + root + ": XDG_RUNTIME_DIR=" + root + " systemd-analyze --user --generators=no --man=no verify " + file}) {
		t.Fatalf("validation was not an isolated read-only command: calls=%v err=%v", r.calls, err)
	}
	escape := filepath.Join(root, "linked.service")
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.service"), escape); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyUnitSet(context.Background(), ProposedUnitSet{Root: root, Files: []string{escape}}); err == nil || len(r.calls) != 1 {
		t.Fatalf("linked proposed definition escaped validation tree: calls=%v err=%v", r.calls, err)
	}
}

func TestSystemdMutatorsRequireExplicitUserUnitCommand(t *testing.T) {
	r := &recordingRunner{}
	s := Systemctl{Runner: r}
	if err := s.Start(context.Background(), "--system"); err == nil || len(r.calls) != 0 {
		t.Fatalf("unsafe start was executed: %v err=%v", r.calls, err)
	}
	if err := s.Enable(context.Background(), "backup.timer"); err != nil {
		t.Fatal(err)
	}
	if err := s.DaemonReload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"systemctl --user enable -- backup.timer", "systemctl --user daemon-reload"}; !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("unexpected systemd authority: got=%v want=%v", r.calls, want)
	}
}

func (f *fixtureRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	for _, forbidden := range []string{"daemon-reload", " enable ", " disable ", " mask ", " unmask ", " start ", " stop ", " restart ", "sudo", "loginctl", "--system"} {
		if strings.Contains(" "+call+" ", forbidden) {
			return "", fmt.Errorf("mutating inspection command: %s", call)
		}
	}
	if name != "systemctl" || len(args) < 2 || args[0] != "--user" {
		return "", fmt.Errorf("unexpected command: %s", call)
	}
	switch args[1] {
	case "list-unit-files":
		return f.files, nil
	case "list-units":
		return f.loaded, nil
	case "show":
		var records []string
		separator := -1
		for i, arg := range args {
			if arg == "--" {
				separator = i
				break
			}
		}
		if separator < 0 {
			return "", fmt.Errorf("missing show separator")
		}
		for _, unit := range args[separator+1:] {
			if strings.Contains(unit, "@.") {
				return "", fmt.Errorf("naked template queried")
			}
			value, ok := f.show[unit]
			if !ok {
				return "", fmt.Errorf("unknown show operand")
			}
			records = append(records, strings.TrimSpace(value))
		}
		return strings.Join(records, "\n\n") + "\n", nil
	case "cat":
		unit := args[len(args)-1]
		if value, ok := f.cat[unit]; ok {
			return value, nil
		}
	}
	return "", fmt.Errorf("unknown read-only inspection: %s", call)
}

func TestInspectNormalizesFragmentDropInsEnablementAndActiveState(t *testing.T) {
	f := &fixtureRunner{files: "backup.service enabled enabled\n", show: map[string]string{"backup.service": "Id=backup.service\nLoadState=loaded\nUnitFileState=enabled\nActiveState=active\nFragmentPath=/home/test/.config/systemd/user/backup.service\nDropInPaths=/home/test/.config/systemd/user/backup.service.d/10-env.conf /home/test/.config/systemd/user/backup.service.d/20-extra.conf\nRequires=network.target\nWants=backup.timer network.target\n"}}
	got, err := (Systemctl{Runner: f}).InspectUserUnits(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("InspectUserUnits = %+v err=%v", got, err)
	}
	unit := got[0]
	if unit.Name != "backup.service" || unit.Kind != "service" || unit.FragmentPath != "/home/test/.config/systemd/user/backup.service" || unit.StartIntent != profile.ServiceStartEnabled || !unit.ObservedActive || !unit.Persistent || !unit.TopologyKnown || len(unit.DropInPaths) != 2 || !reflect.DeepEqual(unit.RelatedUnits, []string{"backup.timer", "network.target"}) {
		t.Fatalf("normalized user unit = %+v", unit)
	}
	if len(f.calls) != 3 {
		t.Fatalf("inspection did not use the bounded read-only inventory: %v", f.calls)
	}
}

func TestInspectSeparatesRuntimeGeneratedFromPersistentSources(t *testing.T) {
	f := &fixtureRunner{
		files:  "backup.service disabled enabled\ngenerated.service generated -\n",
		loaded: "transient.service loaded active running Transient unit\n",
		show: map[string]string{
			"backup.service":    "Id=backup.service\nUnitFileState=disabled\nFragmentPath=/home/test/.config/systemd/user/backup.service\n",
			"generated.service": "Id=generated.service\nUnitFileState=generated\nFragmentPath=/run/user/1000/systemd/generator/generated.service\n",
			"transient.service": "Id=transient.service\nUnitFileState=transient\nTransient=yes\nFragmentPath=/run/user/1000/systemd/transient/transient.service\n",
		},
	}
	got, err := (Systemctl{Runner: f}).InspectUserUnits(context.Background())
	if err != nil || len(got) != 3 {
		t.Fatalf("InspectUserUnits = %+v err=%v", got, err)
	}
	if !got[0].Persistent || got[1].Persistent || !got[1].Generated || got[2].Persistent || !got[2].Transient || !got[2].Runtime {
		t.Fatalf("runtime/generated units were promoted into portable state: %+v", got)
	}
}

func TestInspectTemplateAndInstances(t *testing.T) {
	f := &fixtureRunner{files: "backup@.service indirect -\nbackup@photos.service enabled -\n", cat: map[string]string{
		"backup@.service": "# /home/test/.config/systemd/user/backup@.service\n[Service]\n# /home/test/private/backup@.service.d/not-really-a-dropin.conf\nExecStart=/usr/bin/true\n\n# /home/test/.config/systemd/user/service.d/10-effective.conf\n[Service]\nEnvironment=MODE=photos\n",
	}, show: map[string]string{
		"backup@photos.service": "Id=backup@photos.service\nUnitFileState=enabled\nFragmentPath=/home/test/.config/systemd/user/backup@.service\nActiveState=inactive\n",
	}}
	got, err := (Systemctl{Runner: f}).InspectUserUnits(context.Background())
	if err != nil || len(got) != 2 || !got[0].Template || got[0].TopologyKnown || got[0].RawUnitFileState != "indirect" || got[0].StartIntent != profile.ServiceStartIndirect || got[0].FragmentPath != "" || len(got[0].DropInPaths) != 0 || got[1].InstanceOf != "backup@.service" || !got[1].TopologyKnown || got[1].ObservedActive || got[1].FragmentPath != "/home/test/.config/systemd/user/backup@.service" {
		t.Fatalf("template and instance = %+v err=%v", got, err)
	}
	for _, call := range f.calls {
		if strings.HasSuffix(call, "backup@.service") {
			t.Fatalf("unresolved naked template was passed to show/cat: %q", call)
		}
	}
	if len(f.calls) != 3 {
		t.Fatalf("unresolved template introduced extra probes: %v", f.calls)
	}
}

func TestInspectMaskedTemplateUsesCatalogWithoutInventingDefinition(t *testing.T) {
	f := &fixtureRunner{files: "vendor@.service masked -\n"}
	got, err := (Systemctl{Runner: f}).InspectUserUnits(context.Background())
	if err != nil || len(got) != 1 || !got[0].Template || got[0].TopologyKnown || !got[0].Persistent || got[0].StartIntent != profile.ServiceStartMasked || got[0].FragmentPath != "" {
		t.Fatalf("masked template evidence = %+v err=%v", got, err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("masked template was asked to expose a hidden definition: %v", f.calls)
	}
}

func TestInspectTemplateWithoutAuthoritativeSourceRemainsUnresolved(t *testing.T) {
	f := &fixtureRunner{files: "backup@.service static -\n", cat: map[string]string{
		"backup@.service": "[Service]\nExecStart=/usr/bin/true\n",
	}}
	if got, err := (Systemctl{Runner: f}).InspectUserUnits(context.Background()); err != nil || len(got) != 1 || !got[0].Template || got[0].TopologyKnown || got[0].FragmentPath != "" || len(got[0].DropInPaths) != 0 || len(f.calls) != 2 {
		t.Fatalf("template with unknown source aborted inventory or guessed paths: %+v calls=%v err=%v", got, f.calls, err)
	}
}

func TestInspectTemplateAliasesAndBroadDropInsRemainUnknown(t *testing.T) {
	f := &fixtureRunner{files: "alias@.service alias -\nbackup@.service static -\n", cat: map[string]string{
		"alias@.service":  "# /home/test/.config/systemd/user/alias@.service\n[Service]\n# /home/test/private/alias@.service.d/fake.conf\n",
		"backup@.service": "# /home/test/.config/systemd/user/backup@.service\n[Service]\n# /home/test/.config/systemd/user/service.d/10-effective.conf\n",
	}}
	got, err := (Systemctl{Runner: f}).InspectUserUnits(context.Background())
	if err != nil || len(got) != 2 || got[0].RawUnitFileState != "alias" || got[1].RawUnitFileState != "static" {
		t.Fatalf("alias/template inventory = %+v err=%v", got, err)
	}
	for _, unit := range got {
		if unit.TopologyKnown || unit.FragmentPath != "" || len(unit.DropInPaths) != 0 {
			t.Fatalf("authored comments or incomplete drop-in rules invented topology: %+v", unit)
		}
	}
	if len(f.calls) != 2 {
		t.Fatalf("template alias/hierarchy caused extra probes: %v", f.calls)
	}
}

func TestInspectShowWithoutSourceKeepsTopologyUnknown(t *testing.T) {
	f := &fixtureRunner{files: "alias.service alias -\n", show: map[string]string{
		"alias.service": "Id=alias.service\nUnitFileState=alias\nFragmentPath=\nDropInPaths=\n",
	}}
	got, err := (Systemctl{Runner: f}).InspectUserUnits(context.Background())
	if err != nil || len(got) != 1 || got[0].TopologyKnown || got[0].FragmentPath != "" || len(got[0].DropInPaths) != 0 {
		t.Fatalf("ID-only show was treated as resolved topology: %+v err=%v", got, err)
	}
}

func TestInspectDoesNotTreatRawSystemctlStateAsProfileEnum(t *testing.T) {
	f := &fixtureRunner{files: "runtime.service enabled-runtime -\nmask.service masked-runtime -\nlinked.service linked -\n", show: map[string]string{
		"runtime.service": "Id=runtime.service\nUnitFileState=enabled-runtime\nFragmentPath=/home/test/.config/systemd/user/runtime.service\n",
		"mask.service":    "Id=mask.service\nUnitFileState=masked-runtime\nFragmentPath=/dev/null\n",
		"linked.service":  "Id=linked.service\nUnitFileState=linked\nFragmentPath=/home/test/Projects/app/linked.service\n",
	}}
	got, err := (Systemctl{Runner: f}).InspectUserUnits(context.Background())
	if err != nil || len(got) != 3 {
		t.Fatalf("InspectUserUnits = %+v err=%v", got, err)
	}
	if got[0].RawUnitFileState != "linked" || got[0].LinkedSource != "/home/test/Projects/app/linked.service" || got[0].StartIntent != profile.ServiceStartNotManaged || got[1].StartIntent != profile.ServiceStartNotManaged || !got[1].Runtime || got[2].StartIntent != profile.ServiceStartNotManaged || !got[2].Runtime {
		t.Fatalf("raw runtime/linked state was treated as portable start intent: %+v", got)
	}
}
