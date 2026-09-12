package tui

type LayoutMode string

const (
	LayoutThreePane LayoutMode = "three-pane"
	LayoutTwoPane   LayoutMode = "two-pane"
	LayoutCompact   LayoutMode = "compact"
	LayoutTooSmall  LayoutMode = "too-small"
)

type layout struct {
	mode                         LayoutMode
	sidebarWidth, workspaceWidth int
	detailsWidth, contentHeight  int
}

func layoutForSize(width, height int) layout {
	if width < 70 || height < 18 {
		return layout{mode: LayoutTooSmall}
	}
	contentHeight := max(0, height-4) // Header, separator, footer, and its separator.
	if width >= 120 {
		sidebar := 22
		details := max(28, width/4)
		return layout{mode: LayoutThreePane, sidebarWidth: sidebar, workspaceWidth: max(0, width-sidebar-details-2), detailsWidth: details, contentHeight: contentHeight}
	}
	if width >= 90 {
		sidebar := 22
		return layout{mode: LayoutTwoPane, sidebarWidth: sidebar, workspaceWidth: max(0, width-sidebar-1), contentHeight: contentHeight}
	}
	return layout{mode: LayoutCompact, workspaceWidth: width, contentHeight: contentHeight}
}
