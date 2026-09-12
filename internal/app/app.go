package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	packagesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	pluginsprovider "github.com/Grenco/omarchy-blueprint/internal/providers/plugins"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	themesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/themes"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
	"github.com/Grenco/omarchy-blueprint/internal/tui"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type Dependencies struct {
	Runner            command.Runner
	In                io.Reader
	Out               io.Writer
	Err               io.Writer
	Now               func() time.Time
	StateHome         func() (string, error)
	ThemeDirs         func() (builtin, user string, err error)
	PluginDir         func() (string, error)
	ConfigDirs        func() (baseline, user string, err error)
	BaselineHistory   func() configprovider.BaselineHistory
	ShellPaths        func() (baseline, user string, err error)
	HooksDir          func() (string, error)
	MiseGlobalConfig  func() (string, error)
	HomeDir           func() (string, error)
	Hostname          func() (string, error)
	ResourceLinkRoots func(string) []resourcesprovider.LinkSearchRoot
	IsTTY             func() bool
	RunTUI            func(context.Context, tui.Options, tui.Dependencies) error
}

type options struct {
	profileDir string
	json       bool
	machine    string
}

type driftError struct{}

func (driftError) Error() string { return "profile drift detected" }

func Execute(ctx context.Context, args []string, deps Dependencies) int {
	if deps.Runner == nil {
		deps.Runner = command.SystemRunner{}
	}
	if deps.In == nil {
		deps.In = os.Stdin
	}
	if deps.Out == nil {
		deps.Out = os.Stdout
	}
	if deps.Err == nil {
		deps.Err = os.Stderr
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.StateHome == nil {
		deps.StateHome = restore.StateHome
	}
	if deps.ThemeDirs == nil {
		deps.ThemeDirs = defaultThemeDirs
	}
	if deps.PluginDir == nil {
		deps.PluginDir = defaultPluginDir
	}
	if deps.ConfigDirs == nil {
		deps.ConfigDirs = defaultConfigDirs
	}
	if deps.ShellPaths == nil {
		deps.ShellPaths = defaultShellPaths
	}
	if deps.HooksDir == nil {
		deps.HooksDir = defaultHooksDir
	}
	if deps.MiseGlobalConfig == nil {
		deps.MiseGlobalConfig = packagesprovider.ResolveMiseGlobalConfigPath
	}
	if deps.HomeDir == nil {
		deps.HomeDir = os.UserHomeDir
	}
	if deps.Hostname == nil {
		deps.Hostname = os.Hostname
	}
	if deps.ResourceLinkRoots == nil {
		deps.ResourceLinkRoots = resourcesprovider.DefaultLinkSearchRoots
	}
	if deps.IsTTY == nil {
		deps.IsTTY = defaultIsTTY
	}
	if deps.RunTUI == nil {
		deps.RunTUI = tui.Run
	}
	root := newRoot(deps)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var drift driftError
	if errors.As(err, &drift) {
		return 2
	}
	fmt.Fprintln(deps.Err, "Error:", err)
	return 1
}

func newRoot(deps Dependencies) *cobra.Command {
	opt := &options{}
	root := &cobra.Command{Use: "omarchy-blueprint", Short: "Capture and restore portable Omarchy state", SilenceErrors: true, SilenceUsage: true, PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		profileDir, err := machine.CanonicalProfileRoot(opt.profileDir)
		if err != nil {
			return err
		}
		opt.profileDir = profileDir
		return nil
	}, RunE: func(cmd *cobra.Command, _ []string) error {
		if !deps.IsTTY() {
			return cmd.Help()
		}
		return deps.RunTUI(cmd.Context(), tui.Options{ProfileDir: opt.profileDir, Machine: opt.machine}, tui.Dependencies{Workflow: workflowDependencies(deps), OpenSession: func(_ workflow.Options) (*workflow.Session, error) { return openWorkflow(deps, opt) }, CreateProfile: func(ctx context.Context, dir, name string) (*workflow.Session, error) {
			if _, err := workflow.CreateProfile(ctx, workflowDependencies(deps), dir, name); err != nil {
				return nil, err
			}
			return openWorkflow(deps, opt)
		}})
	}}
	root.PersistentFlags().StringVar(&opt.profileDir, "profile", ".", "profile directory")
	root.PersistentFlags().BoolVar(&opt.json, "json", false, "emit machine-readable JSON")
	root.PersistentFlags().StringVar(&opt.machine, "machine", "", "use machine overlay for this invocation")
	root.AddCommand(initCommand(deps, opt), captureCommand(deps, opt), statusCommand(deps, opt, false), statusCommand(deps, opt, true), restoreCommand(deps, opt), checkCommand(deps, opt), trackCommand(deps, opt), untrackCommand(deps, opt), trackedCommand(deps, opt), inspectCommand(deps, opt), packagePolicyCommand(deps, opt, true), packagePolicyCommand(deps, opt, false), configAutoCommand(deps, opt), machineCommand(deps, opt), profileCommand(deps, opt), tuiCommand(deps, opt))
	root.SetOut(deps.Out)
	return root
}

func defaultIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func tuiCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "tui", Args: cobra.NoArgs, Short: "Open the interactive interface", RunE: func(cmd *cobra.Command, _ []string) error {
		if !deps.IsTTY() {
			return errors.New("tui requires an interactive terminal")
		}
		return deps.RunTUI(cmd.Context(), tui.Options{ProfileDir: opt.profileDir, Machine: opt.machine}, tui.Dependencies{Workflow: workflowDependencies(deps), OpenSession: func(_ workflow.Options) (*workflow.Session, error) { return openWorkflow(deps, opt) }, CreateProfile: func(ctx context.Context, dir, name string) (*workflow.Session, error) {
			if _, err := workflow.CreateProfile(ctx, workflowDependencies(deps), dir, name); err != nil {
				return nil, err
			}
			return openWorkflow(deps, opt)
		}})
	}}
}

type machineOutput struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// resourceOutput supplements portable profile state with the live path used
// for this invocation. It is never written back to resources.toml.
type resourceOutput struct {
	profile.Resource
	DefaultPath   string `json:"default_path"`
	EffectivePath string `json:"effective_path"`
	Machine       string `json:"machine,omitempty"`
}

type resourcesOutput struct {
	Items        []resourceOutput       `json:"items"`
	Links        []profile.ResourceLink `json:"links"`
	IgnoredLinks []string               `json:"ignored_links"`
}

type machineContext struct {
	Selection machine.Selection
	Home      string
	Roots     map[string]string
	Dormant   []string
}

func machineCommand(deps Dependencies, opt *options) *cobra.Command {
	command := &cobra.Command{Use: "machine", Short: "Manage machine resource path overlays"}
	command.AddCommand(machineAddCommand(deps, opt), machineListCommand(deps, opt), machineCurrentCommand(deps, opt), machineUseCommand(deps, opt), machineClearCommand(deps, opt), machineRenameCommand(deps, opt), machineRemoveCommand(deps, opt), machineMapCommand(deps, opt), machineUnmapCommand(deps, opt))
	return command
}

func machineAddCommand(deps Dependencies, opt *options) *cobra.Command {
	var noUse bool
	cmd := &cobra.Command{Use: "add [name]", Args: cobra.MaximumNArgs(1), Short: "Add a machine overlay", RunE: func(_ *cobra.Command, args []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		name := ""
		if len(args) == 1 {
			name = args[0]
		} else {
			if opt.json {
				return errors.New("machine add requires an explicit name with --json")
			}
			hostname, err := deps.Hostname()
			if err != nil {
				return err
			}
			suggestion := machine.SuggestName(hostname, d.Machines.Items)
			fmt.Fprintf(deps.Out, "Machine name [%s]: ", suggestion)
			line, err := bufio.NewReader(deps.In).ReadString('\n')
			if err != nil && !(errors.Is(err, io.EOF) && line != "") {
				return errors.New("machine add cancelled: no name provided")
			}
			name = strings.TrimSpace(line)
			if name == "" {
				name = suggestion
			}
		}
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if err := session.AddMachine(context.Background(), name, !noUse); err != nil {
			return err
		}
		source := "default"
		if !noUse {
			source = "binding"
		}
		return emit(deps.Out, opt.json, "machine add", true, map[string]any{"machine": machineOutput{Name: name, Source: source}}, fmt.Sprintf("Added machine %s.\n", name))
	}}
	cmd.Flags().BoolVar(&noUse, "no-use", false, "create without selecting locally")
	return cmd
}

func machineRenameCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "rename <old> <new>", Args: cobra.ExactArgs(2), Short: "Rename a machine overlay", RunE: func(_ *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if err := session.RenameMachine(context.Background(), args[0], args[1]); err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "machine rename", true, map[string]any{"machine": machineOutput{Name: args[1], Source: "binding"}}, fmt.Sprintf("Renamed machine %s to %s.\n", args[0], args[1]))
	}}
}

func machineRemoveCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "remove <name>", Args: cobra.ExactArgs(1), Short: "Remove a machine overlay", RunE: func(_ *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if err := session.RemoveMachine(context.Background(), args[0]); err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "machine remove", true, map[string]any{"machine": machineOutput{Source: "default"}}, fmt.Sprintf("Removed machine %s.\n", args[0]))
	}}
}

func machineListCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "List machine overlays", RunE: func(_ *cobra.Command, _ []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		selection, err := selectedMachine(deps, opt, d)
		if err != nil {
			return err
		}
		type entry struct {
			Name     string `json:"name"`
			Selected bool   `json:"selected"`
			Source   string `json:"source,omitempty"`
		}
		items := make([]entry, 0, len(d.Machines.Items))
		var human strings.Builder
		human.WriteString("Machines\n")
		for _, item := range d.Machines.Items {
			selected := item.Name == selection.Name
			source := ""
			if selected {
				source = selection.Source
			}
			items = append(items, entry{Name: item.Name, Selected: selected, Source: source})
			marker := " "
			if selected {
				marker = "*"
			}
			fmt.Fprintf(&human, "%s %s\n", marker, item.Name)
		}
		if selection.Source == "binding" {
			human.WriteString("\n* locally selected for this profile\n")
		}
		return emit(deps.Out, opt.json, "machine list", true, map[string]any{"machines": items, "machine": machineOutput{Name: selection.Name, Source: selection.Source}}, human.String())
	}}
}

func machineCurrentCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "current", Args: cobra.NoArgs, Short: "Show the active machine overlay", RunE: func(_ *cobra.Command, _ []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		selection, err := selectedMachine(deps, opt, d)
		if err != nil {
			return err
		}
		human := "Machine: none (portable default resource paths)\n"
		if selection.Name != "" {
			label := "local binding"
			if selection.Source == "explicit" {
				label = "explicit --machine"
			}
			human = fmt.Sprintf("Machine: %s (%s)\n", selection.Name, label)
		}
		return emit(deps.Out, opt.json, "machine current", true, map[string]any{"machine": machineOutput{Name: selection.Name, Source: selection.Source}}, human)
	}}
}

func machineUseCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "use <name>", Args: cobra.ExactArgs(1), Short: "Select a machine overlay locally", RunE: func(_ *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if err := session.UseMachine(context.Background(), args[0]); err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "machine use", true, map[string]any{"machine": machineOutput{Name: args[0], Source: "binding"}}, fmt.Sprintf("Using machine %s.\n", args[0]))
	}}
}

func machineClearCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "clear", Args: cobra.NoArgs, Short: "Clear the local machine selection", RunE: func(_ *cobra.Command, _ []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if err := session.ClearMachine(context.Background()); err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "machine clear", true, map[string]any{"machine": machineOutput{Source: "default"}}, "Machine selection cleared.\n")
	}}
}

func machineMapCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "map <resource-ref> <path>", Args: cobra.ExactArgs(2), Short: "Map a resource root for the active machine", RunE: func(_ *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		selection := session.Machine()
		id := strings.TrimPrefix(args[0], "resource:")
		if err := session.MapResource(context.Background(), selection.Name, id, args[1]); err != nil {
			return err
		}
		path := args[1]
		if home, err := deps.HomeDir(); err == nil {
			path, _ = machine.NormalizeMappingPath(home, args[1])
		}
		return emit(deps.Out, opt.json, "machine map", true, map[string]any{"machine": machineOutput{Name: selection.Name, Source: selection.Source}, "resource": id, "path": path}, fmt.Sprintf("Mapped resource %s to %s on %s.\n", id, path, selection.Name))
	}}
}

func machineUnmapCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "unmap <resource-ref>", Args: cobra.ExactArgs(1), Short: "Remove a resource mapping from the active machine", RunE: func(_ *cobra.Command, args []string) error {
		session, err := openWorkflow(deps, opt)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		selection := session.Machine()
		id := strings.TrimPrefix(args[0], "resource:")
		if err := session.UnmapResource(context.Background(), selection.Name, id); err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "machine unmap", true, map[string]any{"machine": machineOutput{Name: selection.Name, Source: selection.Source}, "resource": id}, fmt.Sprintf("Unmapped resource %s from %s.\n", id, selection.Name))
	}}
}

func machineBindingStore(deps Dependencies) (machine.BindingStore, error) {
	state, err := deps.StateHome()
	if err != nil {
		return machine.BindingStore{}, err
	}
	return machine.BindingStore{StateHome: state}, nil
}

func selectedMachine(deps Dependencies, opt *options, d profile.Data) (machine.Selection, error) {
	state, err := deps.StateHome()
	if err != nil {
		return machine.Selection{}, err
	}
	bound, err := (machine.BindingStore{StateHome: state}).Load(opt.profileDir)
	if err != nil {
		return machine.Selection{}, err
	}
	return machine.Select(opt.machine, bound, d.Machines.Items)
}

func resolveMachineContext(deps Dependencies, opt *options, d profile.Data) (machineContext, error) {
	home, err := deps.HomeDir()
	if err != nil {
		return machineContext{}, err
	}
	state, err := deps.StateHome()
	if err != nil {
		return machineContext{}, err
	}
	selection, err := selectedMachine(deps, opt, d)
	if err != nil {
		return machineContext{}, err
	}
	profileDir, err := machine.CanonicalProfileRoot(opt.profileDir)
	if err != nil {
		return machineContext{}, err
	}
	roots, dormant, err := machine.ResolveEffectiveRoots(home, profileDir, filepath.Join(state, "omarchy-blueprint"), d.Resources, selection.Machine)
	if err != nil {
		return machineContext{}, err
	}
	return machineContext{Selection: selection, Home: home, Roots: roots, Dormant: dormant}, nil
}

func machineContextOutput(context machineContext) machineOutput {
	return machineOutput{Name: context.Selection.Name, Source: context.Selection.Source}
}

func runtimeResources(resources profile.Resources, context machineContext) resourcesOutput {
	items := make([]resourceOutput, 0, len(resources.Items))
	for _, item := range resources.Items {
		output := resourceOutput{Resource: item, DefaultPath: item.Path, EffectivePath: context.Roots[item.ID], Machine: context.Selection.Name}
		items = append(items, output)
	}
	return resourcesOutput{Items: items, Links: resources.Links, IgnoredLinks: resources.IgnoredLinks}
}

func renderMachineContext(context machineContext) string {
	if context.Selection.Name == "" {
		return "Machine: none (portable default resource paths)\n"
	}
	return "Machine: " + context.Selection.Name + "\n"
}

func trackCommand(deps Dependencies, opt *options) *cobra.Command {
	var id, strategy string
	var includeUntracked, excludeUntracked []string
	cmd := &cobra.Command{Use: "track <path|link:...>", Args: cobra.ExactArgs(1), Short: "Track a portable user resource", RunE: func(cmd *cobra.Command, args []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		adapter := resourcesStateProvider{deps: deps, opt: opt}
		provider, err := adapter.provider(d)
		if err != nil {
			return err
		}
		var changes []model.Change
		if strings.HasPrefix(args[0], "link:") {
			d.Resources, err = provider.EnableLink(d.Resources, strings.TrimPrefix(args[0], "link:"))
			if err == nil {
				changes = []model.Change{{Type: model.ChangeAdd, Provider: "resources", Kind: "link", Name: strings.TrimPrefix(args[0], "link:"), Summary: "+ link " + strings.TrimPrefix(args[0], "link:")}}
			}
		} else {
			prepared, prepareErr := provider.PrepareTrack(cmd.Context(), d.Resources, args[0], resourcesprovider.TrackOptions{
				ID: id, Strategy: strategy, IncludeUntracked: includeUntracked, ExcludeUntracked: excludeUntracked,
			})
			if prepareErr != nil {
				return prepareErr
			}
			if err := prepared.Install(); err != nil {
				return err
			}
			changes = trackChanges(d.Resources, prepared.State, prepared.Changes)
			d.Resources = prepared.State
			d.Manifest.Capture.Resources = true
			d.Manifest.Profile.UpdatedAt = deps.Now().UTC()
			if err := profile.Save(opt.profileDir, d); err != nil {
				_ = prepared.Rollback()
				return fmt.Errorf("save profile: %w", err)
			}
			if err := prepared.Commit(); err != nil {
				return err
			}
			if err := prepared.Finalize(); err != nil {
				return err
			}
			return emit(deps.Out, opt.json, "track", true, map[string]any{"resources": d.Resources, "changes": changes}, renderTrackResult(d.Resources, changes))
		}
		if err != nil {
			return err
		}
		d.Manifest.Capture.Resources = true
		d.Manifest.Profile.UpdatedAt = deps.Now().UTC()
		if err := profile.Save(opt.profileDir, d); err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "track", true, map[string]any{"resources": d.Resources, "changes": changes}, renderChanges("Tracked portable resource", changes))
	}}
	cmd.Flags().StringVar(&id, "id", "", "stable resource ID")
	cmd.Flags().StringVar(&strategy, "strategy", "", "resource strategy: git, git+diff, or copy")
	cmd.Flags().StringArrayVar(&includeUntracked, "include-untracked", nil, "include exact untracked file in git+diff state (repeatable)")
	cmd.Flags().StringArrayVar(&excludeUntracked, "exclude-untracked", nil, "remove exact untracked file from git+diff state (repeatable)")
	return cmd
}

