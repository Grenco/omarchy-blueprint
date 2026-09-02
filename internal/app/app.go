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
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/graeme/omarchy-blueprint/internal/command"
	"github.com/graeme/omarchy-blueprint/internal/model"
	"github.com/graeme/omarchy-blueprint/internal/omarchy"
	"github.com/graeme/omarchy-blueprint/internal/profile"
	packagesprovider "github.com/graeme/omarchy-blueprint/internal/providers/packages"
	themesprovider "github.com/graeme/omarchy-blueprint/internal/providers/themes"
	"github.com/graeme/omarchy-blueprint/internal/restore"
)

type Dependencies struct {
	Runner    command.Runner
	In        io.Reader
	Out       io.Writer
	Err       io.Writer
	Now       func() time.Time
	StateHome func() (string, error)
	ThemeDirs func() (builtin, user string, err error)
}

type options struct {
	profileDir string
	json       bool
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
	root := &cobra.Command{Use: "omarchy-blueprint", Short: "Capture and restore portable Omarchy state", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().StringVar(&opt.profileDir, "profile", ".", "profile directory")
	root.PersistentFlags().BoolVar(&opt.json, "json", false, "emit machine-readable JSON")
	root.AddCommand(initCommand(deps, opt), captureCommand(deps, opt), statusCommand(deps, opt, false), statusCommand(deps, opt, true), restoreCommand(deps, opt), checkCommand(deps, opt), packagePolicyCommand(deps, opt, true), packagePolicyCommand(deps, opt, false))
	return root
}

func initCommand(deps Dependencies, opt *options) *cobra.Command {
	var name string
	cmd := &cobra.Command{Use: "init [directory]", Args: cobra.MaximumNArgs(1), Short: "Create a profile", RunE: func(cmd *cobra.Command, args []string) error {
		dir := opt.profileDir
		if len(args) == 1 {
			dir = args[0]
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(abs, "profile.toml")); err == nil {
			return fmt.Errorf("profile already exists at %s", abs)
		}
		if name == "" {
			name = filepath.Base(abs)
		}
		info, err := omarchy.Detect(cmd.Context(), deps.Runner)
		if err != nil {
			return err
		}
		d := profile.New(name, deps.Now())
		d.Manifest.Omarchy.CapturedVersion = info.Version
		d.Manifest.Omarchy.Channel = info.Channel
		if err := profile.Save(abs, d); err != nil {
			return fmt.Errorf("create profile: %w", err)
		}
		return emit(deps.Out, opt.json, "init", true, map[string]any{"profile": abs, "name": name}, fmt.Sprintf("Created profile %q at %s\n", name, abs))
	}}
	cmd.Flags().StringVar(&name, "name", "", "profile name")
	return cmd
}

func captureCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "capture [packages|themes]", Args: supportedCategory, Short: "Capture system state", RunE: func(cmd *cobra.Command, args []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if len(args) == 0 {
			return captureAll(cmd.Context(), deps, opt, d)
		}
		if selectedCategory(args) == "themes" {
			return captureThemes(cmd.Context(), deps, opt, d)
		}
		if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
			return err
		}
		info, err := omarchy.Detect(cmd.Context(), deps.Runner)
		if err != nil {
			return err
		}
		provider := packagesprovider.Provider{Runner: deps.Runner}
		current, err := provider.Detect(cmd.Context())
		if err != nil {
			return err
		}
		current = packagesprovider.ApplyExclusions(current, d.Packages.Excluded)
		changes := packagesprovider.Diff(d.Packages, current)
		d.Packages = current
		d.Manifest.Capture.Packages = true
		d.Manifest.Profile.UpdatedAt = deps.Now().UTC()
		d.Manifest.Omarchy.CapturedVersion = info.Version
		d.Manifest.Omarchy.Channel = info.Channel
		if err := profile.Save(opt.profileDir, d); err != nil {
			return fmt.Errorf("save profile: %w", err)
		}
		return emit(deps.Out, opt.json, "capture", true, map[string]any{"changes": changes, "packages": current}, renderChanges("Captured package state", changes))
	}}
}

