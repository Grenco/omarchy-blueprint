package screens

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Machines manages overlay metadata only; mappings never move resource bytes.
type Machines struct {
	ctx                     context.Context
	session                 *workflow.Session
	width, height, selected int
	name, mode, confirm     string
	browser                 *components.Browser
	resource                int
	focusMapping            string
	focusMappings           bool
	machineList             components.Selectable
	mappingNavigation       components.Selectable
	mappingTable            components.Table
	styles                  components.Styles
	busy                    bool
	err                     error
}

type MachineMutationComplete struct {
	Notice string
	Err    error
}

func NewMachines(session *workflow.Session) *Machines {
	return NewMachinesContext(context.Background(), session)
}
func NewMachinesContext(ctx context.Context, session *workflow.Session) *Machines {
	return &Machines{ctx: ctx, session: session}
}
func (s *Machines) SetStyles(styles components.Styles) {
	s.styles = styles
	if s.browser != nil {
		s.browser.SetStyles(styles)
	}
}

// OwnsWorkspaceKey keeps movement inside the machine subpanels until an outer
// edge is reached, where root focus navigation may take over.
func (s *Machines) OwnsWorkspaceKey(key string) bool {
	switch key {
	case "h", "left":
		return s.focusMappings
	case "l", "right":
		return !s.focusMappings
	case "tab", "j", "down", "k", "up":
		return true
	}
	return key != ":" && key != "?" && key != "q"
}
func (s *Machines) Focus(mapping string) {
	s.focusMapping = mapping
	for i, item := range s.machines() {
		if strings.HasPrefix(mapping, item.Name+":") {
			s.selected, s.machineList.Selected = i, i
			resource := strings.TrimPrefix(mapping, item.Name+":")
			for j, candidate := range s.mappingRows() {
				if candidate.id == resource {
					s.resource = j
					break
				}
			}
			break
		}
	}
}
func (s *Machines) SetSize(width, height int) {
	s.width, s.height = width, height
	if s.browser != nil {
		s.browser.SetSize(width, height)
	}
}
func (s *Machines) Init() tea.Cmd         { return nil }
func (s *Machines) TransientActive() bool { return s.browser != nil || s.confirm != "" || s.mode != "" }