func renderTrackResult(resources profile.Resources, changes []model.Change) string {
	human := renderChanges("Tracked portable resource", changes)
	for _, item := range resources.Items {
		if item.Strategy == "git" && item.Dirty {
			return human + "strategy: git\nLocal Git state is not captured by strategy git\nUse --strategy git+diff to capture local Git state or --strategy copy to snapshot the worktree.\n"
		}
	}
	return human
}

func trackChanges(saved, current profile.Resources, changes []model.Change) []model.Change {
	savedItems := make(map[string]profile.Resource, len(saved.Items))
	for _, item := range saved.Items {
		savedItems[item.ID] = item
	}
	result := make([]model.Change, 0, len(changes))
	for _, change := range changes {
		item, found := resourceByID(current.Items, change.Name)
		if change.Provider != "resources" || change.Kind != "resource" || !found || item.Strategy != "git" || !item.Dirty || !strings.Contains(change.Summary, "local changes are not captured") {
			result = append(result, change)
			continue
		}
		if previous, exists := savedItems[item.ID]; exists && previous.Strategy != item.Strategy {
			change.Summary = "~ resource " + item.ID + " differs"
			result = append(result, change)
		}
	}
	return result
}

func resourceByID(items []profile.Resource, id string) (profile.Resource, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return profile.Resource{}, false
}

func untrackCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "untrack <resource-ref|link-ref>", Args: cobra.ExactArgs(1), Short: "Stop tracking a portable resource", RunE: func(cmd *cobra.Command, args []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		ref := args[0]
		if !strings.HasPrefix(ref, "resource:") && !strings.HasPrefix(ref, "link:") {
			ref = "resource:" + ref
		}
		provider, err := (resourcesStateProvider{deps: deps, opt: opt}).provider(d)
		if err != nil {
			return err
		}
		prepared, changed, err := provider.PrepareUntrack(d.Resources, ref)
		if err != nil {
			return err
		}
		if prepared != nil {
			if err := prepared.Install(); err != nil {
				return err
			}
			d.Resources = prepared.State
		}
		d.Manifest.Capture.Resources = true
		d.Manifest.Profile.UpdatedAt = deps.Now().UTC()
		if err := profile.Save(opt.profileDir, d); err != nil {
			if prepared != nil {
				_ = prepared.Rollback()
			}
			return err
		}
		if prepared != nil {
			if err := prepared.Commit(); err != nil {
				return err
			}
			if err := prepared.Finalize(); err != nil {
				return err
			}
		}
		return emit(deps.Out, opt.json, "untrack", true, map[string]any{"resources": d.Resources, "removed": changed}, "Untracked "+strings.Join(changed, ", ")+"\n")
	}}
}

func trackedCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "tracked", Args: cobra.NoArgs, Short: "List tracked portable resources", RunE: func(_ *cobra.Command, _ []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		context, err := resolveMachineContext(deps, opt, d)
		if err != nil {
			return err
		}
		var b strings.Builder
		b.WriteString(renderMachineContext(context))
		b.WriteString("Portable resources\n\n")
		for _, item := range d.Resources.Items {
			fmt.Fprintf(&b, "%s\n  %s\n  %s\n", item.ID, item.Path, item.Strategy)
			if effective := context.Roots[item.ID]; effective != "" {
				defaultPath, _ := resourcesprovider.ExpandHomePath(context.Home, item.Path)
				if filepath.Clean(effective) != filepath.Clean(defaultPath) {
					fmt.Fprintf(&b, "  effective: %s (%s)\n", effective, context.Selection.Name)
				}
			}
		}
		if len(d.Resources.IgnoredLinks) > 0 {
			b.WriteString("Ignored links\n")
			for _, link := range d.Resources.IgnoredLinks {
				fmt.Fprintf(&b, "  %s\n", link)
			}
		}
		return emit(deps.Out, opt.json, "tracked", true, map[string]any{"machine": machineContextOutput(context), "resources": runtimeResources(d.Resources, context)}, b.String())
	}}
}

func initCommand(deps Dependencies, opt *options) *cobra.Command {
	var name string
	cmd := &cobra.Command{Use: "init [directory]", Args: cobra.MaximumNArgs(1), Short: "Create a profile", RunE: func(cmd *cobra.Command, args []string) error {
		dir := opt.profileDir
		if len(args) == 1 {
			dir = args[0]
		}
		abs, err := workflow.CreateProfile(cmd.Context(), workflowDependencies(deps), dir, name)
		if err != nil {
			return err
		}
		if name == "" {
			name = filepath.Base(abs)
		}
		return emit(deps.Out, opt.json, "init", true, map[string]any{"profile": abs, "name": name}, fmt.Sprintf("Created profile %q at %s\n", name, abs))
	}}
	cmd.Flags().StringVar(&name, "name", "", "profile name")
	return cmd
}

func captureCommand(deps Dependencies, opt *options) *cobra.Command {
	providers := stateProviders(deps, opt)
	return &cobra.Command{Use: "capture [packages|themes|plugins|resources|config|defaults|shell|hooks]", Args: supportedCategory(providers), Short: "Capture system state", RunE: func(cmd *cobra.Command, args []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if len(args) == 0 {
			return captureAll(cmd.Context(), deps, opt, d)
		}
		provider, ok := categoryProvider(providers, selectedCategory(args))
		if !ok {
			return fmt.Errorf("unknown category %s", selectedCategory(args))
		}
		return captureProviders(cmd.Context(), deps, opt, d, []stateProvider{provider})
	}}
}

func statusCommand(deps Dependencies, opt *options, diff bool) *cobra.Command {
	providers := stateProviders(deps, opt)
	use, short := "status [packages|themes|plugins|resources|config|defaults|shell|hooks]", "Show profile drift"
	if diff {
		use, short = "diff [packages|themes|plugins|resources|config|defaults|shell|hooks]", "Show semantic differences"
	}
	return &cobra.Command{Use: use, Args: supportedCategory(providers), Short: short, RunE: func(cmd *cobra.Command, args []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if len(args) == 0 {
			return statusAll(cmd.Context(), deps, opt, d, providers, diff)
		}
		provider, ok := categoryProvider(providers, selectedCategory(args))
		if !ok {
			return fmt.Errorf("unknown category %s", selectedCategory(args))
		}
		if !provider.Captured(d) {
			return captureRequiredError(provider.ID())
		}
		return statusAll(cmd.Context(), deps, opt, d, []stateProvider{provider}, diff)
	}}
}

func restoreCommand(deps Dependencies, opt *options) *cobra.Command {
	var dryRun, yes, force bool
	providers := stateProviders(deps, opt)
	cmd := &cobra.Command{Use: "restore [packages|themes|plugins|resources|config|defaults|shell|hooks]", Args: supportedCategory(providers), Short: "Plan or restore system state", RunE: func(cmd *cobra.Command, args []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if len(args) == 0 {
			return restoreAll(cmd.Context(), deps, opt, d, providers, dryRun, yes, restorePlanOptions{Force: force})
		}
		provider, ok := categoryProvider(providers, selectedCategory(args))
		if !ok {
			return fmt.Errorf("unknown category %s", selectedCategory(args))
		}
		if !provider.Captured(d) {
			return captureRequiredError(provider.ID())
		}
		return restoreProviders(cmd.Context(), deps, opt, d, []stateProvider{provider}, dryRun, yes, restorePlanOptions{Force: force})
	}}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the restore plan without changing the machine")
	cmd.Flags().BoolVar(&yes, "yes", false, "approve the restore non-interactively")
	cmd.Flags().BoolVar(&force, "force", false, "resolve supported restore conflicts in favor of the profile")
	return cmd
}

