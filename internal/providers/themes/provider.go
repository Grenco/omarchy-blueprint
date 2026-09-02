package themes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/graeme/omarchy-blueprint/internal/command"
	"github.com/graeme/omarchy-blueprint/internal/model"
	"github.com/graeme/omarchy-blueprint/internal/profile"
)

type Provider struct {
	Runner     command.Runner
	BuiltinDir string
	UserDir    string
}

func (p Provider) Detect(ctx context.Context) (profile.Themes, error) {
	out, err := p.Runner.Run(ctx, "omarchy", "theme", "current")
	if err != nil {
		return profile.Themes{}, fmt.Errorf("detect current Omarchy theme: %w", err)
	}
	id := slug(strings.TrimSpace(out))
	if id == "" || id == "unknown" {
		return profile.Themes{}, fmt.Errorf("Omarchy did not report an active theme")
	}
	if exists(filepath.Join(p.UserDir, id)) {
		return profile.Themes{Current: id, Source: "user"}, nil
	}
	if !exists(filepath.Join(p.BuiltinDir, id)) {
		return profile.Themes{Current: id, Source: "unknown"}, nil
	}
	return profile.Themes{Current: id, Source: "builtin"}, nil
}

func ValidateCapture(theme profile.Themes) error {
	if theme.Source != "builtin" {
		return fmt.Errorf("active theme %q is user-installed or locally overridden; capturing its content is not supported yet", theme.Current)
	}
	return nil
}

func Diff(saved, current profile.Themes) []model.Change {
	if saved.Current == "" || (saved.Current == current.Current && saved.Source == current.Source) {
		return nil
	}
	return []model.Change{{Type: model.ChangeAdd, Provider: "themes", Kind: "active", Name: current.Current, Summary: fmt.Sprintf("~ active theme %s (%s) → %s (%s)", saved.Current, saved.Source, current.Current, current.Source)}}
}

func Plan(saved, current profile.Themes, schema int, from, to string) model.RestorePlan {
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	if saved.Current == current.Current && saved.Source != current.Source {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "themes", Resource: "theme:" + saved.Current, Reason: "user theme overrides saved built-in; automatic removal disabled"})
		return plan
	}
	if saved.Current != "" && saved.Current != current.Current {
		plan.Operations = append(plan.Operations, model.Operation{ID: "themes.activate." + saved.Current, Provider: "themes", Action: "activate", Resource: "theme:" + saved.Current, Items: []string{saved.Current}, Command: []string{"omarchy", "theme", "set", saved.Current}, Risk: model.RiskLow, Reversible: true})
	}
	return plan
}

func Verify(saved, current profile.Themes) model.VerificationResult {
	if saved.Current == "" || (saved.Current == current.Current && saved.Source == current.Source) {
		return model.VerificationResult{OK: true}
	}
	return model.VerificationResult{OK: false, Missing: []string{fmt.Sprintf("theme:%s (source %s)", saved.Current, saved.Source)}}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
