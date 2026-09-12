package screens

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/providers/config"
)

func TestConfigScreenShowsProviderClassificationAndExactReason(t *testing.T) {
	screen := &Config{width: 80, candidates: []config.Candidate{
		{Path: ".config/gh/hosts.yml", Classification: config.ConfigSensitive, Reason: string(config.PolicySensitive)},
		{Path: ".config/nvim/init.lua", Classification: config.ConfigModifiedBaseline, Reason: "modified-baseline"},
	}}
	view := screen.View()
	if !strings.Contains(view, "> .config/gh/hosts.yml  sensitive (sensitive)") || !strings.Contains(view, "Reason: sensitive") {
		t.Fatalf("view=%q", view)
	}
}