func checkCommand(deps Dependencies, opt *options) *cobra.Command {
	providers := stateProviders(deps, opt)
	return &cobra.Command{Use: "check", Args: cobra.NoArgs, Short: "Validate the profile and environment", RunE: func(cmd *cobra.Command, _ []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if err := profile.Validate(d); err != nil {
			return err
		}
		context, err := resolveMachineContext(deps, opt, d)
		if err != nil {
			return err
		}
		info, err := omarchy.Detect(cmd.Context(), deps.Runner)
		if err != nil {
			return err
		}
		checks := []string{"profile valid", "schema supported", "Omarchy compatible"}
		for _, provider := range providers {
			if !provider.Captured(d) {
				continue
			}
			if err := provider.Check(cmd.Context(), d); err != nil {
				return fmt.Errorf("check %s: %w", provider.ID(), err)
			}
			checks = append(checks, providerCheckLabel(provider.ID()))
		}
		human := "✓ " + strings.Join(checks, "\n✓ ") + "\n"
		human += renderMachineContext(context)
		if len(context.Dormant) > 0 {
			human += fmt.Sprintf("ℹ dormant machine mappings: %s\n", strings.Join(context.Dormant, ", "))
		}
		if len(d.Packages.Excluded) > 0 {
			human += fmt.Sprintf("ℹ %d excluded package(s): %s\n", len(d.Packages.Excluded), strings.Join(d.Packages.Excluded, ", "))
		}
		return emit(deps.Out, opt.json, "check", true, map[string]any{"checks": checks, "omarchy": info, "excluded": d.Packages.Excluded, "machine": machineContextOutput(context), "dormant_mappings": context.Dormant}, human)
	}}
}

func packagePolicyCommand(deps Dependencies, opt *options, exclude bool) *cobra.Command {
	verb := "include"
	short := "Include previously excluded packages"
	if exclude {
		verb, short = "exclude", "Exclude packages from capture, drift, and restore"
	}
	return &cobra.Command{Use: verb + " <package-reference>...", Args: cobra.MinimumNArgs(1), Short: short, RunE: func(_ *cobra.Command, refs []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if strings.HasPrefix(refs[0], "config:") {
			if len(refs) != 1 {
				return fmt.Errorf("config policy accepts exactly one config:<path> reference")
			}
			path := strings.TrimPrefix(refs[0], "config:")
			if exclude {
				d.Config, _, err = configprovider.AddExclusion(d.Config, path)
			} else {
				d.Config, _, err = configprovider.RemoveExclusion(d.Config, path)
				if err == nil {
					d.Config, _, err = configprovider.AddInclusion(d.Config, path)
				}
			}
			if err != nil {
				return err
			}
			path, err = configprovider.NormalizeExclusionPath(path)
			if err != nil {
				return err
			}
			d.Manifest.Profile.UpdatedAt = deps.Now().UTC()
			if err := profile.Save(opt.profileDir, d); err != nil {
				return fmt.Errorf("save profile: %w", err)
			}
			action := "Included"
			if exclude {
				action = "Excluded"
			}
			return emit(deps.Out, opt.json, verb, true, map[string]any{"kind": "config", "path": path, "excluded": exclude, "included": !exclude}, fmt.Sprintf("%s config %s.\n", action, path))
		}
		if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
			return err
		}
		var changed []string
		if exclude {
			d.Packages, changed, err = packagesprovider.Exclude(d.Packages, refs)
		} else {
			d.Packages, changed, err = packagesprovider.Include(d.Packages, refs)
		}
		if err != nil {
			return err
		}
		d.Manifest.Profile.UpdatedAt = deps.Now().UTC()
		if err := profile.Save(opt.profileDir, d); err != nil {
			return fmt.Errorf("save profile: %w", err)
		}
		action := "Included"
		if exclude {
			action = "Excluded"
		}
		human := fmt.Sprintf("%s %d package(s).\n", action, len(changed))
		if exclude {
			for _, association := range configprovider.RelatedConfig(changed, d.Config) {
				human += fmt.Sprintf("Related Config state remains included:\n  ~/.config/%s\nRun:\n  omarchy-blueprint exclude config:%s\n", association.ConfigPath, association.ConfigPath)
			}
		}
		return emit(deps.Out, opt.json, verb, true, map[string]any{"changed": changed, "excluded": d.Packages.Excluded}, human)
	}}
}

func configAutoCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "auto <config-reference>", Args: cobra.ExactArgs(1), Short: "Return a Config path to automatic discovery policy", RunE: func(_ *cobra.Command, refs []string) error {
		if !strings.HasPrefix(refs[0], "config:") {
			return fmt.Errorf("auto accepts exactly one config:<path> reference")
		}
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		path := strings.TrimPrefix(refs[0], "config:")
		var changed bool
		d.Config, changed, err = configprovider.ClearPolicy(d.Config, path)
		if err != nil {
			return err
		}
		path, err = configprovider.NormalizeConfigPolicyPath(path)
		if err != nil {
			return err
		}
		d.Manifest.Profile.UpdatedAt = deps.Now().UTC()
		if err := profile.Save(opt.profileDir, d); err != nil {
			return fmt.Errorf("save profile: %w", err)
		}
		return emit(deps.Out, opt.json, "auto", true, map[string]any{"kind": "config", "path": path, "policy": "auto", "changed": changed}, fmt.Sprintf("Automatic config policy restored for %s.\n", path))
	}}
}

func supportedCategory(providers []stateProvider) cobra.PositionalArgs {
	allowedIDs := categoryProviderIDs(providers)
	allowed := strings.Join(allowedIDs, ", ")
	return func(_ *cobra.Command, args []string) error {
		if len(args) > 1 {
			return fmt.Errorf("category must be %s", allowed)
		}
		if len(args) == 1 {
			valid := false
			for _, id := range allowedIDs {
				if args[0] == id {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("category must be %s", allowed)
			}
		}
		return nil
	}
}

func selectedCategory(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return "packages"
}

func defaultThemeDirs() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return "/usr/share/omarchy/themes", filepath.Join(home, ".config", "omarchy", "themes"), nil
}

func defaultPluginDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "omarchy", "plugins"), nil
}

func defaultHooksDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "omarchy", "hooks"), nil
}

func defaultShellPaths() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	omarchyPath := strings.TrimSpace(os.Getenv("OMARCHY_PATH"))
	if omarchyPath == "" {
		omarchyPath = "/usr/share/omarchy"
	}
	return filepath.Join(omarchyPath, "config", "omarchy", "shell.json"),
		filepath.Join(home, ".config", "omarchy", "shell.json"), nil
}

func defaultConfigDirs() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	omarchyRoot := os.Getenv("OMARCHY_PATH")
	if omarchyRoot == "" {
		omarchyRoot = "/usr/share/omarchy"
	}
	return filepath.Join(omarchyRoot, "config"), filepath.Join(home, ".config"), nil
}

func workflowDependencies(d Dependencies) workflow.Dependencies {
	return workflow.Dependencies{
		Runner: d.Runner, Now: d.Now, StateHome: d.StateHome, ThemeDirs: d.ThemeDirs,
		PluginDir: d.PluginDir, ConfigDirs: d.ConfigDirs, BaselineHistory: d.BaselineHistory,
		ShellPaths: d.ShellPaths, HooksDir: d.HooksDir, MiseGlobalConfig: d.MiseGlobalConfig,
		HomeDir: d.HomeDir, Hostname: d.Hostname, ResourceLinkRoots: d.ResourceLinkRoots,
	}
}

func openWorkflow(deps Dependencies, opt *options) (*workflow.Session, error) {
	session, err := workflow.Open(workflowDependencies(deps), workflow.Options{ProfileDir: opt.profileDir, ExplicitMachine: opt.machine})
	if err != nil {
		return nil, err
	}
	providers := stateProviders(deps, opt)
	adapters := make([]workflow.Provider, len(providers))
	for i, provider := range providers {
		adapter := restoreProviderAdapter{stateProvider: provider}
		switch provider.ID() {
		case "config":
			adapters[i] = configWorkflowProvider{adapter}
		case "resources":
			adapters[i] = resourcesWorkflowProvider{adapter}
		default:
			adapters[i] = adapter
		}
	}
	session.SetProviders(adapters)
	session.SetRestoreFinalizer(func(ctx context.Context, data profile.Data, selected []workflow.Provider, plan *model.RestorePlan, mode workflow.RestoreMode) error {
		state := make([]stateProvider, 0, len(selected))
		for _, provider := range selected {
			if adapter, ok := provider.(interface{ StateProvider() stateProvider }); ok {
				state = append(state, adapter.StateProvider())
			}
		}
		return finalizeRestorePlan(ctx, deps, opt, data, state, plan, restorePlanOptions{Force: mode == workflow.RestoreForced})
	})
	return session, nil
}

type restoreProviderAdapter struct{ stateProvider }

func (p restoreProviderAdapter) StateProvider() stateProvider { return p.stateProvider }

func (p restoreProviderAdapter) Plan(ctx context.Context, data profile.Data, info omarchy.Info, mode workflow.RestoreMode) (model.RestorePlan, error) {
	return p.stateProvider.Plan(ctx, data, info, restorePlanOptions{Force: mode == workflow.RestoreForced})
}

// The workflow layer discovers these optional capabilities by interface. Keep
// them visible through the restore adapter so wrapping does not alter provider
// lifecycle or UI behavior.
func (p restoreProviderAdapter) CategoryEnabled() bool {
	provider, ok := p.stateProvider.(categoryStateProvider)
	return ok && provider.CategoryEnabled()
}

type configWorkflowProvider struct{ restoreProviderAdapter }