func (s *Machines) Update(msg tea.Msg) tea.Cmd {
	if submitted, ok := msg.(components.TextInputSubmitted); ok {
		s.name = strings.TrimSpace(submitted.Value)
		if s.name == "" {
			return nil
		}
		if s.mode == "add" {
			s.mode = ""
			return s.add()
		}
		if s.mode == "rename" {
			s.mode, s.confirm = "", "rename"
			return s.confirmModal()
		}
	}
	if result, ok := msg.(MachineMutationComplete); ok {
		s.err, s.busy = result.Err, false
		s.machineList.SetSelected(s.machineList.Selected, len(s.machines()), s.listHeight())
		s.selected = s.machineList.Selected
		s.mappingTable.Ensure(s.resource, len(s.mappingRows()), s.tableHeight())
		return nil
	}
	if inspection, ok := msg.(components.BrowserInspectionMsg); ok && s.browser != nil {
		return s.browser.Update(inspection)
	}
	if listing, ok := msg.(components.BrowserReadDirMsg); ok && s.browser != nil {
		return s.browser.Update(listing)
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || s.busy {
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
		switch key.String() {
		case "esc":
			s.mode, s.name = "", ""
		case "enter":
			mode := s.mode
			s.mode = ""
			if mode == "add" {
				return s.add()
			}
			s.confirm = "rename"
			return s.confirmModal()
		case "backspace":
			if len(s.name) > 0 {
				s.name = s.name[:len(s.name)-1]
			}
		default:
			if len(key.String()) == 1 {
				s.name += key.String()
			}
		}
		return nil
	}
	if s.focusMappings {
		s.mappingNavigation.Selected = s.resource
		if s.mappingNavigation.Vim(key.String(), len(s.mappingRows()), s.tableHeight()) {
			s.resource = s.mappingNavigation.Selected
			s.mappingTable.Ensure(s.resource, len(s.mappingRows()), s.tableHeight())
			return nil
		}
	} else if s.machineList.Vim(key.String(), len(s.machines()), s.listHeight()) {
		s.selected = s.machineList.Selected
		s.resource = 0
		return nil
	}
	switch key.String() {
	case "tab":
		s.focusMappings = !s.focusMappings
	case "h", "left":
		s.focusMappings = false
	case "l", "right":
		s.focusMappings = true
	case "j", "down":
		if s.focusMappings {
			s.resource = min(len(s.mappingRows())-1, s.resource+1)
			s.mappingTable.Ensure(s.resource, len(s.mappingRows()), s.tableHeight())
		} else if s.machineList.Move(1, len(s.machines()), s.listHeight()) {
			s.selected = s.machineList.Selected
			s.resource = 0
		}
	case "k", "up":
		if s.focusMappings {
			s.resource = max(0, s.resource-1)
			s.mappingTable.Ensure(s.resource, len(s.mappingRows()), s.tableHeight())
		} else if s.machineList.Move(-1, len(s.machines()), s.listHeight()) {
			s.selected = s.machineList.Selected
			s.resource = 0
		}
	case "a":
		s.mode = "add"
		s.name, _ = s.session.SuggestedMachineName()
		return s.nameModal("Add machine")
	case "u":
		if !s.focusMappings {
			return s.use()
		}
		if s.selectedMapping().override {
			return s.unmapResource()
		}
	case "c":
		if !s.focusMappings {
			return s.clear()
		}
	case "r":
		if !s.focusMappings && s.selectedMachine().Name != "" {
			s.mode, s.name = "rename", s.selectedMachine().Name
			return s.nameModal("Rename machine")
		}
	case "x":
		if !s.focusMappings && s.selectedMachine().Name != "" {
			s.confirm = "remove"
			return s.confirmModal()
		}
	case "m":
		if s.focusMappings && s.selectedMachine().Name != "" && s.selectedMapping().id != "" {
			browser := components.NewBrowser(components.PickDirectory, components.BrowserConfig{Home: s.session.HomeDir(), ProfileDir: s.session.ProfileDir(), Profile: s.session.Profile(), InspectPathCmd: s.inspectPath})
			browser.SetSize(s.width, s.height)
			browser.SetStyles(s.styles)
			s.browser = &browser
			return s.browser.Init()
		}
	}
	return nil
}

func (s *Machines) View() string {
	if s.err != nil {
		return s.styles.Error("Unable to update machine: " + s.err.Error())
	}
	if s.browser != nil {
		return s.browser.View()
	}
	if s.mode != "" {
		return "Machine name: " + s.name
	}
	machineLines := make([]string, 0, len(s.machines()))
	for i, item := range s.machines() {
		marker := " "
		if i == s.machineList.Selected {
			marker = ">"
		}
		active := ""
		if item.Name == s.session.Machine().Name {
			active = " *"
		}
		machineLines = append(machineLines, marker+" "+item.Name+active)
	}
	if len(machineLines) == 0 {
		machineLines = append(machineLines, "No machine overlays.")
	}
	rows := []components.Row{}
	for i, row := range s.mappingRows() {
		rows = append(rows, components.Row{Cells: []string{row.id, row.portable, row.effective, row.source}, Selected: i == s.resource, Focused: s.focusMappings})
	}
	if len(rows) == 0 {
		rows = append(rows, components.Row{Cells: []string{"No resource mappings."}})
	}
	leftWidth := max(20, s.width/3)
	if s.width == 0 {
		leftWidth = 28
	}
	left := s.machineList.View(machineLines, leftWidth, s.listHeight())
	rightWidth := s.width - leftWidth - 2
	if s.width == 0 {
		rightWidth = 80
	}
	right := s.mappingTable.Render([]components.Column{{Title: "Resource", Width: 14, MinWidth: 10}, {Title: "Portable", Width: 20, MinWidth: 12}, {Title: "Effective", Width: 20, MinWidth: 12}, {Title: "Source", MinWidth: 8}}, rows, max(1, rightWidth), s.tableHeight()+1, s.styles)
	return lipgloss.JoinHorizontal(lipgloss.Top, "Machines\n"+left, "  ", "Resource paths\n"+right)
}

func (s *Machines) DetailView() string {
	if s.browser != nil {
		return s.browser.DetailView()
	}
	if s.focusMappings {
		row := s.selectedMapping()
		return "Mapping\nMachine: " + s.selectedMachine().Name + "\nResource: " + row.id + "\nPortable: " + row.portable + "\nEffective: " + row.effective + "\nSource: " + row.source
	}
	item := s.selectedMachine()
	if item.Name == "" {
		return "Machine overlays\nPortable paths are active."
	}
	state := "inactive"
	if item.Name == s.session.Machine().Name {
		state = "active"
	}
	return "Machine: " + item.Name + "\nState: " + state + "\nMappings: " + fmt.Sprint(len(item.ResourcePaths)) + "\nMappings change placement policy only; resource bytes are never moved."
}

type mappingRow struct {
	id, portable, effective, source string
	override                        bool
}

func (s *Machines) mappingRows() []mappingRow {
	current := s.selectedMachine()
	rows := make([]mappingRow, 0, len(s.session.Profile().Resources.Items)+len(current.ResourcePaths))
	for _, resource := range s.session.Profile().Resources.Items {
		row := mappingRow{id: resource.ID, portable: resource.Path, effective: resource.Path, source: "portable"}
		for _, mapping := range current.ResourcePaths {
			if mapping.Resource == resource.ID {
				row.effective, row.source, row.override = mapping.Path, "override", true
				break
			}
		}
		rows = append(rows, row)
	}
	for _, mapping := range current.ResourcePaths {
		if !s.resourceExists(mapping.Resource) {
			rows = append(rows, mappingRow{id: mapping.Resource, portable: "-", effective: mapping.Path, source: "dormant", override: true})
		}
	}
	return rows
}
func (s *Machines) selectedMapping() mappingRow {
	rows := s.mappingRows()
	if s.resource >= 0 && s.resource < len(rows) {
		return rows[s.resource]
	}
	return mappingRow{}
}
func (s *Machines) machines() []profile.Machine { return s.session.Profile().Machines.Items }
func (s *Machines) selectedMachine() profile.Machine {
	items := s.machines()
	if s.selected >= 0 && s.selected < len(items) {
		return items[s.selected]
	}
	return profile.Machine{}
}
func (s *Machines) resourceExists(id string) bool {
	for _, item := range s.session.Profile().Resources.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}
func (s *Machines) listHeight() int {
	if s.height == 0 {
		return len(s.machines()) + 2
	}
	return max(1, s.height-2)
}
func (s *Machines) tableHeight() int {
	if s.height == 0 {
		return len(s.mappingRows()) + 1
	}
	return max(1, s.height-3)
}
func (s *Machines) confirmModal() tea.Cmd {
	prompt := "Rename machine to " + s.name + "?"
	if s.confirm == "remove" {
		prompt = "Remove " + s.selectedMachine().Name + "? Resource files remain untouched."
	}
	return func() tea.Msg { return components.ModalRequest{Title: "Machines", Content: components.Confirm(prompt)} }
}
func (s *Machines) nameModal(title string) tea.Cmd {
	return func() tea.Msg {
		return components.ModalRequest{Title: title, Content: "Enter a machine overlay name.", Input: s.name, Placeholder: "Machine name"}
	}
}
func (s *Machines) inspectPath(requestID uint64, path string) tea.Cmd {
	return func() tea.Msg {
		inspection, err := s.session.InspectPath(s.ctx, path)
		return components.BrowserInspectionMsg{RequestID: requestID, Inspection: inspection, Err: err}
	}
}
func (s *Machines) mutate(notice string, run func() error) tea.Cmd {
	s.busy = true
	return func() tea.Msg { return MachineMutationComplete{Notice: notice, Err: run()} }
}
func (s *Machines) add() tea.Cmd {
	name := s.name
	return s.mutate("Machine added.", func() error { return s.session.AddMachine(s.ctx, name, true) })
}
func (s *Machines) use() tea.Cmd {
	return s.mutate("Machine selected.", func() error { return s.session.UseMachine(s.ctx, s.selectedMachine().Name) })
}
func (s *Machines) clear() tea.Cmd {
	return s.mutate("Using portable resource paths.", func() error { return s.session.ClearMachine(s.ctx) })
}
func (s *Machines) rename() tea.Cmd {
	old, name := s.selectedMachine().Name, s.name
	return s.mutate("Machine renamed.", func() error { return s.session.RenameMachine(s.ctx, old, name) })
}
func (s *Machines) remove() tea.Cmd {
	return s.mutate("Machine removed.", func() error { return s.session.RemoveMachine(s.ctx, s.selectedMachine().Name) })
}
func (s *Machines) mapResource(path string) tea.Cmd {
	return s.mutate("Resource mapping updated.", func() error {
		return s.session.MapResource(s.ctx, s.selectedMachine().Name, s.selectedMapping().id, path)
	})
}
func (s *Machines) unmapResource() tea.Cmd {
	return s.mutate("Resource mapping removed.", func() error {
		return s.session.UnmapResource(s.ctx, s.selectedMachine().Name, s.selectedMapping().id)
	})
}
