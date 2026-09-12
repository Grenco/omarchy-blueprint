package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Sync is a deliberately narrow UI over Profile Git's safe operations.
type Sync struct {
	session                 *workflow.Session
	width, height, selected int
	status                  profilegit.Status
	lastFetched             time.Time
	diff                    *components.DiffViewer
	input, confirm, message string
	err                     error
}

type SyncAction struct {
	ID, Label, Shortcut, DisabledReason string
	Enabled                             bool
}
type syncStatusMsg struct {
	status profilegit.Status
	err    error
}
type syncResultMsg struct {
	status profilegit.Status
	err    error
}
type syncDiffMsg struct {
	diff profilegit.Diff
	err  error
}

func NewSync(session *workflow.Session) *Sync { return &Sync{session: session} }
func (s *Sync) SetSize(width, height int) {
	s.width, s.height = width, height
	if s.diff != nil {
		s.diff.SetSize(width, height)
	}
}
func (s *Sync) Init() tea.Cmd { return s.refresh() }

func (s *Sync) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case syncStatusMsg:
		s.status, s.err = msg.status, msg.err
		if s.selected >= len(s.status.Changes) {
			s.selected = max(0, len(s.status.Changes)-1)
		}
		return nil
	case syncResultMsg:
		s.err = msg.err
		if msg.err == nil {
			s.status = msg.status
		}
		return nil
	case syncDiffMsg:
		s.err = msg.err
		if msg.err == nil && len(msg.diff.Files) > 0 {
			viewer := components.NewDiffViewer(msg.diff.Files[0].Document)
			viewer.SetSize(s.width, s.height)
			s.diff = &viewer
		}
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if s.input != "" {
		switch key.String() {
		case "esc":
			s.input = ""
		case "backspace":
			if len(s.message) > 0 {
				s.message = s.message[:len(s.message)-1]
			}
		case "enter":
			if s.input == "commit" {
				s.input, s.confirm = "", "commit"
			} else if s.input == "commit-push" {
				s.input, s.confirm = "", "commit-push"
			} else {
				return s.setRemote(s.message)
			}
		default:
			if text := key.Text; text != "" {
				s.message += text
			}
		}
		return nil
	}
	if s.confirm != "" {
		switch key.String() {
		case "esc":
			s.confirm = ""
		case "enter":
			if s.confirm == "commit" {
				return s.commit(false)
			}
			if s.confirm == "commit-push" {
				return s.commit(true)
			}
		}
		return nil
	}
	if s.diff != nil {
		if key.String() == "esc" {
			s.diff = nil
			return nil
		}
		return s.diff.Update(msg)
	}
	switch key.String() {
	case "j", "down":
		if s.selected < len(s.status.Changes)-1 {
			s.selected++
		}
	case "k", "up":
		if s.selected > 0 {
			s.selected--
		}
	case "d":
		if change := s.selectedChange(); change.Managed {
			return s.loadDiff(change.Path)
		}
	case "f":
		if s.allowed("fetch") {
			return s.fetch()
		}
	case "c":
		if s.allowed("commit") {
			s.input, s.message = "commit", profilegit.SuggestedCommitMessage(s.status)
		}
	case "g":
		if s.allowed("commit-push") {
			s.input, s.message = "commit-push", profilegit.SuggestedCommitMessage(s.status)
		}
	case "p":
		if s.allowed("push") {
			return s.push()
		}
	case "l":
		if s.allowed("pull") {
			return s.pull()
		}
	case "i":
		if s.allowed("init") {
			return s.initRepo()
		}
	case "r":
		if s.status.Repository {
			s.input, s.message = "remote", s.status.Origin
		}
	case "x":
		if s.allowed("remove-remote") {
			return s.removeRemote()
		}
	case "y":
		if s.status.Origin != "" {
			return func() tea.Msg { return HandoffRequest{Kind: "copy", Path: s.status.Origin} }
		}
	case "v":
		if s.complex() {
			return func() tea.Msg { return HandoffRequest{Kind: "copy", Path: s.session.ProfileDir()} }
		}
	case "b":
		if url, ok := profilegit.BrowserURL(s.status.Origin); ok {
			return func() tea.Msg { return HandoffRequest{Kind: "browser", Path: url} }
		}
	case "z":
		if s.complex() {
			return func() tea.Msg { return HandoffRequest{Kind: "lazygit", Path: s.session.ProfileDir()} }
		}
	}
	return nil
}