func (p configWorkflowProvider) DiffWithScan(ctx context.Context, data profile.Data) ([]model.Change, configprovider.ScanSummary, error) {
	provider, ok := p.stateProvider.(scanDiffProvider)
	if !ok {
		return nil, configprovider.ScanSummary{}, errors.New("config status is unavailable")
	}
	return provider.DiffWithScan(ctx, data)
}

func (p configWorkflowProvider) InspectConfig(ctx context.Context, data profile.Data, path string) (workflow.ConfigInspection, error) {
	provider, ok := p.stateProvider.(interface {
		InspectConfig(context.Context, profile.Data, string) (workflow.ConfigInspection, error)
	})
	if !ok {
		return workflow.ConfigInspection{}, errors.New("config inspection is unavailable")
	}
	return provider.InspectConfig(ctx, data, path)
}

type resourcesWorkflowProvider struct{ restoreProviderAdapter }

func (p resourcesWorkflowProvider) CommitCapture() error {
	provider, ok := p.stateProvider.(interface{ CommitCapture() error })
	if !ok {
		return nil
	}
	return provider.CommitCapture()
}

func (p resourcesWorkflowProvider) FinalizeCapture() error {
	provider, ok := p.stateProvider.(interface{ FinalizeCapture() error })
	if !ok {
		return nil
	}
	return provider.FinalizeCapture()
}

func (p resourcesWorkflowProvider) RollbackCapture() error {
	provider, ok := p.stateProvider.(interface{ RollbackCapture() error })
	if !ok {
		return nil
	}
	return provider.RollbackCapture()
}

func (p resourcesWorkflowProvider) DiffWithGitWorkingState(ctx context.Context, data profile.Data) ([]model.Change, map[string]resourcesprovider.GitWorkingSummary, error) {
	provider, ok := p.stateProvider.(resourceDiffProvider)
	if !ok {
		return nil, nil, errors.New("resources status is unavailable")
	}
	return provider.DiffWithGitWorkingState(ctx, data)
}

func (p resourcesWorkflowProvider) InspectPath(ctx context.Context, data profile.Data, path string) (workflow.PathInspection, error) {
	provider, ok := p.stateProvider.(interface {
		InspectPath(context.Context, profile.Data, string) (workflow.PathInspection, error)
	})
	if !ok {
		return workflow.PathInspection{}, errors.New("resources inspection is unavailable")
	}
	return provider.InspectPath(ctx, data, path)
}

func (p resourcesWorkflowProvider) InspectResource(ctx context.Context, data profile.Data, id string) (workflow.ResourceInspection, error) {
	provider, ok := p.stateProvider.(interface {
		InspectResource(context.Context, profile.Data, string) (workflow.ResourceInspection, error)
	})
	if !ok {
		return workflow.ResourceInspection{}, errors.New("resources inspection is unavailable")
	}
	return provider.InspectResource(ctx, data, id)
}

func (p resourcesWorkflowProvider) TrackResource(ctx context.Context, data profile.Data, request workflow.TrackRequest) (profile.Data, profile.Resource, []model.Change, error) {
	provider, ok := p.stateProvider.(interface {
		TrackResource(context.Context, profile.Data, workflow.TrackRequest) (profile.Data, profile.Resource, []model.Change, error)
	})
	if !ok {
		return data, profile.Resource{}, nil, errors.New("resources tracking is unavailable")
	}
	return provider.TrackResource(ctx, data, request)
}

func (p resourcesWorkflowProvider) UntrackResource(ctx context.Context, data profile.Data, id string) (profile.Data, []string, error) {
	provider, ok := p.stateProvider.(interface {
		UntrackResource(context.Context, profile.Data, string) (profile.Data, []string, error)
	})
	if !ok {
		return data, nil, errors.New("resources tracking is unavailable")
	}
	return provider.UntrackResource(ctx, data, id)
}
func pluginProvider(deps Dependencies, opt *options) (pluginsprovider.Provider, error) {
	dir, err := deps.PluginDir()
	return pluginsprovider.Provider{Runner: deps.Runner, UserDir: dir, ProfileDir: opt.profileDir}, err
}

func themeProvider(deps Dependencies, opt *options) (themesprovider.Provider, error) {
	builtin, user, err := deps.ThemeDirs()
	return themesprovider.Provider{Runner: deps.Runner, BuiltinDir: builtin, UserDir: user, ProfileDir: opt.profileDir}, err
}

func captureAll(ctx context.Context, deps Dependencies, opt *options, d profile.Data) error {
	return captureProviders(ctx, deps, opt, d, stateProviders(deps, opt))
}

func captureProviders(ctx context.Context, deps Dependencies, opt *options, d profile.Data, providers []stateProvider) error {
	session, err := openWorkflow(deps, opt)
	if err != nil {
		return profileError(opt.profileDir, err)
	}
	result, err := session.Capture(ctx, captureOnlyProvider(providers, stateProviders(deps, opt)))
	if err != nil {
		return err
	}
	data := map[string]any{}
	for _, id := range result.Providers {
		switch id {
		case "packages":
			data[id] = result.Profile.Packages
		case "themes":
			data[id] = result.Profile.Themes
		case "plugins":
			data[id] = result.Profile.Plugins
		case "resources":
			if len(result.Profile.Resources.Items) > 0 {
				data[id] = result.Profile.Resources
			}
		case "config":
			if result.ConfigScan != nil {
				data[id] = configCaptureOutput{Configs: result.Profile.Config, Scan: configScanOutput{Counts: result.ConfigScan.Counts(), Candidates: result.ConfigScan.Candidates, Surfaces: result.ConfigScan.Surfaces}}
			}
		case "defaults":
			if result.Profile.Defaults != (profile.Defaults{}) {
				data[id] = result.Profile.Defaults
			}
		case "shell":
			if result.Profile.Shell.Hash != "" {
				data[id] = result.Profile.Shell
			}
		case "hooks":
			if len(result.Profile.Hooks.Items) > 0 {
				data[id] = result.Profile.Hooks
			}
		}
	}
	data["changes"] = result.Changes
	var configResult *configprovider.CaptureResult
	if result.ConfigScan != nil {
		configResult = &configprovider.CaptureResult{State: result.Profile.Config, Scan: *result.ConfigScan}
	}
	human := renderCaptureChanges("Captured "+providerStateLabel(result.Providers)+" state", result.Changes, configResult)
	return emit(deps.Out, opt.json, "capture", true, data, human)
}

func captureOnlyProvider(selected, all []stateProvider) string {
	if len(selected) != 1 || len(all) == 1 {
		return ""
	}
	return selected[0].ID()
}

func renderCaptureChanges(title string, changes []model.Change, result *configprovider.CaptureResult) string {
	if result == nil {
		return renderChanges(title, changes)
	}
	var b strings.Builder
	fmt.Fprintln(&b, title)
	configChanges := make([]model.Change, 0)
	pruned := map[string]int{}
	for _, change := range changes {
		if change.Provider == "config" {
			if change.Type == model.ChangeRemove {
				pruned[configSurfaceName(change.Name)]++
				continue
			}
			configChanges = append(configChanges, change)
		} else {
			fmt.Fprintln(&b, change.Summary)
		}
	}
	configHuman := renderConfigStatus(configChanges, result.Scan)
	if configHuman == "" && len(changes) == 0 {
		fmt.Fprintln(&b, "No changes.")
	}
	b.WriteString(configHuman)
	if len(pruned) > 0 {
		fmt.Fprintln(&b, "No longer captured")
		surfaces := make([]string, 0, len(pruned))
		for surface := range pruned {
			surfaces = append(surfaces, surface)
		}
		sort.Strings(surfaces)
		for _, surface := range surfaces {
			fmt.Fprintf(&b, "  %-16s %d\n", surface, pruned[surface])
		}
	}
	return b.String()
}

type configCaptureOutput struct {
	profile.Configs
	Scan configScanOutput `json:"scan"`
}

type configScanOutput struct {
	Counts     map[configprovider.Classification]int `json:"counts"`
	Candidates []configprovider.Candidate            `json:"candidates"`
	Surfaces   []configprovider.SurfaceSummary       `json:"surfaces,omitempty"`
}

func renderConfigScan(scan configScanOutput) string {
	var b strings.Builder
	groups := []struct {
		title string
		items []configprovider.Classification
	}{
		{"Modified", []configprovider.Classification{configprovider.ConfigModifiedBaseline}},
		{"Added", []configprovider.Classification{configprovider.ConfigAdded}},
		{"Deleted", []configprovider.Classification{configprovider.ConfigDeletedBaseline}},
		{"Ignored", []configprovider.Classification{configprovider.ConfigUnchangedBaseline, configprovider.ConfigDelegated, configprovider.ConfigExcluded, configprovider.ConfigVolatile}},
		{"Skipped", []configprovider.Classification{configprovider.ConfigSensitive, configprovider.ConfigUnmanagedSymlink, configprovider.ConfigUnsupported, configprovider.ConfigOversized}},
	}
	for _, group := range groups {
		count := 0
		for _, classification := range group.items {
			count += scan.Counts[classification]
		}
		if count == 0 {
			continue
		}
		fmt.Fprintf(&b, "\nConfig %s: %d\n", group.title, count)
	}
	b.WriteString(renderConfigDiscovery(scan.Surfaces, "Skipped automatic Config discovery"))
	return b.String()
}

