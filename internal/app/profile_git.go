package app

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
)

func profileCommand(deps Dependencies, opt *options) *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Manage the active profile"}
	cmd.AddCommand(profileGitCommand(deps, opt))
	return cmd
}

func profileGitCommand(deps Dependencies, opt *options) *cobra.Command {
	cmd := &cobra.Command{Use: "git", Short: "Synchronize the profile repository"}
	cmd.AddCommand(
		profileGitStatusCommand(deps, opt),
		profileGitInitCommand(deps, opt),
		profileGitRemoteCommand(deps, opt),
		profileGitFetchCommand(deps, opt),
		profileGitDiffCommand(deps, opt),
		profileGitCommitCommand(deps, opt),
		profileGitPullCommand(deps, opt),
		profileGitPushCommand(deps, opt),
	)
	return cmd
}

func profileGitService(deps Dependencies, opt *options) (profilegit.Service, error) {
	return profilegit.New(deps.Runner, opt.profileDir)
}

func profileGitStatusCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "status", Args: cobra.NoArgs, Short: "Show profile repository status", RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := profileGitService(deps, opt)
		if err != nil {
			return err
		}
		status, err := service.Status(cmd.Context())
		if err != nil {
			return err
		}
		return emit(deps.Out, opt.json, "profile git status", true, map[string]any{"profilegit": status}, renderProfileGitStatus(status))
	}}
}

func profileGitInitCommand(deps Dependencies, opt *options) *cobra.Command {
	return profileGitResultCommand(deps, opt, "init", "Initialize a profile repository", func(cmd *cobra.Command, service profilegit.Service) (profilegit.Result, error) {
		return service.Init(cmd.Context())
	})
}

func profileGitRemoteCommand(deps Dependencies, opt *options) *cobra.Command {
	cmd := &cobra.Command{Use: "remote", Args: cobra.NoArgs, Short: "Show the profile origin", RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := profileGitService(deps, opt)
		if err != nil {
			return err
		}
		origin, err := service.Remote(cmd.Context())
		if err != nil {
			return err
		}
		human := "Profile Git origin: not configured\n"
		if origin != "" {
			human = "Profile Git origin: " + origin + "\n"
		}
		return emit(deps.Out, opt.json, "profile git remote", true, map[string]any{"origin": origin}, human)
	}}
	cmd.AddCommand(
		&cobra.Command{Use: "set <url>", Args: cobra.ExactArgs(1), Short: "Set the profile origin", RunE: func(cmd *cobra.Command, args []string) error {
			service, err := profileGitService(deps, opt)
			if err != nil {
				return err
			}
			result, err := service.SetRemote(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return emitProfileGitResult(deps, opt, "profile git remote set", result, "Profile Git origin updated.\n")
		}},
		profileGitResultCommand(deps, opt, "remove", "Remove the profile origin", func(cmd *cobra.Command, service profilegit.Service) (profilegit.Result, error) {
			return service.RemoveRemote(cmd.Context())
		}),
	)
	return cmd
}

func profileGitFetchCommand(deps Dependencies, opt *options) *cobra.Command {
	return profileGitResultCommand(deps, opt, "fetch", "Fetch the profile origin", func(cmd *cobra.Command, service profilegit.Service) (profilegit.Result, error) {
		return service.Fetch(cmd.Context())
	})
}

func profileGitDiffCommand(deps Dependencies, opt *options) *cobra.Command {
	return &cobra.Command{Use: "diff [path]", Args: cobra.MaximumNArgs(1), Short: "Show managed profile changes", RunE: func(cmd *cobra.Command, args []string) error {
		service, err := profileGitService(deps, opt)
		if err != nil {
			return err
		}
		path := ""
		if len(args) != 0 {
			path = args[0]
		}
		diff, err := service.Diff(cmd.Context(), path)
		if err != nil {
			return err
		}
		var human strings.Builder
		human.WriteString("Profile Git changes\n")
		if len(diff.Files) == 0 {
			human.WriteString("No managed changes.\n")
		} else {
			for _, file := range diff.Files {
				fmt.Fprintf(&human, "\n%s\n", profilegit.SanitizeDisplay(file.Path))
				renderProfileGitDocument(&human, file.Document)
			}
		}
		return emit(deps.Out, opt.json, "profile git diff", true, map[string]any{"profilegit": diff}, human.String())
	}}
}

