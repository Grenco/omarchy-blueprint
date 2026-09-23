package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type profileCreatedMsg struct {
	session *workflow.Session
	dir     string
	verb    string
	err     error
}

// handleProfileCreated is the canonical response to a profile Create/Open
// attempt: on success it rebuilds the root against the new session, on
// failure it reports the error inline within the welcome flow.
func (m model) handleProfileCreated(created profileCreatedMsg) (tea.Model, tea.Cmd) {
	m.welcomeBusy = false
	if created.err != nil {
		m.welcomeError = created.err
		return m, nil
	}
	dir := created.dir
	if dir == "" {
		dir = m.profileDir
	}
	fresh := newModelWithContext(m.ctx, m.cancel, m.themeLoader, created.session, dir, m.createProfile)
	fresh.width, fresh.height = m.width, m.height
	fresh.setScreenSizes()
	return fresh, fresh.Init()
}

func (m *model) enableProfileChooser(openSession func(workflow.Options) (*workflow.Session, error), machine string) {
	m.openSession, m.machine, m.welcomeChooser, m.welcomeStep, m.welcomeChoice = openSession, machine, true, "choose", 0
	path := filepath.Join(filepath.Dir(m.profileDir), "omarchy-profile")
	if home, err := os.UserHomeDir(); err == nil {
		path = filepath.Join(home, "omarchy-profile")
	}
	m.welcomePath = components.NewTextInputModal(path, "Profile path")
	m.openModal(modalWelcome)
}

func (m model) updateWelcome(msg tea.Msg, key string, isKey bool) (tea.Model, tea.Cmd) {
	if m.welcomeChooser && m.welcomeStep == "choose" {
		if isKey {
			switch key {
			case "q", "esc":
				m.stop()
				return m, tea.Quit
			case "up", "k":
				m.welcomeChoice = max(0, m.welcomeChoice-1)
			case "down", "j":
				m.welcomeChoice = min(2, m.welcomeChoice+1)
			case "enter":
				switch m.welcomeChoice {
				case 0:
					m.welcomeStep = "create-path"
					return m, m.welcomePath.Focus()
				case 1:
					m.welcomeStep = "open-path"
					return m, m.welcomePath.Focus()
				default:
					m.stop()
					return m, tea.Quit
				}
			}
		}
		return m, nil
	}
	if isKey {
		switch key {
		case "q":
			m.stop()
			return m, tea.Quit
		case "esc":
			if m.welcomeChooser {
				if m.welcomeStep == "create-name" {
					m.welcomeStep, m.welcomeError = "create-path", nil
					return m, m.welcomePath.Focus()
				}
				m.welcomeStep, m.welcomeError = "choose", nil
				return m, nil
			}
			m.stop()
			return m, tea.Quit
		case "enter":
			if m.welcomeStep == "create-path" {
				path := expandWelcomePath(strings.TrimSpace(m.welcomePath.Value()))
				if path == "" {
					m.welcomeError = fmt.Errorf("profile path is required")
					return m, nil
				}
				m.profileDir, m.welcomeStep = path, "create-name"
				m.welcomeName = components.NewTextInputModal(filepath.Base(filepath.Clean(path)), "Profile name")
				return m, m.welcomeName.Focus()
			}
			if m.welcomeStep == "open-path" {
				path := expandWelcomePath(strings.TrimSpace(m.welcomePath.Value()))
				if path == "" {
					m.welcomeError = fmt.Errorf("profile path is required")
					return m, nil
				}
				if !m.welcomeBusy {
					m.welcomeBusy, m.welcomeError = true, nil
					return m, func() tea.Msg {
						session, err := m.openSession(workflow.Options{ProfileDir: path, ExplicitMachine: m.machine})
						dir := path
						if session != nil {
							dir = session.ProfileDir()
						}
						return profileCreatedMsg{session: session, dir: dir, verb: "opened", err: err}
					}
				}
				return m, nil
			}
			name := strings.TrimSpace(m.welcomeName.Value())
			if name == "" {
				m.welcomeError = fmt.Errorf("profile name is required")
				return m, nil
			}
			if !m.welcomeBusy {
				m.welcomeBusy, m.welcomeError = true, nil
				return m, func() tea.Msg {
					session, err := m.createProfile(m.ctx, m.profileDir, name)
					dir := m.profileDir
					if session != nil {
						dir = session.ProfileDir()
					}
					return profileCreatedMsg{session: session, dir: dir, verb: "created", err: err}
				}
			}
		}
	}
	var cmd tea.Cmd
	if m.welcomeStep == "create-path" || m.welcomeStep == "open-path" {
		cmd = m.welcomePath.Update(msg)
	} else {
		cmd = m.welcomeName.Update(msg)
	}
	return m, cmd
}

func expandWelcomePath(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
}

// welcomeFooter is the canonical footer text for the profile Create/Open/Quit flow.
func (m model) welcomeFooter() string {
	if m.welcomeChooser {
		switch m.welcomeStep {
		case "choose":
			return "up/down select   enter continue   esc quit"
		case "open-path":
			return "enter open   esc back"
		case "create-name":
			return "enter create   esc back"
		default:
			return "enter continue   esc back"
		}
	}
	return "enter create   esc quit"
}

// welcomeContent is the canonical modal body for the profile Create/Open/Quit flow.
func (m model) welcomeContent(width int) string {
	styles := components.NewStyles(m.palette)
	lines := []string{}
	if m.welcomeChooser && m.welcomeStep == "choose" {
		lines = append(lines, "No Blueprint profile was found here.", styles.SubtleAccent("A profile is the folder where Blueprint saves what it captures."), "")
		for i, choice := range []struct{ label, hint string }{
			{"Create a new profile", "start fresh from this machine"},
			{"Open an existing profile", "use a profile folder you already have"},
			{"Quit", ""},
		} {
			if lipgloss.Width("  "+choice.label+"  "+choice.hint) > width {
				choice.hint = ""
			}
			if i != m.welcomeChoice {
				line := "  " + choice.label
				if choice.hint != "" {
					line += "  " + styles.SubtleAccent(choice.hint)
				}
				lines = append(lines, line)
				continue
			}
			line := "  " + choice.label
			if choice.hint != "" {
				line += "  " + choice.hint
			}
			if !m.palette.ColorEnabled {
				line = components.Icons.Selected + line[1:]
			}
			lines = append(lines, styles.Selection(components.PadLine(line, width), true))
		}
	} else if m.welcomeStep == "create-path" || m.welcomeStep == "open-path" {
		action := "Create profile at:"
		if m.welcomeStep == "open-path" {
			action = "Open profile at:"
		}
		lines = append(lines, action, "", m.welcomePath.View())
	} else {
		lines = append(lines,
			"Create a new profile at:",
			components.DisplayText(m.profileDir),
			"",
			"Profile name: "+m.welcomeName.View(),
		)
	}
	if m.welcomeBusy {
		lines = append(lines, "", styles.Muted("Working..."))
	}
	if m.welcomeError != nil {
		lines = append(lines, "", styles.Error("Error: "+components.DisplayText(m.welcomeError.Error())))
	}
	return strings.Join(lines, "\n")
}