func renderConfigDiscovery(surfaces []configprovider.SurfaceSummary, title string) string {
	var b strings.Builder
	for _, surface := range surfaces {
		if surface.Classification == configprovider.SurfaceConfigLean {
			continue
		}
		if b.Len() == 0 {
			fmt.Fprintln(&b, title)
		}
		fmt.Fprintf(&b, "  %-16s %s\n", configSurfaceName(surface.Path), configSurfaceDescription(surface))
	}
	if b.Len() > 0 {
		b.WriteString("  Informational only; skipped surfaces do not create drift. Include a safe subtree with include config:<path>.\n")
	}
	return b.String()
}

func configSurfaceDescription(surface configprovider.SurfaceSummary) string {
	for _, reason := range surface.Reasons {
		switch reason {
		case "browser-profile-chromium", "browser-profile-gecko", "browser-profile-webkit":
			return "browser/profile state"
		case "mixed-config-and-runtime":
			return "mixed config / application state"
		}
	}
	if surface.Classification == configprovider.SurfaceMixed {
		return "mixed config / application state"
	}
	return "application/profile state"
}

func configSurfaceName(path string) string {
	path = strings.TrimPrefix(filepath.ToSlash(path), ".config/")
	if index := strings.IndexByte(path, '/'); index >= 0 {
		return path[:index]
	}
	return path
}

func renderConfigStatus(changes []model.Change, scan configprovider.ScanSummary) string {
	var b strings.Builder
	counts := map[string]map[model.ChangeType]int{}
	for _, change := range changes {
		surface := configSurfaceName(change.Name)
		if surface == "" {
			surface = "home"
		}
		if counts[surface] == nil {
			counts[surface] = map[model.ChangeType]int{}
		}
		counts[surface][change.Type]++
	}
	if len(counts) > 0 {
		fmt.Fprintln(&b, "Config")
		surfaces := make([]string, 0, len(counts))
		for surface := range counts {
			surfaces = append(surfaces, surface)
		}
		sort.Strings(surfaces)
		for _, surface := range surfaces {
			for _, changeType := range []model.ChangeType{model.ChangeModify, model.ChangeAdd, model.ChangeRemove} {
				if count := counts[surface][changeType]; count > 0 {
					label := string(changeType)
					if changeType == model.ChangeAdd {
						label = "uncaptured"
					} else if changeType == model.ChangeRemove {
						label = "missing"
					}
					fmt.Fprintf(&b, "  %-9s %-16s %d\n", label, surface, count)
				}
			}
		}
	}
	if count := scan.Counts()[configprovider.ConfigAmbiguousBaseline] + scan.Counts()[configprovider.ConfigAmbiguousDeletion]; count > 0 {
		fmt.Fprintf(&b, "Baseline provenance requires review: %d\n", count)
	}
	b.WriteString(renderConfigDiscovery(scan.Surfaces, "Skipped automatic Config discovery"))
	return b.String()
}

func statusAll(ctx context.Context, deps Dependencies, opt *options, d profile.Data, providers []stateProvider, diff bool) error {
	machineContext, err := resolveMachineContext(deps, opt, d)
	if err != nil {
		return err
	}
	session, err := openWorkflow(deps, opt)
	if err != nil {
		return profileError(opt.profileDir, err)
	}
	onlyProvider := ""
	if len(providers) == 1 && len(stateProviders(deps, opt)) != 1 {
		onlyProvider = providers[0].ID()
	}
	report, err := session.Status(ctx, onlyProvider)
	if err != nil {
		return err
	}
	var changes []model.Change
	var configScan *configprovider.ScanSummary
	var gitWorking map[string]resourcesprovider.GitWorkingSummary
	for _, provider := range report.Providers {
		changes = append(changes, provider.Changes...)
		if provider.ConfigScan != nil {
			configScan = provider.ConfigScan
		}
		if provider.ResourceGit != nil {
			gitWorking = provider.ResourceGit
		}
	}
	driftCount := 0
	for _, change := range changes {
		if change.Type != model.ChangeWarn || change.Provider != "hooks" {
			driftCount++
		}
	}
	title := "Profile matches this machine"
	if diff {
		title = "Profile differences"
	} else if driftCount > 0 {
		title = fmt.Sprintf("%d profile difference(s)", driftCount)
	}
	if len(report.Providers) == 1 && report.Providers[0].ID == "packages" {
		title = "Profile matches this machine"
		if diff {
			title = "Package differences"
		} else if driftCount > 0 {
			title = fmt.Sprintf("%d package differences", driftCount)
		}
	}
	commandName := "status"
	if diff {
		commandName = "diff"
	}
	data := map[string]any{"drift": driftCount > 0, "changes": changes, "providers": report.Providers, "machine": machineContextOutput(machineContext), "resources": runtimeResources(d.Resources, machineContext)}
	human := renderMachineContext(machineContext) + renderChanges(title, changes)
	if len(gitWorking) > 0 {
		data["git_working_state"] = gitWorking
		human += renderGitWorkingState(d.Resources, gitWorking)
	}
	if configScan != nil {
		data["config"] = configCaptureOutput{Configs: d.Config, Scan: configScanOutput{Counts: configScan.Counts(), Candidates: configScan.Candidates, Surfaces: configScan.Surfaces}}
		if diff {
			human += renderConfigDiscovery(configScan.Surfaces, "Skipped discovery surfaces")
			human += renderConfigProvenanceReview(*configScan)
		} else {
			var configChanges []model.Change
			for _, change := range changes {
				if change.Provider == "config" {
					configChanges = append(configChanges, change)
				}
			}
			configHuman := renderConfigStatus(configChanges, *configScan)
			otherHuman := renderNonConfigChanges("", changes)
			if configHuman == "" && otherHuman == "" {
				human = renderChanges(title, nil)
			} else {
				human = configHuman + otherHuman
				if driftCount > 0 {
					human = title + "\n\n" + human
				}
			}
		}
	}
	if err := emit(deps.Out, opt.json, commandName, true, data, human); err != nil {
		return err
	}
	if driftCount > 0 {
		return driftError{}
	}
	return nil
}

func renderGitWorkingState(resources profile.Resources, working map[string]resourcesprovider.GitWorkingSummary) string {
	var b strings.Builder
	gitIDs, gitDiffIDs := make([]string, 0, len(working)), make([]string, 0, len(working))
	for id, summary := range working {
		item, ok := resourceByID(resources.Items, id)
		if !ok {
			continue
		}
		if item.Strategy == "git" && (summary.StagedTracked > 0 || summary.UnstagedTracked > 0 || len(summary.Untracked) > 0) {
			gitIDs = append(gitIDs, id)
		}
		if item.Strategy == "git+diff" && len(summary.Untracked) > len(summary.SelectedUntracked) {
			gitDiffIDs = append(gitDiffIDs, id)
		}
	}
	if len(gitIDs) == 0 && len(gitDiffIDs) == 0 {
		return ""
	}
	sort.Strings(gitIDs)
	if len(gitIDs) > 0 {
		b.WriteString("Git working state not managed\n")
	}
	for _, id := range gitIDs {
		summary := working[id]
		fmt.Fprintf(&b, "  %-14s %d staged, %d unstaged, %d untracked\n", id, summary.StagedTracked, summary.UnstagedTracked, len(summary.Untracked))
		fmt.Fprintln(&b, "  Capture policy: git")
	}
	sort.Strings(gitDiffIDs)
	if len(gitDiffIDs) > 0 {
		b.WriteString("Additional untracked Git files not managed\n")
	}
	for _, id := range gitDiffIDs {
		summary := working[id]
		fmt.Fprintf(&b, "  %-14s %d untracked\n", id, len(summary.Untracked)-len(summary.SelectedUntracked))
		fmt.Fprintln(&b, "  Capture policy: git+diff")
	}
	return b.String()
}

