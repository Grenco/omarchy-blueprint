package omarchy

// AppRecipe selects an Omarchy-owned application lifecycle instead of
// treating a semantically configured application as only a package.
type AppRecipe struct {
	ID            string
	Install       []string
	Remove        []string
	Interactive   bool
	InstallNotice string
	RemoveNotice  string
	Verify        [][]string
}

// SemanticRecipe returns the small, reviewed set of applications for which
// Omarchy owns meaningful setup beyond package installation. Authentication
// data is deliberately not represented here or in the portable profile.
func SemanticRecipe(id string) (AppRecipe, bool) {
	switch id {
	case "tailscale":
		return AppRecipe{
			ID:            id,
			Install:       []string{"omarchy-install-service-tailscale"},
			Remove:        []string{"omarchy-remove-service-tailscale"},
			Interactive:   true,
			InstallNotice: "Tailscale setup requires interactive device authentication; credentials are not stored by Blueprint.",
			RemoveNotice:  "Tailscale removal uses Omarchy's interactive service teardown.",
			Verify:        [][]string{{"systemctl", "is-enabled", "tailscaled"}, {"tailscale", "status"}},
		}, true
	default:
		return AppRecipe{}, false
	}
}