func statusCommand(deps Dependencies, opt *options, diff bool) *cobra.Command {
	use, short := "status [packages|themes]", "Show profile drift"
	if diff {
		use, short = "diff [packages|themes]", "Show semantic differences"
	}
	return &cobra.Command{Use: use, Args: supportedCategory, Short: short, RunE: func(cmd *cobra.Command, args []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if len(args) == 0 && d.Manifest.Capture.Themes {
			return statusAll(cmd.Context(), deps, opt, d, diff)
		}
		if selectedCategory(args) == "themes" {
			return statusThemes(cmd.Context(), deps, opt, d, diff)
		}
		if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
			return err
		}
		current, err := (packagesprovider.Provider{Runner: deps.Runner}).Detect(cmd.Context())
		if err != nil {
			return err
		}
		changes := packagesprovider.Diff(d.Packages, current)
		title := "Profile matches this machine"
		if diff {
			title = "Package differences"
		}
		if len(changes) > 0 && !diff {
			title = fmt.Sprintf("%d package differences", len(changes))
		}
		if err := emit(deps.Out, opt.json, strings.Fields(use)[0], true, map[string]any{"drift": len(changes) > 0, "changes": changes}, renderChanges(title, changes)); err != nil {
			return err
		}
		if len(changes) > 0 {
			return driftError{}
		}
		return nil
	}}
}

func restoreCommand(deps Dependencies, opt *options) *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{Use: "restore [packages|themes]", Args: supportedCategory, Short: "Plan or restore system state", RunE: func(cmd *cobra.Command, args []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if len(args) == 0 && d.Manifest.Capture.Themes {
			return restoreAll(cmd.Context(), deps, opt, d, dryRun, yes)
		}
		if selectedCategory(args) == "themes" {
			return restoreThemes(cmd.Context(), deps, opt, d, dryRun, yes)
		}
		if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
			return err
		}
		info, err := omarchy.Detect(cmd.Context(), deps.Runner)
		if err != nil {
			return err
		}
		provider := packagesprovider.Provider{Runner: deps.Runner}
		current, err := provider.Detect(cmd.Context())
		if err != nil {
			return err
		}
		plan := packagesprovider.Plan(d.Packages, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version)
		if dryRun {
			return emit(deps.Out, opt.json, "restore", true, map[string]any{"dry_run": true, "plan": plan}, renderPlan(plan, true))
		}
		if len(plan.Operations) == 0 {
			human := renderPlan(plan, false) + "All desired packages are installed. No changes applied.\n"
			return emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": model.VerificationResult{OK: true}}, human)
		}
		if !yes {
			if opt.json {
				return errors.New("restore with --json requires --yes or --dry-run")
			}
			fmt.Fprint(deps.Out, renderPlan(plan, false), "Apply this restore? [y/N] ")
			answer, _ := bufio.NewReader(deps.In).ReadString('\n')
			if strings.ToLower(strings.TrimSpace(answer)) != "y" && strings.ToLower(strings.TrimSpace(answer)) != "yes" {
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
		execution, err := restore.Execute(cmd.Context(), deps.Runner, plan, journal, deps.Now, 5*time.Second, progress)
		if err != nil {
			return err
		}
		current, err = provider.Detect(cmd.Context())
		if err != nil {
			return fmt.Errorf("verify restore: %w", err)
		}
		verification := packagesprovider.Verify(d.Packages, current)
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
			return fmt.Errorf("restore completed but verification failed: missing %s", strings.Join(verification.Missing, ", "))
		}
		return emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": verification, "journal": journal.Path}, fmt.Sprintf("Restore verified. Journal: %s\n", journal.Path))
	}}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the restore plan without changing the machine")
	cmd.Flags().BoolVar(&yes, "yes", false, "approve the restore non-interactively")
	return cmd
}

func checkCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "check", Args: cobra.NoArgs, Short: "Validate the profile and environment", RunE: func(cmd *cobra.Command, _ []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
		}
		if err := profile.Validate(d); err != nil {
			return err
		}
		if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
			return err
		}
		info, err := omarchy.Detect(cmd.Context(), deps.Runner)
		if err != nil {
			return err
		}
		if _, err := (packagesprovider.Provider{Runner: deps.Runner}).Detect(cmd.Context()); err != nil {
			return err
		}
		checks := []string{"profile valid", "schema supported", "Omarchy compatible", "package discovery available"}
		if d.Manifest.Capture.Themes {
			provider, err := themeProvider(deps, opt)
			if err != nil {
				return err
			}
			if _, err := provider.Detect(cmd.Context()); err != nil {
				return err
			}
			checks = append(checks, "theme discovery available")
		}
		human := "✓ " + strings.Join(checks, "\n✓ ") + "\n"
		if len(d.Packages.Excluded) > 0 {
			human += fmt.Sprintf("ℹ %d excluded package(s): %s\n", len(d.Packages.Excluded), strings.Join(d.Packages.Excluded, ", "))
		}
		return emit(deps.Out, opt.json, "check", true, map[string]any{"checks": checks, "omarchy": info, "excluded": d.Packages.Excluded}, human)
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
		return emit(deps.Out, opt.json, verb, true, map[string]any{"changed": changed, "excluded": d.Packages.Excluded}, human)
	}}
}

func supportedCategory(_ *cobra.Command, args []string) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "packages" && args[0] != "themes") {
		return errors.New("category must be packages or themes")
	}
	return nil
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

func themeProvider(deps Dependencies, opt *options) (themesprovider.Provider, error) {
	builtin, user, err := deps.ThemeDirs()
	return themesprovider.Provider{Runner: deps.Runner, BuiltinDir: builtin, UserDir: user, ProfileDir: opt.profileDir}, err
}

func captureThemes(ctx context.Context, deps Dependencies, opt *options, d profile.Data) error {
	p, err := themeProvider(deps, opt)
	if err != nil {
		return err
	}
	current, err := p.Capture(ctx)
	if err != nil {
		return err
	}
	info, err := omarchy.Detect(ctx, deps.Runner)
	if err != nil {
		return err
	}
	changes := themesprovider.Diff(d.Themes, current)
	d.Themes, d.Manifest.Capture.Themes, d.Manifest.Profile.UpdatedAt = current, true, deps.Now().UTC()
	d.Manifest.Omarchy.CapturedVersion, d.Manifest.Omarchy.Channel = info.Version, info.Channel
	if err := profile.Save(opt.profileDir, d); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return emit(deps.Out, opt.json, "capture", true, map[string]any{"changes": changes, "themes": current}, renderChanges("Captured theme state", changes))
}

func captureAll(ctx context.Context, deps Dependencies, opt *options, d profile.Data) error {
	if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
		return err
	}
	info, err := omarchy.Detect(ctx, deps.Runner)
	if err != nil {
		return err
	}
	packages, err := (packagesprovider.Provider{Runner: deps.Runner}).Detect(ctx)
	if err != nil {
		return err
	}
	packages = packagesprovider.ApplyExclusions(packages, d.Packages.Excluded)
	themeProvider, err := themeProvider(deps, opt)
	if err != nil {
		return err
	}
	themes, err := themeProvider.Capture(ctx)
	if err != nil {
		return err
	}
	changes := packagesprovider.Diff(d.Packages, packages)
	changes = append(changes, themesprovider.Diff(d.Themes, themes)...)
	d.Packages, d.Themes = packages, themes
	d.Manifest.Capture.Packages, d.Manifest.Capture.Themes = true, true
	d.Manifest.Profile.UpdatedAt = deps.Now().UTC()
	d.Manifest.Omarchy.CapturedVersion, d.Manifest.Omarchy.Channel = info.Version, info.Channel
	if err := profile.Save(opt.profileDir, d); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return emit(deps.Out, opt.json, "capture", true, map[string]any{"changes": changes, "packages": packages, "themes": themes}, renderChanges("Captured package and theme state", changes))
}