func renderConfigProvenanceReview(scan configprovider.ScanSummary) string {
	var entries []configprovider.Candidate
	for _, candidate := range scan.Candidates {
		if candidate.Classification == configprovider.ConfigAmbiguousBaseline || candidate.Classification == configprovider.ConfigAmbiguousDeletion {
			entries = append(entries, candidate)
		}
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	var b strings.Builder
	b.WriteString("Baseline provenance requires review\n")
	for _, entry := range entries {
		label := "differs"
		if entry.Classification == configprovider.ConfigAmbiguousDeletion {
			label = "absent"
		}
		fmt.Fprintf(&b, "  %-9s %s\n", label, entry.Path)
	}
	b.WriteString("\nNot captured automatically. Include a path to declare its current state as managed Config.\n")
	return b.String()
}

func renderNonConfigChanges(title string, changes []model.Change) string {
	other := make([]model.Change, 0, len(changes))
	for _, change := range changes {
		if change.Provider != "config" {
			other = append(other, change)
		}
	}
	if len(other) == 0 {
		return ""
	}
	if title == "" {
		var b strings.Builder
		for _, change := range other {
			fmt.Fprintln(&b, change.Summary)
		}
		return b.String()
	}
	return renderChanges(title, other)
}

type restorePlanOptions struct {
	Force bool
}

func restoreAll(ctx context.Context, deps Dependencies, opt *options, d profile.Data, providers []stateProvider, dryRun, yes bool, planOptions restorePlanOptions) error {
	return restoreProviders(ctx, deps, opt, d, capturedProviders(providers, d), dryRun, yes, planOptions)
}

func restoreProviders(ctx context.Context, deps Dependencies, opt *options, d profile.Data, providers []stateProvider, dryRun, yes bool, planOptions restorePlanOptions) error {
	session, err := openWorkflow(deps, opt)
	if err != nil {
		return profileError(opt.profileDir, err)
	}
	onlyProvider := ""
	if len(providers) == 1 {
		onlyProvider = providers[0].ID()
	}
	mode := workflow.RestoreNormal
	if planOptions.Force {
		mode = workflow.RestoreForced
	}
	plan, err := session.PlanRestore(ctx, onlyProvider, mode)
	if err != nil {
		return err
	}
	d = session.Profile()
	if dryRun {
		return emit(deps.Out, opt.json, "restore", true, map[string]any{"dry_run": true, "plan": plan}, renderPlanWithOptions(plan, true, planOptions))
	}
	if len(plan.Operations) == 0 {
		verification, err := verifyProviders(ctx, d, providers)
		if err != nil {
			return err
		}
		if !verification.OK {
			if message, ok := unresolvedShellConflictMessage(plan); ok {
				if err := emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": verification}, message); err != nil {
					return err
				}
				return driftError{}
			}
			return fmt.Errorf("nothing can be restored automatically; verification failed: missing %s", strings.Join(verification.Missing, ", "))
		}
		message := "All captured state is already restored. No changes applied.\n"
		if len(providers) == 1 && providers[0].ID() == "packages" {
			message = "All desired packages are installed. No changes applied.\n"
		}
		return emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": verification}, renderPlanWithOptions(plan, false, planOptions)+message)
	}
	if !yes {
		if opt.json {
			return errors.New("restore with --json requires --yes or --dry-run")
		}
		fmt.Fprint(deps.Out, renderPlanWithOptions(plan, false, planOptions), "Apply this restore? [y/N] ")
		answer, _ := bufio.NewReader(deps.In).ReadString('\n')
		if value := strings.ToLower(strings.TrimSpace(answer)); value != "y" && value != "yes" {
			return errors.New("restore cancelled")
		}
	}
	stateHome, err := deps.StateHome()
	if err != nil {
		return err
	}
	journal, err := restore.NewJournal(stateHome, deps.Now())
	if err != nil {
		return fmt.Errorf("create restore journal: %w", err)
	}
	defer journal.Close()
	var progress restore.ProgressFunc
	if !opt.json {
		progress = func(event restore.Progress) { renderProgress(deps.Out, event) }
	}
	execution, err := restore.Execute(ctx, deps.Runner, plan, journal, deps.Now, 5*time.Second, progress)
	if err != nil {
		return err
	}
	verification, err := verifyProviders(ctx, d, providers)
	if err != nil {
		return err
	}
	_ = journal.Write(restore.Event{Time: deps.Now().UTC(), Type: "VERIFY_COMPLETED", Message: fmt.Sprintf("ok=%t", verification.OK)})
	if len(execution.Failed) > 0 {
		if opt.json {
			_ = emit(deps.Out, true, "restore", false, map[string]any{"plan": plan, "execution": execution, "verification": verification, "journal": journal.Path}, "")
		} else {
			renderRestoreFailures(deps.Out, execution, verification, journal.Path)
		}
		return fmt.Errorf("restore completed with %d failed operation(s)", len(execution.Failed))
	}
	if !verification.OK {
		if message, ok := unresolvedShellConflictMessage(plan); ok {
			if err := emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": verification, "journal": journal.Path}, message); err != nil {
				return err
			}
			return driftError{}
		}
		return fmt.Errorf("restore completed but verification failed: missing %s", strings.Join(verification.Missing, ", "))
	}
	return emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": verification, "journal": journal.Path}, fmt.Sprintf("Restore verified. Journal: %s\n", journal.Path))
}

func unresolvedShellConflictMessage(plan model.RestorePlan) (string, bool) {
	var conflicts []string
	for _, skipped := range plan.Skipped {
		if skipped.Provider == "shell" && strings.HasPrefix(skipped.Resource, "shell:") && skipped.Resource != "shell:config" && strings.Contains(skipped.Reason, "keeping the current value") {
			conflicts = append(conflicts, strings.TrimPrefix(skipped.Resource, "shell:"))
		}
	}
	if len(conflicts) == 0 {
		return "", false
	}
	sort.Strings(conflicts)
	return fmt.Sprintf("Safe Shell changes were restored. %d conflict(s) remain: %s\nRun `restore shell --force` to apply captured intent.\n", len(conflicts), strings.Join(conflicts, ", ")), true
}

func verifyProviders(ctx context.Context, d profile.Data, providers []stateProvider) (model.VerificationResult, error) {
	result := model.VerificationResult{OK: true}
	for _, provider := range providers {
		verification, err := provider.Verify(ctx, d)
		if err != nil {
			return model.VerificationResult{}, fmt.Errorf("verify %s restore: %w", provider.ID(), err)
		}
		result.OK = result.OK && verification.OK
		result.Missing = append(result.Missing, verification.Missing...)
	}
	return result, nil
}

func profileError(dir string, err error) error { return fmt.Errorf("load profile at %s: %w", dir, err) }

