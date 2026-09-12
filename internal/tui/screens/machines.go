package screens

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Machines manages overlay metadata only; mappings never move resource bytes.
type Machines struct {
	session                 *workflow.Session
	width, height, selected int
	name, mode, confirm     string
	browser                 *components.Browser
	resource                int
	focusMapping            string
	err                     error
}

type machineMutationMsg struct{ err error }

func NewMachines(session *workflow.Session) *Machines { return &Machines{session: session} }
func (s *Machines) Focus(mapping string) {
	s.focusMapping = mapping
	for i, item := range s.machines() {
		if strings.HasPrefix(mapping, item.Name+":") {
			s.selected = i
			break
		}
	}
}
func (s *Machines) SetSize(width, height int) { s.width, s.height = width, height }
func (s *Machines) Init() tea.Cmd             { return nil }

func (s *Machines) Update(msg tea.Msg) tea.Cmd {
	if result, ok := msg.(machineMutationMsg); ok {
		s.err = result.err
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if s.browser != nil {
		if key.String() == "enter" && s.browser.CanSelect() {
			entry, _ := s.browser.Selected()
			s.browser = nil
			return s.mapResource(entry.Path)
		}
		cmd := s.browser.Update(msg)
		if s.browser.Closed() {
			s.browser = nil
		}
		return cmd
	}
	if s.confirm != "" {
		if key.String() == "esc" {
			s.confirm = ""
			return nil
		}
		if key.String() == "enter" {
			action := s.confirm
			s.confirm = ""
			if action == "remove" {
				return s.remove()
			}
			return s.rename()
		}
		return nil
	}
	if s.mode != "" {
		if key.String() == "esc" {
			s.mode, s.name = "", ""
			return nil
		}
		if key.String() == "enter" {
			mode := s.mode
			s.mode = ""
			if mode == "add" {
				return s.add()
			}
			s.confirm = "rename"
			return nil
		}
		if key.String() == "backspace" && len(s.name) > 0 {
			s.name = s.name[:len(s.name)-1]
			return nil
		}
		if len(key.String()) == 1 {
			s.name += key.String()
		}
		return nil
	}
	switch key.String() {
	case "j", "down":
		if s.selected < len(s.machines())-1 {
			s.selected++
		}
	case "k", "up":
		if s.selected > 0 {
			s.selected--
		}
	case "a":
		s.mode = "add"
		s.name, _ = s.session.SuggestedMachineName()
	case "u":
		return s.use()
	case "c":
		return s.clear()
	case "r":
		if current := s.selectedMachine(); current.Name != "" {
			s.mode, s.name = "rename", current.Name
		}
	case "x":
		if s.selectedMachine().Name != "" {
			s.confirm = "remove"
		}
	case "m":
		if len(s.session.Profile().Resources.Items) > 0 && s.selectedMachine().Name != "" {
			home, _ := os.UserHomeDir()
			browser := components.NewBrowser(components.PickDirectory, components.BrowserConfig{Home: home, ProfileDir: s.session.ProfileDir(), Profile: s.session.Profile()})
			s.browser = &browser
			return s.browser.Init()
		}
	case "n":
		if len(s.session.Profile().Resources.Items) > 0 {
			s.resource = (s.resource + 1) % len(s.session.Profile().Resources.Items)
		}
	}
	return nil
}

func (s *Machines) View() string {
	if s.err != nil {
		return "Machines\n\nUnable to update machine: " + s.err.Error()
	}
	if s.browser != nil {
		return "Map " + s.selectedResource().ID + " to a directory\n\n" + s.browser.View()
	}
	if s.confirm == "remove" {
		return components.Confirm("Remove " + s.selectedMachine().Name + "? Resource files remain untouched.")
	}
	if s.confirm == "rename" {
		return components.Confirm("Rename machine to " + s.name + "?")
	}
	if s.mode != "" {
		return "Machine name: " + s.name + "\nPress Enter to confirm."
	}
	lines := []string{"Machines"}
	for i, item := range s.machines() {
		marker := " "
		if i == s.selected {
			marker = ">"
		}
		active := ""
		if item.Name == s.session.Machine().Name {
			active = " *"
		}
		lines = append(lines, marker+" "+item.Name+active)
	}
	if len(s.machines()) == 0 {
		lines = append(lines, "No machine overlays.")
	}
	if current := s.selectedMachine(); current.Name != "" {
		for _, mapping := range current.ResourcePaths {
			label := "dormant"
			if s.resourceExists(mapping.Resource) {
				label = "override"
			}
			lines = append(lines, fmt.Sprintf("  %s -> %s (%s)", mapping.Resource, mapping.Path, label))
		}
		if resource := s.selectedResource(); resource.ID != "" {
			lines = append(lines, "Mapping target: "+resource.ID)
		}
	}
	lines = append(lines, "a add  u use  c clear  r rename  x remove  n resource  m map")
	return strings.Join(lines, "\n")
}

func (s *Machines) machines() []profile.Machine { return s.session.Profile().Machines.Items }
func (s *Machines) selectedMachine() profile.Machine {
	items := s.machines()
	if s.selected >= 0 && s.selected < len(items) {
		return items[s.selected]
	}
	return profile.Machine{}
}
func (s *Machines) selectedResource() profile.Resource {
	items := s.session.Profile().Resources.Items
	if s.resource >= 0 && s.resource < len(items) {
		return items[s.resource]
	}
	return profile.Resource{}
}
func (s *Machines) resourceExists(id string) bool {
	for _, item := range s.session.Profile().Resources.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}
func (s *Machines) mutate(run func() error) tea.Cmd {
	return func() tea.Msg { return machineMutationMsg{err: run()} }
}
func (s *Machines) add() tea.Cmd {
	name := s.name
	return s.mutate(func() error { return s.session.AddMachine(context.Background(), name, true) })
}
func (s *Machines) use() tea.Cmd {
	name := s.selectedMachine().Name
	return s.mutate(func() error { return s.session.UseMachine(context.Background(), name) })
}
func (s *Machines) clear() tea.Cmd {
	return s.mutate(func() error { return s.session.ClearMachine(context.Background()) })
}
func (s *Machines) rename() tea.Cmd {
	old, name := s.selectedMachine().Name, s.name
	return s.mutate(func() error { return s.session.RenameMachine(context.Background(), old, name) })
}
func (s *Machines) remove() tea.Cmd {
	name := s.selectedMachine().Name
	return s.mutate(func() error { return s.session.RemoveMachine(context.Background(), name) })
}
func (s *Machines) mapResource(path string) tea.Cmd {
	name, resource := s.selectedMachine().Name, s.selectedResource().ID
	return s.mutate(func() error { return s.session.MapResource(context.Background(), name, resource, path) })
}
