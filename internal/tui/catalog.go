package tui

import "github.com/Grenco/omarchy-blueprint/internal/tui/components"

type ScreenSection string

const (
	SectionWorkflow      ScreenSection = "Workflow"
	SectionSoftware      ScreenSection = "Software"
	SectionCustomisation ScreenSection = "Customisation"
	SectionProfile       ScreenSection = "Profile"
)

type ScreenInfo struct {
	ID       ScreenID
	Label    string
	Section  ScreenSection
	Short    string
	Long     string
	Keywords string
}

var screenCatalog = []ScreenInfo{
	{ScreenOverview, "Overview", "", "See what differs, what needs attention, and where to go next.", "Overview is your starting point. It groups what differs between this machine and your Blueprint profile: Needs attention for blocked or unresolved items, Changes available for differences Capture or Restore would act on, and Intentional differences that your Capture and Restore policy deliberately leaves alone on this machine. Open an item to jump to the screen where you can review or act on it.", "home summary attention warning difference drift"},
	{ScreenCapture, "Capture", SectionWorkflow, "Update your Blueprint profile from the system you're using now.", "Capture reads the current system and saves selected categories into your profile. Use it after you intentionally change your setup and want Blueprint to remember those changes. Choose categories, then review the proposed profile changes grouped as Changes, Preserved by policy, and Blocked. You can switch a target between Include and Preserve before approving; Capture re-checks the system and your policy before it writes anything.", "save remember update profile"},
	{ScreenRestore, "Restore", SectionWorkflow, "Preview and apply what your Blueprint profile would recreate on this machine.", "Restore shows one current plan using independent conflict and convergence settings. Safe preserves supported conflicts; Force can resolve them in favour of captured intent. Additive creates or updates desired state, while Exact can also remove Blueprint-managed extras. One-run overrides do not change machine defaults.", "rebuild recreate apply recover exact additive safe force"},
	{ScreenPackages, "Packages", SectionSoftware, "Packages and tools Blueprint expects to be installed.", "Packages shows captured system packages, AUR packages, and global Mise tools, plus differences on this machine. Capture and Restore policy decide, for the profile or a single machine, which packages Blueprint records and which it reinstalls, so a package can stay on one computer without being restored everywhere.", "software apps aur pacman mise tools install installed"},
	{ScreenThemes, "Themes", SectionSoftware, "Themes Blueprint remembers, including your selected theme.", "Themes shows the themes saved in your profile and the active theme Blueprint observed. Built-in themes are referenced by name, installed themes retain their source and revision when possible, and local or modified themes are copied into the profile.", "appearance theme installed"},
	{ScreenPlugins, "Plugins", SectionSoftware, "Omarchy plugins Blueprint remembers and can reconstruct.", "Plugins shows plugin code and sources saved in the profile. Built-in plugins are referenced by name, installed plugins retain their source when possible, and local or modified plugins are copied. If Shell state is captured, Shell controls plugin enablement while this screen describes how the plugin itself is reconstructed.", "extensions plugin installed"},
	{ScreenDefaults, "Defaults", SectionSoftware, "Your chosen default terminal, browser, editor, and agent.", "Defaults records the applications Omarchy considers your terminal, browser, editor, and agent. Restore can set supported defaults through Omarchy, but installing those applications belongs to Packages. Agent selection is remembered and checked but is not automatically changed.", "terminal browser editor agent default apps applications"},
	{ScreenConfig, "Config", SectionCustomisation, "Review dotfiles and other system/app configuration Blueprint can remember and restore.", "Config is about your dotfiles and other application/system configuration — primarily files under `~/.config`, plus supported home dotfiles — not settings for Blueprint itself. Blueprint compares them with Omarchy's defaults so it can save meaningful customisations without copying everything. State tells you what Blueprint found; Policy tells you whether Blueprint should decide automatically, include the path, or leave it out. Some configuration is handled by more specific Blueprint features and is shown as Handled elsewhere so it is not captured twice.", "dotfiles dot files configuration settings .config"},
	{ScreenShell, "Shell", SectionCustomisation, "Your customised Omarchy shell, bar, and layout state.", "Shell captures supported Omarchy shell customisations relative to the normal Omarchy setup. Restore preserves unrelated changes where possible and reports overlapping changes as conflicts instead of silently replacing them.", "shell bar waybar layout widgets"},
	{ScreenHooks, "Hooks", SectionCustomisation, "Scripts Omarchy runs automatically at supported hook points.", "Hooks are scripts Omarchy runs when particular supported events occur. This screen shows which hook files Blueprint has saved and whether this machine differs. Hook source is stored as written, so credentials and secrets should stay outside the scripts.", "scripts events automation"},
	{ScreenResources, "Resources", SectionCustomisation, "Files, folders, and Git projects you explicitly asked Blueprint to carry.", "Resources are things outside Blueprint's normal categories that you deliberately want to reconstruct. They can be copied, rebuilt from Git, or rebuilt from Git plus selected local changes. A Resource can also have a different path on different machines.", "files folders directories projects git copy"},
	{ScreenMachines, "Machines", SectionProfile, "Per-machine restore defaults, policy overrides, and Resource paths.", "Machines lets one computer differ from the portable profile without changing it: its own Safe/Force and Additive/Exact restore defaults, Capture and Restore policy overrides, and Resource paths for Resources that cannot use one shared location. Select a machine to use its overlay, or leave the portable defaults active when no override is needed.", "computer laptop desktop host paths mappings"},
	{ScreenSync, "Sync", SectionProfile, "Git status and safe sync actions for the Blueprint profile itself.", "Sync manages the Git repository containing your Blueprint profile; it does not manage your application or Resource repositories. It handles the common safe workflow for committing, fetching, pulling, and pushing Blueprint-managed files. Use Git or LazyGit for more advanced repository work.", "git commit push pull fetch remote profile repository"},
}

func screenInfo(id ScreenID) ScreenInfo {
	for _, info := range screenCatalog {
		if info.ID == id {
			return info
		}
	}
	return ScreenInfo{ID: id}
}

func orderedScreenIDs() []ScreenID {
	ids := make([]ScreenID, len(screenCatalog))
	for i, info := range screenCatalog {
		ids[i] = info.ID
	}
	return ids
}

func sidebarSections() []components.NavSection {
	sections := make([]components.NavSection, 0, 5)
	for _, info := range screenCatalog {
		if len(sections) == 0 || ScreenSection(sections[len(sections)-1].Label) != info.Section {
			sections = append(sections, components.NavSection{Label: string(info.Section)})
		}
		last := len(sections) - 1
		sections[last].Items = append(sections[last].Items, components.NavItem{ID: string(info.ID), Label: info.Label})
	}
	return sections
}

func moveSidebarSection(current ScreenID, delta int) ScreenID {
	sections := sidebarSections()
	for index, section := range sections {
		for _, item := range section.Items {
			if item.ID != string(current) {
				continue
			}
			destination := index + delta
			if destination < 0 || destination >= len(sections) {
				return current
			}
			return ScreenID(sections[destination].Items[0].ID)
		}
	}
	return current
}
