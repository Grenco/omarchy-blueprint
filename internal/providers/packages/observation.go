package packages

import (
	"context"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Observation owns the complete Detect result, including live scratch facts.
// It is descriptive only; authoritative methods must call Observe afresh.
type Observation struct{ packages profile.Packages }

func (p Provider) Observe(ctx context.Context) (Observation, error) {
	facts, err := p.Detect(ctx)
	if canceled := ctx.Err(); canceled != nil {
		return Observation{}, canceled
	}
	if err != nil {
		return Observation{}, err
	}
	return Observation{packages: profile.ClonePackages(facts)}, nil
}
func (o Observation) Clone() Observation {
	return Observation{packages: profile.ClonePackages(o.packages)}
}
func (o Observation) Packages() profile.Packages { return profile.ClonePackages(o.packages) }