func statusThemes(ctx context.Context, deps Dependencies, opt *options, d profile.Data, diff bool) error {
	if !d.Manifest.Capture.Themes {
		return errors.New("theme state has not been captured; run capture themes first")
	}
	p, err := themeProvider(deps, opt)
	if err != nil {
		return err
	}
	current, err := p.Detect(ctx)
	if err != nil {
		return err
	}
	changes := themesprovider.Diff(d.Themes, current)
	title := "Profile theme matches this machine"
	if diff {
		title = "Theme differences"
	} else if len(changes) > 0 {
		title = fmt.Sprintf("%d theme difference(s)", len(changes))
	}
	commandName := "status"
	if diff {
		commandName = "diff"
	}
	if err := emit(deps.Out, opt.json, commandName, true, map[string]any{"drift": len(changes) > 0, "changes": changes}, renderChanges(title, changes)); err != nil {
		return err
	}
	if len(changes) > 0 {
		return driftError{}
	}
	return nil
}

func statusAll(ctx context.Context, deps Dependencies, opt *options, d profile.Data, diff bool) error {
	if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
		return err
	}
	packages, err := (packagesprovider.Provider{Runner: deps.Runner}).Detect(ctx)
	if err != nil {
		return err
	}
	changes := packagesprovider.Diff(d.Packages, packages)
	if d.Manifest.Capture.Themes {
		provider, err := themeProvider(deps, opt)
		if err != nil {
			return err
		}
		current, err := provider.Detect(ctx)
		if err != nil {
			return err
		}
		changes = append(changes, themesprovider.Diff(d.Themes, current)...)
	}
	title := "Profile matches this machine"
	if diff {
		title = "Profile differences"
	} else if len(changes) > 0 {
		title = fmt.Sprintf("%d profile difference(s)", len(changes))
	}
	commandName := "status"
	if diff {
		commandName = "diff"
	}
	if err := emit(deps.Out, opt.json, commandName, true, map[string]any{"drift": len(changes) > 0, "changes": changes}, renderChanges(title, changes)); err != nil {
		return err
	}
	if len(changes) > 0 {
		return driftError{}
	}
	return nil
}

func restoreAll(ctx context.Context, deps Dependencies, opt *options, d profile.Data, dryRun, yes bool) error {
	if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
		return err
	}
	info, err := omarchy.Detect(ctx, deps.Runner)
	if err != nil {
		return err
	}
	packageProvider := packagesprovider.Provider{Runner: deps.Runner}
	currentPackages, err := packageProvider.Detect(ctx)
	if err != nil {
		return err
	}
	plan := packagesprovider.Plan(d.Packages, currentPackages, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version)
	var currentThemes profile.Themes
	var themes themesprovider.Provider
	if d.Manifest.Capture.Themes {
		themes, err = themeProvider(deps, opt)
		if err != nil {
			return err
		}
		currentThemes, err = themes.Detect(ctx)
		if err != nil {
			return err
		}
		themePlan := themes.Plan(d.Themes, currentThemes, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version)
		plan.Operations = append(plan.Operations, themePlan.Operations...)
		plan.Skipped = append(plan.Skipped, themePlan.Skipped...)
	}
	if dryRun {
		return emit(deps.Out, opt.json, "restore", true, map[string]any{"dry_run": true, "plan": plan}, renderPlan(plan, true))
	}
	if len(plan.Operations) == 0 {
		verification := packagesprovider.Verify(d.Packages, currentPackages)
		if d.Manifest.Capture.Themes {
			themeVerification := themesprovider.Verify(d.Themes, currentThemes)
			verification.Missing = append(verification.Missing, themeVerification.Missing...)
			verification.OK = verification.OK && themeVerification.OK
		}
		if !verification.OK {
			return fmt.Errorf("nothing can be restored automatically; verification failed: missing %s", strings.Join(verification.Missing, ", "))
		}
		return emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": verification}, renderPlan(plan, false)+"All captured state is already restored. No changes applied.\n")
	}
	if !yes {
		if opt.json {
			return errors.New("restore with --json requires --yes or --dry-run")
		}
		fmt.Fprint(deps.Out, renderPlan(plan, false), "Apply this restore? [y/N] ")
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
	currentPackages, err = packageProvider.Detect(ctx)
	if err != nil {
		return fmt.Errorf("verify package restore: %w", err)
	}
	verification := packagesprovider.Verify(d.Packages, currentPackages)
	if d.Manifest.Capture.Themes {
		currentThemes, err = themes.Detect(ctx)
		if err != nil {
			return fmt.Errorf("verify theme restore: %w", err)
		}
		themeVerification := themesprovider.Verify(d.Themes, currentThemes)
		verification.Missing = append(verification.Missing, themeVerification.Missing...)
		verification.OK = verification.OK && themeVerification.OK
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
		return fmt.Errorf("restore completed but verification failed: missing %s", strings.Join(verification.Missing, ", "))
	}
	return emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": verification, "journal": journal.Path}, fmt.Sprintf("Restore verified. Journal: %s\n", journal.Path))
}

