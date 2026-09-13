package tui

import "testing"

func TestActionRegistryKeepsNavigationSearchOnlyInitially(t *testing.T) {
	registry := ActionRegistry{Actions: []Action{
		{ID: "refresh", Label: "Refresh", PaletteInitial: true},
		{ID: "screen.sync", Label: "Sync", Group: "Navigate", Keywords: "screen"},
	}, Bindings: []Binding{{ActionID: "refresh", Key: "r"}}}
	if initial := registry.Initial(); len(initial) != 1 || initial[0].ID != "refresh" {
		t.Fatalf("initial actions=%#v", initial)
	}
	if matches := registry.Search("sync"); len(matches) != 1 || matches[0].ID != "screen.sync" {
		t.Fatalf("search actions=%#v", matches)
	}
	if key := registry.Key("refresh"); key != "r" {
		t.Fatalf("binding key=%q", key)
	}
}
