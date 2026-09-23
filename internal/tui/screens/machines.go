package screens

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
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
	policyCategory          int
	focusMapping            string
	region                  machineRegion
	machineList             components.Selectable
	machineTable            components.Table
	policyTable             components.Table
	mappingNavigation       components.Selectable
	mappingTable            components.Table
	styles                  components.Styles
	busy                    bool
	err                     error
}

type machineRegion uint8

const (
	machineRegionMachines machineRegion = iota
	machineRegionPolicy
	machineRegionResources
)

type MachineMutationComplete struct {
	Notice string
	Err    error
}

// AuthorityChanged tells the root that persisted machine or policy intent
// changed and every already-visited authority-dependent screen must refresh.
type AuthorityChanged struct{ Notice string }

// PolicyNavigation asks the root to open a category's policy controls.
type PolicyNavigation struct {
	Category string
	Machine  string
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
	case "tab", "j", "down", "k", "up":
		return true
	case "h", "left", "l", "right":
		return false
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
func (s *Machines) BrowserActive() bool   { return s.browser != nil }
func (s *Machines) HasSelectedMachine() bool {
	return s.region == machineRegionMachines && s.selectedMachine().Name != ""
}
func (s *Machines) CanAddMachine() bool { return s.region == machineRegionMachines }

// machineRowCount includes the trailing "+ Add machine" row, which is
// reachable with j/k like any machine but is never a machine itself.
func (s *Machines) machineRowCount() int { return len(s.machines()) + 1 }
func (s *Machines) AddRowSelected() bool {
	return s.region == machineRegionMachines && s.selected == len(s.machines())
}
func (s *Machines) startAdd() tea.Cmd {
	s.mode = "add"
	s.name, _ = s.session.SuggestedMachineName()
	return s.nameModal("Add machine")
}
func (s *Machines) CanUseMachine() bool {
	return s.HasSelectedMachine() && s.selectedMachine().Name != s.session.Machine().Name
}
func (s *Machines) CanClearMachine() bool {
	return s.region == machineRegionMachines && s.session.Machine().Name != ""
}
func (s *Machines) CanMapResource() bool {
	return s.region == machineRegionResources && s.selectedMachine().Name != "" && s.selectedMapping().id != ""
}
func (s *Machines) CanUnmapResource() bool { return s.CanMapResource() && s.selectedMapping().override }
func (s *Machines) MappingFocused() bool   { return s.region == machineRegionResources }
func (s *Machines) CanOpenPolicy() bool {
	return s.region == machineRegionPolicy && len(machinePolicyCategories(s.selectedMachine().Policy)) > 0
}
func (s *Machines) CanCyclePolicy() bool {
	return s.region == machineRegionPolicy && len(machinePolicyCategories(s.selectedMachine().Policy)) > 1
}

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
		s.machineList.SetSelected(s.machineList.Selected, s.machineRowCount(), s.listHeight())
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
	if listing, ok := msg.(components.BrowserChildReadDirMsg); ok && s.browser != nil {
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
	if s.region == machineRegionResources {
		s.mappingNavigation.Selected = s.resource
		if s.mappingNavigation.Vim(key.String(), len(s.mappingRows()), s.tableHeight()) {
			s.resource = s.mappingNavigation.Selected
			s.mappingTable.Ensure(s.resource, len(s.mappingRows()), s.tableHeight())
			return nil
		}
	} else if s.region == machineRegionMachines && s.machineList.Vim(key.String(), s.machineRowCount(), s.listHeight()) {
		s.selected = s.machineList.Selected
		s.resource = 0
		return nil
	} else if s.region == machineRegionPolicy && (key.String() == "j" || key.String() == "down" || key.String() == "k" || key.String() == "up") {
		s.movePolicyCategory(key.String() == "j" || key.String() == "down")
		return nil
	}
	switch key.String() {
	case "tab":
		s.nextRegion()
	case "j", "down":
		if s.region == machineRegionResources {
			s.resource = min(len(s.mappingRows())-1, s.resource+1)
			s.mappingTable.Ensure(s.resource, len(s.mappingRows()), s.tableHeight())
		} else if s.region == machineRegionMachines && s.machineList.Move(1, s.machineRowCount(), s.listHeight()) {
			s.selected = s.machineList.Selected
			s.resource = 0
		}
	case "k", "up":
		if s.region == machineRegionResources {
			s.resource = max(0, s.resource-1)
			s.mappingTable.Ensure(s.resource, len(s.mappingRows()), s.tableHeight())
		} else if s.region == machineRegionMachines && s.machineList.Move(-1, s.machineRowCount(), s.listHeight()) {
			s.selected = s.machineList.Selected
			s.resource = 0
		}
	case "a":
		if s.CanAddMachine() {
			return s.startAdd()
		}
	case "u":
		switch s.region {
		case machineRegionMachines:
			if s.CanUseMachine() {
				return s.use()
			}
		case machineRegionResources:
			if s.selectedMapping().override {
				return s.unmapResource()
			}
		}
	case "c":
		if s.region == machineRegionMachines {
			return s.clear()
		}
	case "r":
		if s.region == machineRegionMachines && s.selectedMachine().Name != "" {
			s.mode, s.name = "rename", s.selectedMachine().Name
			return s.nameModal("Rename machine")
		}
	case "x":
		if s.region == machineRegionMachines && s.selectedMachine().Name != "" {
			s.confirm = "remove"
			return s.confirmModal()
		}
	case "m":
		if s.region == machineRegionResources && s.selectedMachine().Name != "" && s.selectedMapping().id != "" {
			browser := components.NewBrowser(components.PickDirectory, components.BrowserConfig{Home: s.session.HomeDir(), ProfileDir: s.session.ProfileDir(), Profile: s.session.Profile(), InspectPathCmd: s.inspectPath})
			browser.SetSize(s.width, s.height)
			browser.SetStyles(s.styles)
			s.browser = &browser
			return s.browser.Init()
		}
	case "f":
		if s.region == machineRegionMachines && s.selectedMachine().Name != "" {
			return s.toggleRestoreConflict()
		}
	case "e":
		if s.region == machineRegionMachines && s.selectedMachine().Name != "" {
			return s.toggleRestoreConvergence()
		}
	case "[", "]":
		if s.region == machineRegionPolicy {
			s.movePolicyCategory(key.String() == "]")
		}
	case "o":
		fallthrough
	case "enter":
		if s.AddRowSelected() && key.String() == "enter" {
			return s.startAdd()
		}
		categories := machinePolicyCategories(s.selectedMachine().Policy)
		if s.region == machineRegionPolicy && len(categories) > 0 {
			category := categories[min(s.policyCategory, len(categories)-1)].category
			machine := s.selectedMachine().Name
			return func() tea.Msg { return PolicyNavigation{Category: category, Machine: machine} }
		}
	}
	return nil
}

func (s *Machines) View() string {
	if s.err != nil {
		return s.styles.Error("Unable to update machine: " + components.DisplayText(s.err.Error()))
	}
	if s.browser != nil {
		return s.browser.View()
	}
	if s.mode != "" {
		return "Machine name: " + components.DisplayText(s.name)
	}
	if s.session != nil && len(s.machines()) == 0 && len(s.session.Profile().Resources.Items) == 0 {
		return renderEmptyState(s.styles, s.width, emptyStateCopy{
			Heading:     "No machine-specific paths needed",
			Explanation: "Machines only matters when a Resource needs a different location on one computer. There are no Resources here that need mapping yet.",
			Guidance:    "There is nothing to configure on this screen.",
		})
	}
	machineRows := make([]components.Row, 0, len(s.machines()))
	for i, item := range s.machines() {
		selected := i == s.machineList.Selected && s.region == machineRegionMachines
		active := "No"
		if item.Name == s.session.Machine().Name {
			active = "Yes"
		}
		options := item.EffectiveRestoreDefaults()
		defaults := restoreDefaultsLabel(options)
		if !selected {
			if options.Conflicts == policy.ConflictForce || options.Convergence == policy.ConvergenceExact {
				defaults = s.styles.Warning(defaults)
			} else {
				defaults = s.styles.Muted(defaults)
			}
		}
		overrides := len(item.Policy.Capture) + len(item.Policy.Restore)
		overrideLabel := fmt.Sprint(overrides)
		if !selected {
			if overrides > 0 {
				overrideLabel = s.styles.Accent(overrideLabel)
			} else {
				overrideLabel = s.styles.Muted(overrideLabel)
			}
		}
		machineRows = append(machineRows, components.Row{Cells: []string{components.DisplayText(item.Name), styledDecision(s.styles, active, selected), defaults, overrideLabel}, Selected: selected, Focused: s.region == machineRegionMachines})
	}
	addSelected := s.AddRowSelected()
	addLabel := "+ Add machine"
	if !addSelected {
		addLabel = s.styles.Accent(addLabel)
	}
	machineRows = append(machineRows, components.Row{Cells: []string{addLabel}, Selected: addSelected, Focused: s.region == machineRegionMachines})
	rows := []components.Row{}
	for i, row := range s.mappingRows() {
		selected := i == s.resource && s.region == machineRegionResources
		source := components.DisplayText(row.source)
		if !selected {
			if row.source == "dormant" {
				source = s.styles.Warning(source)
			} else if row.source == "override" {
				source = s.styles.Accent(source)
			} else {
				source = s.styles.Muted(source)
			}
		}
		rows = append(rows, components.Row{Cells: []string{components.DisplayText(row.id), components.DisplayText(row.portable), components.DisplayText(row.effective), source}, Selected: selected, Focused: s.region == machineRegionResources})
	}
	if len(rows) == 0 {
		rows = append(rows, components.Row{Cells: []string{"No resource mappings."}})
	}
	width := s.width
	if width <= 0 {
		width = 120
	}
	machineHeight := s.machineRenderHeight()
	s.machineTable.Ensure(s.machineList.Selected, len(machineRows), max(1, machineHeight-1))
	machines := s.machineTable.Render([]components.Column{{Title: "MACHINE", Width: 30, MinWidth: 12}, {Title: "ACTIVE", Width: 9, MinWidth: 6}, {Title: "RESTORE DEFAULT", Width: 22, MinWidth: 15}, {Title: "OVERRIDES", MinWidth: 9}}, machineRows, width, machineHeight, s.styles)
	parts := []string{components.SectionDivider("Machines", width, s.styles), machines}
	if categories := s.policyCategoriesView(width); categories != "" {
		parts = append(parts, categories)
	}
	right := s.mappingTable.Render([]components.Column{{Title: "RESOURCE", Width: 18, MinWidth: 10}, {Title: "PORTABLE", Width: 28, MinWidth: 12}, {Title: "EFFECTIVE", Width: 28, MinWidth: 12}, {Title: "SOURCE", MinWidth: 8}}, rows, width, s.tableHeight()+1, s.styles)
	parts = append(parts, components.SectionDivider("Resource paths", width, s.styles), right)
	separator := "\n\n"
	// The portable-path guidance can consume much of the screen even when the
	// outer height looks generous. Compact the section gaps according to the
	// space that remains for the tables, so the Resource paths region is not
	// pushed below an 80-column viewport.
	if s.contentHeight() > 0 && s.contentHeight() < 18 {
		separator = "\n"
	}
	existingView := strings.Join(parts, separator)
	if guidance, ok := s.portableGuidance(); ok {
		return guidance + "\n\n" + existingView
	}
	return existingView
}

func (s *Machines) policyCategoriesView(width int) string {
	machine := s.selectedMachine()
	categories := machinePolicyCategories(machine.Policy)
	if len(categories) == 0 {
		return ""
	}
	rows := make([]components.Row, 0, len(categories))
	for i, item := range categories {
		rows = append(rows, components.Row{Cells: []string{components.DisplayText(item.category), fmt.Sprint(item.count)}, Selected: i == min(s.policyCategory, len(categories)-1) && s.region == machineRegionPolicy, Focused: s.region == machineRegionPolicy})
	}
	height := len(rows) + 1
	return components.SectionDivider("Policy overrides", width, s.styles) + "\n" +
		s.styles.SubtleAccent("Select a category and press Enter to review or edit.") + "\n" +
		s.policyTable.Render([]components.Column{{Title: "CATEGORY", Width: 32, MinWidth: 12}, {Title: "OVERRIDES", MinWidth: 9}}, rows, width, height, s.styles)
}

func (s *Machines) movePolicyCategory(forward bool) {
	categories := machinePolicyCategories(s.selectedMachine().Policy)
	if len(categories) == 0 {
		return
	}
	delta := -1
	if forward {
		delta = 1
	}
	s.policyCategory = (s.policyCategory + delta + len(categories)) % len(categories)
}

func (s *Machines) nextRegion() {
	regions := []machineRegion{machineRegionMachines}
	if len(machinePolicyCategories(s.selectedMachine().Policy)) > 0 {
		regions = append(regions, machineRegionPolicy)
	}
	if len(s.mappingRows()) > 0 {
		regions = append(regions, machineRegionResources)
	}
	for i, region := range regions {
		if region == s.region {
			s.region = regions[(i+1)%len(regions)]
			return
		}
	}
	s.region = regions[0]
}

func restoreDefaultsLabel(options policy.RestoreOptions) string {
	return titleMode(string(options.Conflicts)) + " / " + titleMode(string(options.Convergence))
}

func titleMode(value string) string {
	if value == "" {
		return "Default"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

type machinePolicyCategory struct {
	category string
	count    int
}

func machinePolicyCategories(rules policy.Rules) []machinePolicyCategory {
	counts := map[string]int{}
	for _, rule := range append(append([]policy.Rule(nil), rules.Capture...), rules.Restore...) {
		counts[rule.Category]++
	}
	categories := make([]machinePolicyCategory, 0, len(counts))
	for category, count := range counts {
		categories = append(categories, machinePolicyCategory{category: category, count: count})
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i].category < categories[j].category })
	return categories
}
func (s *Machines) toggleRestoreConflict() tea.Cmd {
	machine := s.selectedMachine()
	options := machine.EffectiveRestoreDefaults()
	if options.Conflicts == policy.ConflictSafe {
		options.Conflicts = policy.ConflictForce
	} else {
		options.Conflicts = policy.ConflictSafe
	}
	return s.mutate("Restore conflict default updated.", func() error { return s.session.SetMachineRestoreDefaults(machine.Name, options) })
}
func (s *Machines) toggleRestoreConvergence() tea.Cmd {
	machine := s.selectedMachine()
	options := machine.EffectiveRestoreDefaults()
	if options.Convergence == policy.ConvergenceAdditive {
		options.Convergence = policy.ConvergenceExact
	} else {
		options.Convergence = policy.ConvergenceAdditive
	}
	return s.mutate("Restore convergence default updated.", func() error { return s.session.SetMachineRestoreDefaults(machine.Name, options) })
}

// hasOverrides reports whether any machine has a saved Resource mapping,
// including a dormant one referring to a Resource that no longer exists.
// Those mappings are meaningful saved state and must never be hidden behind
// "Portable paths are in use" guidance.
func (s *Machines) hasOverrides() bool {
	if s.session == nil {
		return true
	}
	for _, machine := range s.session.Profile().Machines.Items {
		if len(machine.ResourcePaths) > 0 {
			return true
		}
	}
	return false
}

// portableGuidance returns the "Portable paths are in use" note and whether
// it should be shown; it never replaces the overlay/mapping panes, only
// supplements them.
// minMachinesPaneHeight is the smallest overlay/mapping pane height Machines
// tries to preserve before compacting the portable-paths guidance down to a
// heading-only line; the panes are never hidden below this to preserve copy.
const minMachinesPaneHeight = 5

func (s *Machines) portableGuidance() (string, bool) {
	if s.hasOverrides() {
		return "", false
	}
	full := renderEmptyState(s.styles, s.width, emptyStateCopy{
		Heading:     "Portable paths are in use",
		Explanation: "Resources normally use the same portable path on every machine.",
		Guidance:    "Add a machine-specific mapping only when one computer needs a different location. If the normal Resource paths work here, there is nothing to configure.",
	})
	if s.height <= 0 || s.height-machinesGuidanceBlockHeight(full) >= minMachinesPaneHeight {
		return full, true
	}
	compact := renderEmptyState(s.styles, s.width, emptyStateCopy{
		Heading: "Portable paths are in use",
	})
	return compact, true
}

func machinesGuidanceBlockHeight(guidance string) int {
	// +1 for the blank separator line prepended before the two-pane view.
	return strings.Count(guidance, "\n") + 1 + 1
}

// contentHeight is the height budget available to the two-pane
// overlay/mapping view once the "Portable paths are in use" guidance, when
// shown, has taken its share of the granted height.
func (s *Machines) contentHeight() int {
	if s.height <= 0 {
		return 0
	}
	if guidance, ok := s.portableGuidance(); ok {
		return max(1, s.height-machinesGuidanceBlockHeight(guidance))
	}
	return s.height
}

func (s *Machines) DetailView() string {
	if s.browser != nil {
		return s.browser.DetailView()
	}
	if s.region == machineRegionResources {
		row := s.selectedMapping()
		return "Mapping\nMachine: " + components.DisplayText(s.selectedMachine().Name) + "\nResource: " + components.DisplayText(row.id) + "\nPortable: " + components.DisplayText(row.portable) + "\nEffective: " + components.DisplayText(row.effective) + "\nSource: " + components.DisplayText(row.source)
	}
	if s.region == machineRegionPolicy {
		categories := machinePolicyCategories(s.selectedMachine().Policy)
		if len(categories) == 0 {
			return "Policy overrides\nNo explicit Capture or Restore overrides."
		}
		selected := categories[min(s.policyCategory, len(categories)-1)]
		return "Policy overrides\nCategory: " + components.DisplayText(selected.category) + "\nOverrides: " + fmt.Sprint(selected.count) + "\nThese categories contain explicit Capture or Restore overrides for the selected machine. Open one to review its targets."
	}
	if s.AddRowSelected() {
		return "Add machine\nCreate a machine overlay for a computer whose Resources need different paths.\n\nEnter opens the name prompt."
	}
	item := s.selectedMachine()
	if item.Name == "" {
		return "Machine overlays\nPortable paths are active."
	}
	state := "inactive"
	if item.Name == s.session.Machine().Name {
		state = "active"
	}
	return "Machine: " + components.DisplayText(item.Name) + "\nState: " + state + "\nMappings: " + fmt.Sprint(len(item.ResourcePaths)) + "\nMappings change placement policy only; resource bytes are never moved."
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
func (s *Machines) machines() []profile.Machine {
	if s.session == nil {
		return nil
	}
	return s.session.Profile().Machines.Items
}
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
	return max(1, s.machineRenderHeight()-1)
}
func (s *Machines) tableHeight() int {
	return max(1, s.mappingRenderHeight()-1)
}
func (s *Machines) machineRenderHeight() int {
	height := s.contentHeight()
	if height <= 0 {
		return max(2, s.machineRowCount()+1)
	}
	reserved := 3 + s.policyBlockHeight()
	return max(2, min(s.machineRowCount()+1, max(2, height-reserved)))
}
func (s *Machines) policyBlockHeight() int {
	categories := machinePolicyCategories(s.selectedMachine().Policy)
	if len(categories) == 0 {
		return 0
	}
	return len(categories) + 3
}
func (s *Machines) mappingRenderHeight() int {
	height := s.contentHeight()
	if height <= 0 {
		return max(2, len(s.mappingRows())+1)
	}
	return max(2, height-2-s.machineRenderHeight()-s.policyBlockHeight())
}
func (s *Machines) confirmModal() tea.Cmd {
	prompt := "Rename \"" + components.DisplayText(s.selectedMachine().Name) + "\" to \"" + components.DisplayText(s.name) + "\"?"
	if s.confirm == "remove" {
		prompt = "Remove machine \"" + components.DisplayText(s.selectedMachine().Name) + "\"?\n\nThis removes the machine overlay and its path mappings.\nLive Resource files will not be moved or deleted."
	}
	return func() tea.Msg {
		return components.ModalRequest{Title: "Confirm machine change", Content: components.Confirm(prompt)}
	}
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
