package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/updates"
)

func TestOverviewUpdateNoticeLineDetailsAndReleasePage(t *testing.T) {
	screen := NewOverview(nil)
	screen.SetSize(100, 12)
	before := screen.listHeight()
	if cmd := screen.Update(tea.KeyPressMsg{Code: 'u'}); cmd != nil || screen.showUpdate {
		t.Fatal("u did something without a notice")
	}
	notice := updates.Notice{Current: "v0.1.1", Latest: "v0.1.2", DetailsURL: "https://github.com/Grenco/omarchy-blueprint/releases/tag/v0.1.2", Instructions: "Download this release's PKGBUILD and run makepkg -si."}
	screen.SetUpdateNotice(&notice)
	if view := screen.View(); !strings.HasPrefix(view, "↑ Update available · v0.1.1 → v0.1.2 · u details") {
		t.Fatalf("notice line missing:\n%s", view)
	}
	if screen.listHeight() != before-1 {
		t.Fatalf("list height %d, want %d: the notice must come out of the list's budget", screen.listHeight(), before-1)
	}
	if cmd := screen.Update(tea.KeyPressMsg{Code: 'b'}); cmd != nil {
		t.Fatal("b opened the browser before the details were shown")
	}
	screen.Update(tea.KeyPressMsg{Code: 'u'})
	detail := screen.DetailView()
	for _, want := range []string{"Update available", "v0.1.2 is out; this is v0.1.1", "makepkg -si", notice.DetailsURL, "never installs them"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("details lack %q:\n%s", want, detail)
		}
	}
	cmd := screen.Update(tea.KeyPressMsg{Code: 'b'})
	if cmd == nil {
		t.Fatal("b did not open the release page")
	}
	if request, ok := cmd().(HandoffRequest); !ok || request.Kind != "browser" || request.Path != notice.DetailsURL {
		t.Fatalf("b requested %#v", cmd())
	}
	screen.Update(tea.KeyPressMsg{Code: 'u'})
	if strings.Contains(screen.DetailView(), "never installs them") {
		t.Fatal("u did not hide the details")
	}
}