func (s *Sync) View() string {
	if s.err != nil {
		return "Sync\n\nUnable to load profile Git state: " + s.err.Error()
	}
	if s.input != "" {
		return fmt.Sprintf("Sync\n\n%s: %s\nEnter confirm  Esc cancel", map[string]string{"commit": "Commit message", "commit-push": "Commit & Push message", "remote": "Origin URL"}[s.input], s.message)
	}
	if s.confirm != "" {
		return components.Confirm("Commit managed profile changes with message: " + s.message + "?")
	}
	if s.diff != nil {
		return "Profile Git diff\n" + s.diff.View()
	}
	if !s.status.Repository {
		return "Sync\n\nProfile is not a Git repository.\ni initialize repository"
	}
	branch := s.status.Branch
	if branch == "" {
		branch = "detached HEAD"
	}
	fetched := "not fetched this session"
	if !s.lastFetched.IsZero() {
		fetched = s.lastFetched.Local().Format(time.Kitchen)
	}
	lines := []string{fmt.Sprintf("Sync  %s  ahead:%d behind:%d  last fetched: %s", branch, s.status.Ahead, s.status.Behind, fetched)}
	if s.status.Origin == "" {
		lines = append(lines, "Origin: not configured (r set origin)")
	} else {
		lines = append(lines, "Origin: "+s.status.Origin)
	}
	for i, change := range s.status.Changes {
		marker := " "
		if i == s.selected {
			marker = ">"
		}
		managed := "unmanaged"
		if change.Managed {
			managed = "managed"
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s", marker, change.Path, managed))
	}
	if len(s.status.Changes) == 0 {
		lines = append(lines, "Working tree clean.")
	}
	if s.complex() {
		lines = append(lines, "Complex Git state: z LazyGit  y copy origin  v copy profile path")
	} else {
		lines = append(lines, "f fetch  d diff  c commit  g commit & push  l pull  p push  r origin")
	}
	return strings.Join(lines, "\n")
}

func (s *Sync) Actions() []SyncAction {
	actions := []SyncAction{{"sync.init", "Initialize profile repository", "i", "profile is already a Git repository", s.allowed("init")}, {"sync.fetch", "Fetch origin", "f", s.reason("fetch"), s.allowed("fetch")}, {"sync.commit", "Commit managed changes", "c", s.reason("commit"), s.allowed("commit")}, {"sync.commit-push", "Commit & Push", "g", s.reason("commit-push"), s.allowed("commit-push")}, {"sync.pull", "Pull fast-forward", "l", s.reason("pull"), s.allowed("pull")}, {"sync.push", "Push", "p", s.reason("push"), s.allowed("push")}, {"sync.set-remote", "Set origin", "r", s.reason("set-remote"), s.allowed("set-remote")}, {"sync.remove-remote", "Remove origin", "x", s.reason("remove-remote"), s.allowed("remove-remote")}}
	if s.status.Origin != "" {
		_, known := profilegit.BrowserURL(s.status.Origin)
		actions = append(actions, SyncAction{"sync.copy-remote", "Copy origin", "y", "origin is not configured", true})
		if !s.complex() {
			actions = append(actions, SyncAction{"sync.open-remote", "Open origin in browser", "b", "origin is not a supported HTTPS or SSH remote", known})
		}
	}
	if s.complex() {
		actions = append(actions, SyncAction{"sync.lazygit", "Open LazyGit", "z", "", true}, SyncAction{"sync.copy-profile", "Copy profile path", "v", "", true})
	}
	return actions
}

func (s *Sync) selectedChange() profilegit.Change {
	if s.selected >= 0 && s.selected < len(s.status.Changes) {
		return s.status.Changes[s.selected]
	}
	return profilegit.Change{}
}
func (s *Sync) complex() bool {
	if !s.status.Repository {
		return false
	}
	if s.status.Branch == "" || s.status.Ahead > 0 && s.status.Behind > 0 {
		return true
	}
	for _, change := range s.status.Changes {
		if change.Index != "" || !change.Managed {
			return true
		}
	}
	return false
}
func (s *Sync) allowed(action string) bool { return s.reason(action) == "" }
func (s *Sync) reason(action string) string {
	if action == "init" {
		if !s.status.Repository {
			return ""
		}
		return "profile is already a Git repository"
	}
	if !s.status.Repository {
		return "profile is not a Git repository"
	}
	if s.complex() {
		return "complex Git state; use LazyGit or copy the profile path"
	}
	managed := false
	for _, change := range s.status.Changes {
		managed = managed || change.Managed
	}
	switch action {
	case "set-remote":
		return ""
	case "remove-remote":
		if s.status.Origin == "" {
			return "origin is not configured"
		}
	case "fetch":
		if s.status.Origin == "" {
			return "origin is not configured"
		}
	case "commit":
		if !managed {
			return "no managed profile changes"
		}
	case "commit-push":
		if !managed && s.status.Ahead == 0 {
			return "no managed changes or commits to push"
		}
		if s.status.Origin == "" {
			return "origin is not configured"
		}
		if s.status.Branch == "" {
			return "cannot push a detached HEAD"
		}
	case "pull":
		if s.status.Head == "" {
			return "profile repository has no HEAD commit"
		}
		if s.status.Upstream == "" {
			return "branch has no upstream"
		}
		if s.status.Behind == 0 {
			return "no fetched commits to pull"
		}
	case "push":
		if s.status.Head == "" {
			return "profile repository has no HEAD commit"
		}
		if s.status.Origin == "" {
			return "origin is not configured"
		}
		if s.status.Branch == "" {
			return "cannot push a detached HEAD"
		}
	}
	return ""
}
func (s *Sync) refresh() tea.Cmd {
	return func() tea.Msg {
		status, err := s.session.ProfileGitStatus(context.Background())
		return syncStatusMsg{status, err}
	}
}
func (s *Sync) loadDiff(path string) tea.Cmd {
	return func() tea.Msg {
		diff, err := s.session.ProfileGitDiff(context.Background(), path)
		return syncDiffMsg{diff, err}
	}
}
func (s *Sync) initRepo() tea.Cmd {
	return func() tea.Msg {
		result, err := s.session.ProfileGitInit(context.Background())
		if err != nil {
			return syncResultMsg{err: err}
		}
		return syncResultMsg{status: result.Status}
	}
}
func (s *Sync) fetch() tea.Cmd {
	return func() tea.Msg {
		_, err := s.session.ProfileGitFetch(context.Background())
		if err != nil {
			return syncResultMsg{err: err}
		}
		status, err := s.session.ProfileGitStatus(context.Background())
		if err == nil {
			s.lastFetched = time.Now()
		}
		return syncResultMsg{status: status, err: err}
	}
}
func (s *Sync) setRemote(raw string) tea.Cmd {
	return func() tea.Msg {
		result, err := s.session.ProfileGitSetRemote(context.Background(), raw)
		return syncResultMsg{status: result.Status, err: err}
	}
}
func (s *Sync) removeRemote() tea.Cmd {
	return func() tea.Msg {
		result, err := s.session.ProfileGitRemoveRemote(context.Background())
		return syncResultMsg{status: result.Status, err: err}
	}
}
func (s *Sync) pull() tea.Cmd {
	return func() tea.Msg {
		result, err := s.session.ProfileGitPull(context.Background())
		return syncResultMsg{status: result.Status, err: err}
	}
}
func (s *Sync) push() tea.Cmd {
	return func() tea.Msg {
		result, err := s.session.ProfileGitPush(context.Background())
		return syncResultMsg{status: result.Status, err: err}
	}
}
func (s *Sync) commit(push bool) tea.Cmd {
	message := s.message
	s.confirm, s.message = "", ""
	return func() tea.Msg {
		result, err := s.session.ProfileGitCommit(context.Background(), message)
		if err != nil {
			return syncResultMsg{err: err}
		}
		status, err := s.session.ProfileGitStatus(context.Background())
		if err != nil || !push || s.reasonFor(status, "push") != "" {
			return syncResultMsg{status: status, err: err}
		}
		result, err = s.session.ProfileGitPush(context.Background())
		return syncResultMsg{status: result.Status, err: err}
	}
}
func (s *Sync) reasonFor(status profilegit.Status, action string) string {
	old := s.status
	s.status = status
	reason := s.reason(action)
	s.status = old
	return reason
}
