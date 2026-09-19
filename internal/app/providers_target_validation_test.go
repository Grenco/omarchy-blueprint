package app

import "testing"

func TestPackagesValidateTarget(t *testing.T) {
	cases := map[string]bool{
		"official:firefox":           true,
		"aur:visual-studio-code-bin": true,
		"mise:node":                  true,
		"firefox":                    false,
		"bogus:firefox":              false,
		"official:":                  false,
		"official:has space":         false,
		"":                           false,
	}
	for target, want := range cases {
		_, err := (packagesStateProvider{}).ValidateTarget(target)
		if got := err == nil; got != want {
			t.Errorf("ValidateTarget(%q) accepted=%v, want %v (err=%v)", target, got, want, err)
		}
	}
}

func TestThemesValidateTarget(t *testing.T) {
	cases := map[string]bool{
		"active":          true,
		"theme:nord":      true,
		"nord":            false,
		"theme:":          false,
		"theme:has space": false,
		"theme:a/b":       false,
	}
	for target, want := range cases {
		_, err := (themesStateProvider{}).ValidateTarget(target)
		if got := err == nil; got != want {
			t.Errorf("ValidateTarget(%q) accepted=%v, want %v (err=%v)", target, got, want, err)
		}
	}
}

func TestPluginsValidateTarget(t *testing.T) {
	cases := map[string]bool{
		"plugin:acme": true,
		"acme":        false,
		"plugin:":     false,
		"plugin:a/b":  false,
	}
	for target, want := range cases {
		_, err := (pluginsStateProvider{}).ValidateTarget(target)
		if got := err == nil; got != want {
			t.Errorf("ValidateTarget(%q) accepted=%v, want %v (err=%v)", target, got, want, err)
		}
	}
}

func TestResourcesValidateTarget(t *testing.T) {
	cases := map[string]bool{
		"resource:dotfiles": true,
		"dotfiles":          false,
		"resource:":         false,
		"resource:a/b":      false,
	}
	for target, want := range cases {
		_, err := (resourcesStateProvider{}).ValidateTarget(target)
		if got := err == nil; got != want {
			t.Errorf("ValidateTarget(%q) accepted=%v, want %v (err=%v)", target, got, want, err)
		}
	}
}

func TestDefaultsValidateTarget(t *testing.T) {
	cases := map[string]bool{
		"terminal": true,
		"browser":  true,
		"editor":   true,
		"agent":    true,
		"bogus":    false,
		"":         false,
	}
	for target, want := range cases {
		_, err := (defaultsStateProvider{}).ValidateTarget(target)
		if got := err == nil; got != want {
			t.Errorf("ValidateTarget(%q) accepted=%v, want %v (err=%v)", target, got, want, err)
		}
	}
}

func TestShellValidateTarget(t *testing.T) {
	if _, err := (shellStateProvider{}).ValidateTarget("state"); err != nil {
		t.Fatalf("ValidateTarget(state) = %v, want accepted", err)
	}
	if _, err := (shellStateProvider{}).ValidateTarget("bogus"); err == nil {
		t.Fatal("bogus target accepted")
	}
}

func TestHooksValidateTarget(t *testing.T) {
	cases := map[string]bool{
		"post-boot":                 true,
		"post-update.d/update-rust": true,
		"":                          false,
		"../escape":                 false,
		"post-update.d/.hidden":     false,
	}
	for target, want := range cases {
		_, err := (hooksStateProvider{}).ValidateTarget(target)
		if got := err == nil; got != want {
			t.Errorf("ValidateTarget(%q) accepted=%v, want %v (err=%v)", target, got, want, err)
		}
	}
}

func TestConfigValidateTargetCanonicalizes(t *testing.T) {
	got, err := (configStateProvider{}).ValidateTarget("~/.config/nvim/init.lua")
	if err != nil {
		t.Fatal(err)
	}
	if got != ".config/nvim/init.lua" {
		t.Fatalf("canonical = %q, want .config/nvim/init.lua", got)
	}
	if _, err := (configStateProvider{}).ValidateTarget("~/.config"); err == nil {
		t.Fatal("bare ~/.config accepted")
	}
}
