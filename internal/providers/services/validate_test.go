package services

import (
	"context"
	"errors"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type proposedSetSystemd struct {
	discoverySystemd
	t     *testing.T
	want  map[string]string
	root  string
	calls int
	fail  bool
}

func (s *proposedSetSystemd) VerifyUnitSet(_ context.Context, set ProposedUnitSet) error {
	s.calls++
	s.root = set.Root
	for path, want := range s.want {
		got, err := os.ReadFile(filepath.Join(set.Root, path))
		if err != nil || string(got) != want {
			s.t.Fatalf("proposed set lost %s: %q err=%v", path, got, err)
		}
	}
	if s.fail {
		return errors.New("systemd rejected proposed set")
	}
	return nil
}

func TestValidateProposedSetPreservesBytesAndCleansTemporaryTree(t *testing.T) {
	for _, fail := range []bool{false, true} {
		s := &proposedSetSystemd{t: t, fail: fail, want: map[string]string{"backup.service": "[Service]\n# exact comment\nExecStart=/usr/bin/true\n", "backup.service.d/10-custom.conf": "[Service]\nEnvironment=MODE=custom\n"}}
		p := Provider{Systemd: s}
		files := map[string][]byte{}
		for name, bytes := range s.want {
			files[name] = []byte(bytes)
		}
		err := p.validateProposedSet(context.Background(), files)
		if (err != nil) != fail || s.calls != 1 {
			t.Fatalf("verification calls=%d err=%v", s.calls, err)
		}
		if _, err := os.Stat(s.root); !os.IsNotExist(err) {
			t.Fatalf("validation tree was retained: %v", err)
		}
	}
}

func TestValidateProposedSetRejectsEscapingPathBeforeSystemd(t *testing.T) {
	s := &proposedSetSystemd{t: t}
	p := Provider{Systemd: s}
	if err := p.validateProposedSet(context.Background(), map[string][]byte{"../outside.service": []byte("[Service]\n")}); err == nil || s.calls != 0 {
		t.Fatalf("escaping proposed path reached systemd: calls=%d err=%v", s.calls, err)
	}
}

func TestValidateRealSystemdLoadsProposedDropIns(t *testing.T) {
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		t.Skip("systemd-analyze is not installed")
	}
	p := Provider{Systemd: Systemctl{Runner: command.SystemRunner{}}}
	base := []byte("[Unit]\nDefaultDependencies=no\n[Service]\nType=oneshot\nExecStart=/usr/bin/true\n")
	if err := p.validateProposedSet(context.Background(), map[string][]byte{"blueprint-verify.service": base}); err != nil {
		t.Fatalf("valid isolated service rejected: %v", err)
	}
	if err := p.validateProposedSet(context.Background(), map[string][]byte{"blueprint-verify.service": base, "blueprint-verify.service.d/10-invalid.conf": []byte("[Service]\nExecStart=\n")}); err == nil {
		t.Fatal("systemd ignored the proposed drop-in that removes required ExecStart")
	}
}
