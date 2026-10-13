package app

import (
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"github.com/spf13/cobra"
	"sort"
	"strings"
	"unicode"
)

func safeDisplay(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, value)
}

func syncCommand(deps Dependencies, opt *options) *cobra.Command {
	requireInspection := func(*cobra.Command, []string) error { return fmt.Errorf("sync: choose a read-only inspect command") }
	root := &cobra.Command{Use: "sync", Short: "Inspect semantic reconciliation (read-only)", Args: cobra.NoArgs, RunE: requireInspection}
	inspection := func(bootstrap bool) *cobra.Command {
		noFetch := false
		cmd := &cobra.Command{Use: "inspect", Short: "Inspect pinned profile revisions and live machine intent without applying changes", Args: cobra.NoArgs}
		cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "use cached upstream refs without network access")
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			session, err := openWorkflow(deps, opt)
			if err != nil {
				return err
			}
			options := workflow.SyncInspectOptions{Fetch: !noFetch}
			if bootstrap {
				plan, err := session.InspectSyncBootstrap(cmd.Context(), options)
				if err != nil {
					return err
				}
				return emit(deps.Out, opt.json, "sync bootstrap inspect", true, map[string]any{"sync": plan}, formatSyncBootstrap(plan))
			}
			plan, err := session.InspectSync(cmd.Context(), options)
			if err != nil {
				return err
			}
			return emit(deps.Out, opt.json, "sync inspect", true, map[string]any{"sync": plan}, formatSyncPlan(plan))
		}
		return cmd
	}
	bootstrap := &cobra.Command{Use: "bootstrap", Short: "Inspect baseless first-Sync choices", Args: cobra.NoArgs, RunE: requireInspection}
	bootstrap.AddCommand(inspection(true))
	root.AddCommand(inspection(false), bootstrap)
	return root
}

func syncShortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	if sha == "" {
		return "(none)"
	}
	return sha
}
func syncHeader(id syncengine.Identity, git syncengine.GitState) string {
	return fmt.Sprintf("Sync inspection — read-only, not execution authority\nB %s  L %s  R %s\nMachine %s  Branch %s  Upstream %s\nClean %t  Ahead %d  Behind %d\n", syncShortSHA(id.Base), syncShortSHA(id.Local), syncShortSHA(id.Remote), safeDisplay(id.Machine), safeDisplay(id.Branch), safeDisplay(id.Upstream), git.Clean, git.Ahead, git.Behind)
}
func syncBlockerText(bs []syncengine.Blocker) string {
	if len(bs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nBlockers\n")
	for _, x := range bs {
		fmt.Fprintf(&b, "  %s: %s", safeDisplay(x.Code), safeDisplay(x.Message))
		if x.Target != nil {
			fmt.Fprintf(&b, " [%s:%s]", safeDisplay(x.Target.Category), safeDisplay(x.Target.Target))
		}
		b.WriteByte('\n')
	}
	return b.String()
}
func formatSyncPlan(p syncengine.Plan) string {
	var b strings.Builder
	b.WriteString(syncHeader(p.Identity, p.Git))
	if p.Control != nil {
		fmt.Fprintf(&b, "\nControl plane\n  history=%s selected=%s (separate from runtime targets)\n", safeDisplay(string(p.Control.History)), safeDisplay(string(p.Control.SelectedSource)))
	}
	b.WriteString(syncBlockerText(p.Blockers))
	items := append([]syncengine.Item(nil), p.Items...)
	sort.Slice(items, func(i, j int) bool {
		a, c := items[i].Key, items[j].Key
		if a.Category != c.Category {
			return a.Category < c.Category
		}
		return a.Target < c.Target
	})
	for _, group := range []struct {
		class syncengine.HistoryClass
		label string
	}{{syncengine.HistoryConflict, "Conflicts"}, {syncengine.HistoryLocal, "Local"}, {syncengine.HistoryRemote, "Remote"}, {syncengine.HistoryConverged, "Converged"}, {syncengine.HistoryUnchanged, "Unchanged"}} {
		header := false
		for _, x := range items {
			if x.History != group.class {
				continue
			}
			if !header {
				fmt.Fprintf(&b, "\n%s\n", group.label)
				header = true
			}
			fmt.Fprintf(&b, "  %s:%s  desired=%s machine=%s review=%s", safeDisplay(x.Key.Category), safeDisplay(x.Key.Target), x.Desired.Kind, x.Machine.Kind, x.Review)
			for _, action := range x.Actions {
				fmt.Fprintf(&b, " candidate=%s", action)
			}
			if x.Inapplicable {
				b.WriteString(" policy-inapplicable")
			}
			if x.Deferred {
				b.WriteString(" semantic-defer")
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}
func formatSyncBootstrap(p workflow.SyncBootstrapPlan) string {
	var b strings.Builder
	b.WriteString(syncHeader(p.Identity, p.Git))
	b.WriteString(syncBlockerText(p.Blockers))
	fmt.Fprintf(&b, "\nBootstrap convergence eligible: %t (inspection never writes the cursor)\n", p.ConvergenceEligible)
	for _, x := range p.Items {
		fmt.Fprintf(&b, "  %s:%s  local=%s upstream=%s machine=%s converged=%t\n", safeDisplay(x.Key.Category), safeDisplay(x.Key.Target), x.Local.Kind, x.Remote.Kind, x.Machine.Kind, x.Converged)
	}
	b.WriteString("\nChoices (not executed)\n")
	for _, choice := range p.Choices {
		fmt.Fprintf(&b, "  %s\n", choice)
		for _, requirement := range p.ChoiceRequirements[choice] {
			fmt.Fprintf(&b, "    %s\n", requirement)
		}
	}
	return b.String()
}
