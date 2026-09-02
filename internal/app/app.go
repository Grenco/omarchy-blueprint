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
	"github.com/graeme/omarchy-blueprint/internal/restore"
)

type Dependencies struct {
	Runner    command.Runner
	In        io.Reader
	Out       io.Writer
	Err       io.Writer
	Now       func() time.Time
	StateHome func() (string, error)
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
	root.AddCommand(initCommand(deps, opt), captureCommand(deps, opt), statusCommand(deps, opt, false), statusCommand(deps, opt, true), restoreCommand(deps, opt), checkCommand(deps, opt))
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
	return &cobra.Command{Use: "capture [packages]", Args: onlyPackages, Short: "Capture package state", RunE: func(cmd *cobra.Command, _ []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
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
	use, short := "status [packages]", "Show package drift"
	if diff {
		use, short = "diff [packages]", "Show semantic package differences"
	}
	return &cobra.Command{Use: use, Args: onlyPackages, Short: short, RunE: func(cmd *cobra.Command, _ []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
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
	cmd := &cobra.Command{Use: "restore [packages]", Args: onlyPackages, Short: "Plan or restore package state", RunE: func(cmd *cobra.Command, _ []string) error {
		d, err := profile.Load(opt.profileDir)
		if err != nil {
			return profileError(opt.profileDir, err)
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
			return emit(deps.Out, opt.json, "restore", true, map[string]any{"plan": plan, "verification": model.VerificationResult{OK: true}}, "Nothing to restore; package state is satisfied.\n")
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
		if err := restore.Execute(cmd.Context(), deps.Runner, plan, journal, deps.Now, 5*time.Second, progress); err != nil {
			return err
		}
		current, err = provider.Detect(cmd.Context())
		if err != nil {
			return fmt.Errorf("verify restore: %w", err)
		}
		verification := packagesprovider.Verify(d.Packages, current)
		_ = journal.Write(restore.Event{Time: deps.Now().UTC(), Type: "VERIFY_COMPLETED", Message: fmt.Sprintf("ok=%t", verification.OK)})
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
		info, err := omarchy.Detect(cmd.Context(), deps.Runner)
		if err != nil {
			return err
		}
		if _, err := (packagesprovider.Provider{Runner: deps.Runner}).Detect(cmd.Context()); err != nil {
			return err
		}
		checks := []string{"profile valid", "schema supported", "Omarchy compatible", "package discovery available"}
		return emit(deps.Out, opt.json, "check", true, map[string]any{"checks": checks, "omarchy": info}, "✓ "+strings.Join(checks, "\n✓ ")+"\n")
	}}
}

func onlyPackages(_ *cobra.Command, args []string) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "packages") {
		return errors.New("this milestone supports only the packages category")
	}
	return nil
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
		fmt.Fprintf(&b, "+ install %s (risk: %s, automatic rollback: no)\n", op.Resource, op.Risk)
	}
	for _, skipped := range plan.Skipped {
		fmt.Fprintf(&b, "- skip %s (%s)\n", skipped.Resource, skipped.Reason)
	}
	return b.String()
}

func renderProgress(w io.Writer, event restore.Progress) {
	kind := strings.TrimPrefix(event.Operation.ID, "packages.install.")
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