func emit(w io.Writer, asJSON bool, command string, ok bool, data map[string]any, human string) error {
	if !asJSON {
		_, err := io.WriteString(w, human)
		return err
	}
	envelope := map[string]any{"api_version": 1, "command": command, "ok": ok, "data": data}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

func renderChanges(title string, changes []model.Change) string {
	var b strings.Builder
	fmt.Fprintln(&b, title)
	if len(changes) == 0 {
		fmt.Fprintln(&b, "No changes.")
		return b.String()
	}
	for _, change := range changes {
		fmt.Fprintln(&b, change.Summary)
	}
	return b.String()
}

func renderPlan(plan model.RestorePlan, dry bool) string {
	var b strings.Builder
	if dry {
		fmt.Fprintln(&b, "Dry-run restore plan")
	} else {
		fmt.Fprintln(&b, "Restore plan")
	}
	fmt.Fprintf(&b, "Omarchy: %s → %s\n", plan.OmarchyFrom, plan.OmarchyTo)
	if len(plan.Operations) == 0 && len(plan.Skipped) == 0 {
		fmt.Fprintln(&b, "No operations required.")
		return b.String()
	}
	for _, op := range plan.Operations {
		fmt.Fprintf(&b, "+ %s %s (risk: %s, reversible: %t)\n", op.Action, op.Resource, op.Risk, op.Reversible)
	}
	for _, op := range plan.Operations {
		if op.Provider == "shell" && op.Action == "write" && op.File != nil {
			if op.File.Backup {
				fmt.Fprintln(&b, "! Existing Omarchy Shell configuration will be replaced; a backup will be stored beside the restore journal.")
			} else {
				fmt.Fprintln(&b, "! Missing Omarchy Shell configuration will be created.")
			}
			break
		}
	}
	for _, op := range plan.Operations {
		if op.Provider == "shell" && op.Action == "restart" {
			fmt.Fprintln(&b, "! Omarchy Shell will be restarted after restore.")
			break
		}
	}
	for _, op := range plan.Operations {
		if op.Provider == "config" && op.Risk == model.RiskMedium && op.File != nil {
			if op.File.Backup {
				fmt.Fprintln(&b, "! Existing Hyprland configuration files will be replaced; backups will be stored beside the restore journal.")
			} else {
				fmt.Fprintln(&b, "! Missing Hyprland configuration files will be created.")
			}
			break
		}
	}
	for _, op := range plan.Operations {
		if op.Provider == "plugins" && op.Risk == model.RiskHigh {
			fmt.Fprintln(&b, "! Third-party plugins execute unsandboxed code inside omarchy-shell; review their source before approval.")
			break
		}
	}
	for _, op := range plan.Operations {
		if op.Provider == "hooks" && op.Risk == model.RiskHigh {
			fmt.Fprintln(&b, "! Omarchy hooks are arbitrary user code that runs automatically on system events. Review captured hook source before approving this restore.")
			break
		}
	}
	for _, skipped := range plan.Skipped {
		if skipped.Provider == "shell" && strings.HasPrefix(skipped.Resource, "shell:") && skipped.Resource != "shell:config" && strings.Contains(skipped.Reason, "keeping the current value") {
			fmt.Fprintf(&b, "! %s changed independently on this machine; keeping the current value\n", strings.TrimPrefix(skipped.Resource, "shell:"))
			continue
		}
		fmt.Fprintf(&b, "- skip %s (%s)\n", skipped.Resource, skipped.Reason)
	}
	return b.String()
}

func renderPlanWithOptions(plan model.RestorePlan, dry bool, options restorePlanOptions) string {
	rendered := renderPlan(plan, dry)
	if !options.Force {
		return rendered
	}
	for _, operation := range plan.Operations {
		if operation.Provider == "shell" {
			return rendered + "! Force enabled: conflicting Shell values will be replaced by captured profile intent; unrelated target-only Shell customization is preserved.\n"
		}
	}
	return rendered
}

func renderProgress(w io.Writer, event restore.Progress) {
	if event.Operation.Provider == "resources" {
		name := strings.TrimPrefix(event.Operation.Resource, "resource:")
		verb, past := "Restoring resource", "Restored resource"
		switch event.Operation.Action {
		case "git clone":
			verb, past = "Cloning resource", "Cloned resource"
		case "git checkout":
			verb, past = "Checking out", "Checked out"
		case "symlink":
			name, verb, past = strings.TrimPrefix(event.Operation.Resource, "link:"), "Creating link", "Created link"
		}
		switch event.Type {
		case restore.ProgressStarted:
			fmt.Fprintf(w, "%s %s...\n", verb, name)
		case restore.ProgressCompleted:
			fmt.Fprintf(w, "✓ %s %s (%s)\n", past, name, event.Elapsed)
		case restore.ProgressHeartbeat:
			fmt.Fprintf(w, "  Still %s %s (%s elapsed)...\n", strings.ToLower(verb), name, event.Elapsed)
		case restore.ProgressFailed:
			fmt.Fprintf(w, "✗ Failed %s %s after %s\n", strings.ToLower(verb), name, event.Elapsed)
		}
		return
	}
	if event.Operation.Provider == "hooks" {
		path := strings.TrimPrefix(event.Operation.Resource, "hook:")
		switch event.Type {
		case restore.ProgressStarted:
			fmt.Fprintf(w, "Restoring hook %s...\n", path)
		case restore.ProgressCompleted:
			fmt.Fprintf(w, "✓ Restored hook %s (%s)\n", path, event.Elapsed)
		case restore.ProgressHeartbeat:
			fmt.Fprintf(w, "  Still restoring hook %s (%s elapsed)...\n", path, event.Elapsed)
		case restore.ProgressFailed:
			fmt.Fprintf(w, "✗ Failed restoring hook %s after %s\n", path, event.Elapsed)
		}
		return
	}
	if event.Operation.Provider == "shell" {
		verb, past := "Restoring Omarchy Shell configuration", "Restored Omarchy Shell configuration"
		if event.Operation.Action == "restart" {
			verb, past = "Restarting Omarchy Shell", "Restarted Omarchy Shell"
		}
		switch event.Type {
		case restore.ProgressStarted:
			fmt.Fprintf(w, "%s...\n", verb)
		case restore.ProgressCompleted:
			fmt.Fprintf(w, "✓ %s (%s)\n", past, event.Elapsed)
		case restore.ProgressHeartbeat:
			fmt.Fprintf(w, "  Still %s (%s elapsed)...\n", strings.ToLower(verb), event.Elapsed)
		case restore.ProgressFailed:
			fmt.Fprintf(w, "✗ Failed %s after %s\n", strings.ToLower(verb), event.Elapsed)
		}
		return
	}
	if event.Operation.Provider == "defaults" {
		kind := ""
		if len(event.Operation.Items) > 0 {
			kind = event.Operation.Items[0]
		}
		switch event.Type {
		case restore.ProgressStarted:
			fmt.Fprintf(w, "Setting default %s...\n", kind)
		case restore.ProgressCompleted:
			fmt.Fprintf(w, "✓ Set default %s (%s)\n", kind, event.Elapsed)
		case restore.ProgressHeartbeat:
			fmt.Fprintf(w, "  Still setting default %s (%s elapsed)...\n", kind, event.Elapsed)
		case restore.ProgressFailed:
			fmt.Fprintf(w, "✗ Failed setting default %s\n", kind)
		}
		return
	}
	if event.Operation.Provider == "config" {
		switch event.Type {
		case restore.ProgressStarted:
			fmt.Fprintf(w, "Restoring %s...\n", event.Operation.Resource)
		case restore.ProgressCompleted:
			fmt.Fprintf(w, "✓ Restored %s (%s)\n", event.Operation.Resource, event.Elapsed)
		case restore.ProgressHeartbeat:
			fmt.Fprintf(w, "  Still restoring %s (%s elapsed)...\n", event.Operation.Resource, event.Elapsed)
		case restore.ProgressFailed:
			fmt.Fprintf(w, "✗ Failed restoring %s after %s\n", event.Operation.Resource, event.Elapsed)
		}
		return
	}
	if event.Operation.Provider == "plugins" {
		verb := strings.ToUpper(event.Operation.Action[:1]) + event.Operation.Action[1:] + "ing"
		if event.Operation.Action == "enable" {
			verb = "Enabling"
		}
		if event.Operation.Action == "disable" {
			verb = "Disabling"
		}
		switch event.Type {
		case restore.ProgressStarted:
			fmt.Fprintf(w, "%s %s...\n", verb, event.Operation.Resource)
		case restore.ProgressCompleted:
			fmt.Fprintf(w, "✓ %s %s (%s)\n", event.Operation.Action, event.Operation.Resource, event.Elapsed)
		case restore.ProgressFailed:
			fmt.Fprintf(w, "✗ Failed %s %s\n", event.Operation.Action, event.Operation.Resource)
		}
		return
	}
	if event.Operation.Provider == "themes" {
		verb, past := "Processing", "Processed"
		switch event.Operation.Action {
		case "install":
			verb, past = "Installing", "Installed"
		case "pin":
			verb, past = "Pinning", "Pinned"
		case "copy":
			verb, past = "Restoring", "Restored"
		case "activate":
			verb, past = "Activating", "Activated"
		}
		switch event.Type {
		case restore.ProgressStarted:
			fmt.Fprintf(w, "%s %s...\n", verb, event.Operation.Resource)
		case restore.ProgressCompleted:
			fmt.Fprintf(w, "✓ %s %s (%s)\n", past, event.Operation.Resource, event.Elapsed)
		case restore.ProgressHeartbeat:
			fmt.Fprintf(w, "  Still %s %s (%s elapsed)...\n", strings.ToLower(verb), event.Operation.Resource, event.Elapsed)
		case restore.ProgressFailed:
			fmt.Fprintf(w, "✗ Failed %s %s after %s\n", strings.ToLower(verb), event.Operation.Resource, event.Elapsed)
		}
		return
	}
	kind := strings.SplitN(event.Operation.Resource, ":", 2)[0]
	count := len(event.Operation.Items)
	label := fmt.Sprintf("%d %s package", count, kind)
	if count != 1 {
		label += "s"
	}
	switch event.Type {
	case restore.ProgressStarted:
		fmt.Fprintf(w, "Installing %s...\n", label)
	case restore.ProgressHeartbeat:
		fmt.Fprintf(w, "  Still installing %s (%s elapsed)...\n", label, event.Elapsed)
	case restore.ProgressCompleted:
		fmt.Fprintf(w, "✓ Installed %s (%s)\n", label, event.Elapsed)
	case restore.ProgressFailed:
		fmt.Fprintf(w, "✗ Failed installing %s after %s\n", label, event.Elapsed)
	}
}

func renderRestoreFailures(w io.Writer, execution restore.Result, verification model.VerificationResult, journal string) {
	fmt.Fprintf(w, "\nRestore completed with %d successful and %d failed operation(s).\n", len(execution.Completed), len(execution.Failed))
	for _, failure := range execution.Failed {
		fmt.Fprintf(w, "✗ %s: %s\n", failure.Operation.Resource, failure.Error)
	}
	if len(execution.Blocked) > 0 {
		fmt.Fprintf(w, "%d dependent operation(s) skipped.\n", len(execution.Blocked))
		for _, blocked := range execution.Blocked {
			fmt.Fprintf(w, "↷ %s (dependency %s failed)\n", blocked.Operation.Resource, blocked.Dependency)
		}
	}
	if len(verification.Missing) > 0 {
		fmt.Fprintf(w, "Still missing: %s\n", strings.Join(verification.Missing, ", "))
	}
	fmt.Fprintf(w, "Journal: %s\n", journal)
}