func restoreThemes(ctx context.Context, deps Dependencies, opt *options, d profile.Data, dryRun, yes bool) error {
	if !d.Manifest.Capture.Themes {
		return errors.New("theme state has not been captured; run capture themes first")
	}
	p, err := themeProvider(deps, opt)
	if err != nil {
		return err
	}
	current, err := p.Detect(ctx)
	if err != nil {
		return err
	}
	info, err := omarchy.Detect(ctx, deps.Runner)
	if err != nil {
		return err
	}
	plan := p.Plan(d.Themes, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version)
	if dryRun {
		return emit(deps.Out, opt.json, "restore", true, map[string]any{"dry_run": true, "plan": plan}, renderPlan(plan, true))
	}
	if len(plan.Operations) == 0 {
		verification := themesprovider.Verify(d.Themes, current)
		if !verification.OK {
			return fmt.Errorf("nothing can be restored automatically; verification failed: missing %s", strings.Join(verification.Missing, ", "))
		}
		return emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": verification}, renderPlan(plan, false)+"Theme already active. No changes applied.\n")
	}
	if !yes {
		if opt.json {
			return errors.New("restore with --json requires --yes or --dry-run")
		}
		fmt.Fprint(deps.Out, renderPlan(plan, false), "Apply this restore? [y/N] ")
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
	execution, err := restore.Execute(ctx, deps.Runner, plan, journal, deps.Now, 5*time.Second, nil)
	if err != nil {
		return err
	}
	if len(execution.Failed) > 0 {
		return fmt.Errorf("restore completed with %d failed operation(s)", len(execution.Failed))
	}
	current, err = p.Detect(ctx)
	if err != nil {
		return fmt.Errorf("verify restore: %w", err)
	}
	verification := themesprovider.Verify(d.Themes, current)
	if !verification.OK {
		return fmt.Errorf("restore completed but verification failed: missing %s", strings.Join(verification.Missing, ", "))
	}
	return emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": verification, "journal": journal.Path}, fmt.Sprintf("Restore verified. Journal: %s\n", journal.Path))
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
	for _, skipped := range plan.Skipped {
		fmt.Fprintf(&b, "- skip %s (%s)\n", skipped.Resource, skipped.Reason)
	}
	return b.String()
}

func renderProgress(w io.Writer, event restore.Progress) {
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
	if len(verification.Missing) > 0 {
		fmt.Fprintf(w, "Still missing: %s\n", strings.Join(verification.Missing, ", "))
	}
	fmt.Fprintf(w, "Journal: %s\n", journal)
}
