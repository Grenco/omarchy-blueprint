package profilegit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

type failingRemoteRunner struct{ command.Runner }

func (failingRemoteRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	for _, arg := range args {
		if strings.Contains(arg, "user:secret@") {
			return "fatal: invalid URL https://user:secret@example.invalid/repo", &command.RunError{Name: name, Args: args, Output: "fatal: invalid URL https://user:secret@example.invalid/repo", Err: errors.New("exit status 128")}
		}
	}
	return command.SystemRunner{}.Run(ctx, name, args...)
}

func TestSanitizeRemoteFailsClosedForMalformedUserinfo(t *testing.T) {
	got := SanitizeRemote("https://user:secret@example.invalid/[bad")
	if strings.Contains(got, "user:secret") || strings.Contains(got, "secret") {
		t.Fatalf("credential leaked: %q", got)
	}
}

func TestSetRemoteFailureRedactsCredentials(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root)
	base := profileGitService(t, root)
	if _, err := base.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	service, err := New(failingRemoteRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SetRemote(context.Background(), "https://user:secret@example.invalid/repo")
	if err == nil || strings.Contains(err.Error(), "user:secret") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("credential leaked in error: %v", err)
	}
}
