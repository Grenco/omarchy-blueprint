package config

import (
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestRelatedConfigExactTopLevelAssociation(t *testing.T) {
	got := RelatedConfig([]string{"ghostty"}, profile.Configs{Files: []profile.ConfigFile{{Path: "ghostty/config"}}})
	want := []Association{{Package: "ghostty", ConfigPath: "ghostty", Confidence: "high"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RelatedConfig() = %#v, want %#v", got, want)
	}
}

func TestRelatedConfigCuratedNeovimAssociation(t *testing.T) {
	got := RelatedConfig([]string{"official:neovim"}, profile.Configs{Files: []profile.ConfigFile{{Path: "nvim/init.lua"}}})
	want := []Association{{Package: "official:neovim", ConfigPath: "nvim", Confidence: "high"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RelatedConfig() = %#v, want %#v", got, want)
	}
}

func TestRelatedConfigSuppressesExcludedAndUnrelatedState(t *testing.T) {
	config := profile.Configs{
		Files:    []profile.ConfigFile{{Path: "nvim/init.lua"}, {Path: "ghostty/config"}},
		Excluded: []string{"nvim"},
	}
	want := []Association{{Package: "ghostty", ConfigPath: "ghostty", Confidence: "high"}}
	got := RelatedConfig([]string{"neovim", "ghostty", "kitty"}, config)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RelatedConfig() = %#v, want %#v", got, want)
	}
}