func renderProfileGitDocument(human *strings.Builder, document inspection.DiffDocument) {
	if document.Kind == inspection.DiffText {
		for _, hunk := range document.Hunks {
			fmt.Fprintf(human, "@@ -%d,%d +%d,%d @@\n", hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
			for _, line := range hunk.Lines {
				prefix := " "
				if line.Kind == "add" {
					prefix = "+"
				}
				if line.Kind == "remove" {
					prefix = "-"
				}
				fmt.Fprintf(human, "%s%s\n", prefix, line.Text)
			}
		}
		return
	}
	fmt.Fprintf(human, "  %s\n", document.Kind)
	for _, fact := range document.Metadata {
		fmt.Fprintf(human, "  %s: %s\n", profilegit.SanitizeDisplay(fact.Key), profilegit.SanitizeDisplay(fact.Value))
	}
}

func profileGitCommitCommand(deps Dependencies, opt *options) *cobra.Command {
	var message string
	cmd := profileGitResultCommand(deps, opt, "commit", "Commit managed profile changes", func(cmd *cobra.Command, service profilegit.Service) (profilegit.Result, error) {
		return service.Commit(cmd.Context(), message)
	})
	cmd.Flags().StringVarP(&message, "message", "m", "", "commit message")
	return cmd
}

func profileGitPullCommand(deps Dependencies, opt *options) *cobra.Command {
	return profileGitResultCommand(deps, opt, "pull", "Fast-forward the profile repository", func(cmd *cobra.Command, service profilegit.Service) (profilegit.Result, error) {
		return service.Pull(cmd.Context())
	})
}

func profileGitPushCommand(deps Dependencies, opt *options) *cobra.Command {
	return profileGitResultCommand(deps, opt, "push", "Push the profile repository", func(cmd *cobra.Command, service profilegit.Service) (profilegit.Result, error) {
		return service.Push(cmd.Context())
	})
}

func profileGitResultCommand(deps Dependencies, opt *options, use, short string, run func(*cobra.Command, profilegit.Service) (profilegit.Result, error)) *cobra.Command {
	return &cobra.Command{Use: use, Args: cobra.NoArgs, Short: short, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := profileGitService(deps, opt)
		if err != nil {
			return err
		}
		result, err := run(cmd, service)
		if err != nil {
			return err
		}
		return emitProfileGitResult(deps, opt, "profile git "+strings.Fields(use)[0], result, profileGitResultHuman(use, result))
	}}
}

func emitProfileGitResult(deps Dependencies, opt *options, command string, result profilegit.Result, human string) error {
	return emit(deps.Out, opt.json, command, true, map[string]any{"profilegit": result}, human)
}

func profileGitResultHuman(use string, result profilegit.Result) string {
	verb := strings.Fields(use)[0]
	if !result.Changed {
		return "Profile Git " + verb + ": no changes.\n"
	}
	if result.Commit != "" {
		return "Profile Git commit: " + result.Commit + "\n"
	}
	return "Profile Git " + verb + " complete.\n"
}

func renderProfileGitStatus(status profilegit.Status) string {
	var b strings.Builder
	b.WriteString("Profile Git\n")
	if !status.Repository {
		b.WriteString("  repository: no\n")
		b.WriteString("Run `omarchy-blueprint profile git init` to initialize this profile repository.\n")
		return b.String()
	}
	b.WriteString("  repository: yes\n")
	if status.Branch != "" {
		fmt.Fprintf(&b, "  branch: %s\n", profilegit.SanitizeDisplay(status.Branch))
	} else {
		b.WriteString("  branch: detached\n")
	}
	if status.Origin != "" {
		fmt.Fprintf(&b, "  origin: %s\n", profilegit.SanitizeDisplay(status.Origin))
	} else {
		b.WriteString("  origin: not configured\n")
	}
	if status.Upstream != "" {
		fmt.Fprintf(&b, "  upstream: %s\n", profilegit.SanitizeDisplay(status.Upstream))
	}
	fmt.Fprintf(&b, "  ahead/behind: %d/%d\n", status.Ahead, status.Behind)
	b.WriteString("\nChanges\n")
	if len(status.Changes) == 0 {
		b.WriteString("  No changes.\n")
		return b.String()
	}
	for _, change := range status.Changes {
		state := change.Index + change.Worktree
		if state == "" {
			state = "?"
		}
		kind := "unmanaged"
		if change.Managed {
			kind = "managed"
		}
		fmt.Fprintf(&b, "  %-2s %-30s %s\n", state, profilegit.SanitizeDisplay(change.Path), kind)
	}
	return b.String()
}
