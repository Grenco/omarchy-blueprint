package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/graeme/omarchy-blueprint/internal/command"
	"github.com/graeme/omarchy-blueprint/internal/model"
	"github.com/graeme/omarchy-blueprint/internal/profile"
)

type Provider struct{ Runner command.Runner }
type catalogItem struct {
	ID         string `json:"id"`
	Enabled    bool   `json:"enabled"`
	FirstParty bool   `json:"firstParty"`
	CanDisable bool   `json:"canDisable"`
}

func (p Provider) Detect(ctx context.Context) (profile.Plugins, error) {
	out, err := p.Runner.Run(ctx, "omarchy", "plugin", "list", "--json")
	if err != nil {
		return profile.Plugins{}, fmt.Errorf("detect Omarchy plugins: %w", err)
	}
	var catalog []catalogItem
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		return profile.Plugins{}, fmt.Errorf("parse Omarchy plugin catalog: %w", err)
	}
	state := profile.Plugins{}
	for _, item := range catalog {
		if item.FirstParty && item.CanDisable {
			state.Items = append(state.Items, profile.Plugin{ID: item.ID, Enabled: item.Enabled})
		}
	}
	sort.Slice(state.Items, func(i, j int) bool { return state.Items[i].ID < state.Items[j].ID })
	return state, nil
}

func Diff(saved, current profile.Plugins) []model.Change {
	have := pluginMap(current.Items)
	var out []model.Change
	for _, want := range saved.Items {
		if got, ok := have[want.ID]; !ok || got.Enabled != want.Enabled {
			out = append(out, model.Change{Type: model.ChangeAdd, Provider: "plugins", Kind: "enabled", Name: want.ID, Summary: fmt.Sprintf("~ plugin %s enabled: %t → %t", want.ID, got.Enabled, want.Enabled)})
		}
	}
	return out
}

func Plan(saved, current profile.Plugins, schema int, from, to string) model.RestorePlan {
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	have := pluginMap(current.Items)
	for _, want := range saved.Items {
		got, ok := have[want.ID]
		if !ok {
			plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "plugins", Resource: "plugin:" + want.ID, Reason: "first-party plugin unavailable in this Omarchy version"})
			continue
		}
		if got.Enabled == want.Enabled {
			continue
		}
		action := "disable"
		if want.Enabled {
			action = "enable"
		}
		plan.Operations = append(plan.Operations, model.Operation{ID: "plugins." + action + "." + want.ID, Provider: "plugins", Action: action, Resource: "plugin:" + want.ID, Items: []string{want.ID}, Command: []string{"omarchy", "plugin", action, want.ID}, Risk: model.RiskLow, Reversible: true})
	}
	return plan
}

func Verify(saved, current profile.Plugins) model.VerificationResult {
	have := pluginMap(current.Items)
	var missing []string
	for _, want := range saved.Items {
		got, ok := have[want.ID]
		if !ok || got.Enabled != want.Enabled {
			missing = append(missing, "plugin:"+want.ID)
		}
	}
	return model.VerificationResult{OK: len(missing) == 0, Missing: missing}
}
func pluginMap(items []profile.Plugin) map[string]profile.Plugin {
	out := map[string]profile.Plugin{}
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}
