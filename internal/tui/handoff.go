package tui

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-shellwords"
)

var ErrHandoffDisabled = errors.New("handoff is unavailable")

type Handoff struct {
	LookupPath func(string) (string, error)
	Visual     string
	Editor     string
}

func (h Handoff) EditorCmd(path string) (*exec.Cmd, error) {
	configured := h.Visual
	if strings.TrimSpace(configured) == "" {
		configured = h.Editor
	}
	if strings.TrimSpace(configured) == "" {
		return nil, fmt.Errorf("%w: set VISUAL or EDITOR", ErrHandoffDisabled)
	}
	return h.command(configured, path)
}

func (h Handoff) FileManagerCmd(path string, isDir bool) (*exec.Cmd, error) {
	if !isDir {
		path = filepath.Dir(path)
	}
	return h.command("xdg-open", path)
}

func (h Handoff) BrowserCmd(rawURL string) (*exec.Cmd, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, fmt.Errorf("%w: only HTTPS URLs may be opened", ErrHandoffDisabled)
	}
	return h.command("xdg-open", rawURL)
}

func (h Handoff) LazyGitCmd(repo string) (*exec.Cmd, error) {
	path, err := h.lookup("lazygit")
	if err != nil {
		return nil, fmt.Errorf("%w: lazygit is not on PATH", ErrHandoffDisabled)
	}
	return &exec.Cmd{Path: path, Dir: repo}, nil
}

func (h Handoff) command(configured string, lastArg string) (*exec.Cmd, error) {
	parser := shellwords.NewParser()
	parser.ParseEnv = false
	parser.ParseBacktick = false
	argv, err := parser.Parse(configured)
	if err != nil || len(argv) == 0 {
		return nil, fmt.Errorf("%w: invalid command", ErrHandoffDisabled)
	}
	path, err := h.lookup(argv[0])
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not on PATH", ErrHandoffDisabled, argv[0])
	}
	return exec.Command(path, append(argv[1:], lastArg)...), nil
}

func (h Handoff) lookup(name string) (string, error) {
	if h.LookupPath != nil {
		return h.LookupPath(name)
	}
	return exec.LookPath(name)
}

func handoffCmd(refresh ScreenID, kind string, command *exec.Cmd) tea.Cmd {
	return tea.ExecProcess(command, func(err error) tea.Msg {
		return handoffFinishedMsg{Refresh: refresh, Kind: kind, Err: err}
	})
}

func copyCmd(text string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg { return clipboardCopiedMsg{} })
}
