package shell

import (
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Plan builds a conservative Shell restore: the exact captured document is
// written only when the target is missing or still semantically the Omarchy
// default. A changed Omarchy baseline does not block restore — a user
// shell.json is authoritative and not deep-merged — but an incompatible Shell
// JSON version requires migration, and unknown user customization is never
// overwritten.
func (p Provider) Plan(saved profile.Shell, current State, schema int, from, to string) (model.RestorePlan, error) {
	plan := model.RestorePlan{ProfileVersion: schema, OmarchyFrom: from, OmarchyTo: to}
	if saved.Hash == "" {
		if current.Status == StatusCustomized {
			plan.Skipped = append(plan.Skipped, model.Skipped{
				Provider: "shell",
				Resource: "shell:config",
				Reason:   "additional Shell customization left in place; removal disabled",
			})
		}
		return plan, nil
	}
	if err := p.checkSnapshots(saved); err != nil {
		return model.RestorePlan{}, err
	}
	if current.Status == StatusUnsupported || current.Version != saved.Version {
		plan.Skipped = append(plan.Skipped, model.Skipped{
			Provider: "shell",
			Resource: "shell:config",
			Reason:   fmt.Sprintf("Shell schema changed; migration required (current version %d, captured %d)", current.Version, saved.Version),
		})
		return plan, nil
	}
	if current.Status == StatusCustomized && current.Hash == saved.Hash {
		return plan, nil
	}
	if current.Status == StatusCustomized {
		plan.Skipped = append(plan.Skipped, model.Skipped{
			Provider: "shell",
			Resource: "shell:config",
			Reason:   "existing user Shell configuration differs; overwrite disabled",
		})
		return plan, nil
	}
	desired, err := ReadDocument(p.shellSnapshotPath("shell.json"))
	if err != nil {
		return model.RestorePlan{}, fmt.Errorf("read captured shell snapshot: %w", err)
	}
	write := model.FileWrite{
		Source:      p.shellSnapshotPath("shell.json"),
		Destination: p.UserPath,
		SourceHash:  desired.RawHash,
		Backup:      current.UserExists,
	}
	if current.UserExists {
		write.ExpectedHash = current.Current.RawHash
	} else {
		write.ExpectedMissing = true
	}
	plan.Operations = append(plan.Operations,
		model.Operation{
			ID:         "shell.write",
			Provider:   "shell",
			Action:     "write",
			Resource:   "shell:config",
			File:       &write,
			Risk:       model.RiskMedium,
			Reversible: write.Backup,
		},
		model.Operation{
			ID:         "shell.restart",
			Provider:   "shell",
			Action:     "restart",
			Resource:   "shell:runtime",
			Command:    []string{"omarchy-restart-shell"},
			DependsOn:  []string{"shell.write"},
			Risk:       model.RiskMedium,
			Reversible: false,
		},
	)
	return plan, nil
}

func (p Provider) shellSnapshotPath(name string) string {
	return p.ProfileDir + "/shell/" + name
}

// checkSnapshots validates the saved snapshots without plugin provenance.
func (p Provider) checkSnapshots(saved profile.Shell) error {
	desired, err := ReadDocument(p.shellSnapshotPath("shell.json"))
	if err != nil {
		return fmt.Errorf("desired shell snapshot: %w", err)
	}
	if desired.Hash != saved.Hash || desired.Version != saved.Version {
		return fmt.Errorf("desired shell snapshot does not match recorded metadata")
	}
	if _, err := ReadDocument(p.shellSnapshotPath("baseline.json")); err != nil {
		return fmt.Errorf("baseline shell snapshot: %w", err)
	}
	return nil
}
