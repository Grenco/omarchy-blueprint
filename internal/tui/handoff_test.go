package tui

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHandoffEditorCmd(t *testing.T) {
	lookup := func(name string) (string, error) { return "/bin/" + name, nil }
	for _, test := range []struct {
		name, visual, editor string
		wantArgs             []string
	}{
		{"visual", "nvim", "", []string{"/bin/nvim", "/tmp/file"}},
		{"quoted arguments", `code --wait --title "My File"`, "", []string{"/bin/code", "--wait", "--title", "My File", "/tmp/file"}},
		{"editor fallback", "", "vim -f", []string{"/bin/vim", "-f", "/tmp/file"}},
		{"backticks literal", "nvim `touch nope`", "", []string{"/bin/nvim", "`touch nope`", "/tmp/file"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd, err := (Handoff{LookupPath: lookup, Visual: test.visual, Editor: test.editor}).EditorCmd("/tmp/file")
			if err != nil || !reflect.DeepEqual(cmd.Args, test.wantArgs) {
				t.Fatalf("command = %#v, error = %v", cmd, err)
			}
		})
	}
	if _, err := (Handoff{}).EditorCmd("/tmp/file"); !errors.Is(err, ErrHandoffDisabled) {
		t.Fatalf("missing editor error = %v", err)
	}
}

func TestHandoffFileManagerAndBrowserCmd(t *testing.T) {
	lookup := func(name string) (string, error) { return "/bin/" + name, nil }
	handoff := Handoff{LookupPath: lookup}
	file, err := handoff.FileManagerCmd("/tmp/example/file", false)
	if err != nil || !reflect.DeepEqual(file.Args, []string{"/bin/xdg-open", filepath.Clean("/tmp/example")}) {
		t.Fatalf("file command = %#v, error = %v", file, err)
	}
	directory, err := handoff.FileManagerCmd("/tmp/example", true)
	if err != nil || !reflect.DeepEqual(directory.Args, []string{"/bin/xdg-open", "/tmp/example"}) {
		t.Fatalf("directory command = %#v, error = %v", directory, err)
	}
	valid, err := handoff.BrowserCmd("https://example.test/path")
	if err != nil || !reflect.DeepEqual(valid.Args, []string{"/bin/xdg-open", "https://example.test/path"}) {
		t.Fatalf("browser command = %#v, error = %v", valid, err)
	}
	for _, rawURL := range []string{"file:///tmp/x", "javascript:alert(1)", "ftp://example.test", "example.test"} {
		if _, err := handoff.BrowserCmd(rawURL); !errors.Is(err, ErrHandoffDisabled) {
			t.Errorf("BrowserCmd(%q) error = %v", rawURL, err)
		}
	}
}

func TestHandoffLazyGitCmd(t *testing.T) {
	missing := Handoff{LookupPath: func(string) (string, error) { return "", errors.New("missing") }}
	if _, err := missing.LazyGitCmd("/repo"); !errors.Is(err, ErrHandoffDisabled) {
		t.Fatalf("missing lazygit error = %v", err)
	}
	cmd, err := (Handoff{LookupPath: func(string) (string, error) { return "/bin/lazygit", nil }}).LazyGitCmd("/repo")
	if err != nil || cmd.Path != "/bin/lazygit" || cmd.Dir != "/repo" || len(cmd.Args) != 0 {
		t.Fatalf("command = %#v, error = %v", cmd, err)
	}
}

func TestHandoffCallbacks(t *testing.T) {
	if handoffCmd(ScreenSync, "sync", nil) == nil {
		t.Fatal("handoff command is nil")
	}
	if copyCmd("value") == nil {
		t.Fatal("copy command is nil")
	}
}
