package machine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Selection identifies the active machine overlay and how it was chosen.
type Selection struct {
	Name    string
	Source  string // explicit | binding | default
	Machine *profile.Machine
}

var invalidSuggestionCharacter = regexp.MustCompile(`[^a-z0-9_.-]+`)

// SuggestName derives an available machine name from hostname.
func SuggestName(hostname string, existing []profile.Machine) string {
	name := strings.ToLower(strings.TrimSpace(hostname))
	name = invalidSuggestionCharacter.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-._")
	if name == "" || name == "localhost" || ValidateName(name) != nil {
		name = "machine"
	}

	used := make(map[string]bool, len(existing))
	for _, machine := range existing {
		used[machine.Name] = true
	}
	if !used[name] {
		return name
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", name, suffix)
		if !used[candidate] {
			return candidate
		}
	}
}

// Select resolves explicit selection ahead of the local binding.
func Select(explicit, bound string, machines []profile.Machine) (Selection, error) {
	if explicit != "" {
		machine := machineByName(explicit, machines)
		if machine == nil {
			return Selection{}, fmt.Errorf("machine %q does not exist", explicit)
		}
		return Selection{Name: explicit, Source: "explicit", Machine: machine}, nil
	}
	if bound != "" {
		machine := machineByName(bound, machines)
		if machine == nil {
			return Selection{}, fmt.Errorf("bound machine %q no longer exists; run `machine use <name>` or `machine clear`", bound)
		}
		return Selection{Name: bound, Source: "binding", Machine: machine}, nil
	}
	return Selection{Source: "default"}, nil
}

func machineByName(name string, machines []profile.Machine) *profile.Machine {
	for index := range machines {
		if machines[index].Name == name {
			return &machines[index]
		}
	}
	return nil
}
