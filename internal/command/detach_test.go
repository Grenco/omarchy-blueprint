package command

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunDetachesFromTheTerminalSoNothingCanPrompt(t *testing.T) {
	// A session leader has session id == its pid; a detached command has
	// no controlling terminal for sudo, ssh or git to prompt on.
	out, err := SystemRunner{}.Run(context.Background(), "sh", "-c", `echo "prompt=$GIT_TERMINAL_PROMPT"; [ "$(awk '{print $6}' /proc/$$/stat)" = "$$" ] && echo leader`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "prompt=0") || !strings.Contains(out, "leader") {
		t.Fatalf("command was not detached: %q", out)
	}
}

func TestRunAsksACancelledCommandToStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	out, _ := SystemRunner{}.Run(ctx, "sh", "-c", `trap 'echo stopped; exit 0' TERM; sleep 5 & wait`)
	if !strings.Contains(out, "stopped") {
		t.Fatalf("cancelled command was not sent SIGTERM: %q", out)
	}
}
