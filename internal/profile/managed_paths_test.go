package profile

import "testing"

func TestIsManagedRepositoryPath(t *testing.T) {
	cases := map[string]bool{
		"profile.toml":              true,
		"resources/resources.toml":  true,
		"machines/desktop.toml":     true,
		"config/hypr/bindings.lua":  true,
		"README.md":                 false,
		".git/config":               false,
		"../outside":                false,
		"resources/../profile.toml": false,
		"resources//resources.toml": false,
		"resources\\resources.toml": false,
		"/resources/resources.toml": false,
		"":                          false,
	}
	for path, want := range cases {
		if got := IsManagedRepositoryPath(path); got != want {
			t.Errorf("IsManagedRepositoryPath(%q) = %v, want %v", path, got, want)
		}
	}
}
