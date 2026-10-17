package autosync

import (
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

var (
	testHost   = strings.Repeat("a", 64)
	testRemote = strings.Repeat("b", 64)
)

func testIdentity(root string) Identity {
	return Identity{ProfileRoot: root, HostID: testHost, Machine: "laptop", Branch: "main", Upstream: "origin/main", RemoteFingerprint: testRemote}
}

// testRegistry delivers one entry per class so authority rules can be
// exercised; the production registry delivers nothing in Phase 11 PR A.
var (
	passive   = []model.EffectActivation{model.ActivationNone}
	activates = []model.EffectActivation{model.ActivationActivate}
)

func testRegistry() Registry {
	return Registry{Version: CapabilityVersion, Entries: []Entry{
		{Family: "themes.copy-local", Class: ClassConservative, Activations: passive, Changes: []model.EffectChange{model.EffectAdd}, Payloads: []model.EffectPayload{model.PayloadData}, Delivered: true},
		{Family: "defaults.set", Class: ClassBalanced, Activations: passive, Changes: []model.EffectChange{model.EffectUpdate}, Sources: []model.EffectSource{model.SourceClosedSet}, Conditions: []Condition{CondInstalledApp}, Delivered: true},
		{Family: "themes.activate", Class: ClassBalanced, Activations: activates, Payloads: []model.EffectPayload{model.PayloadData, model.PayloadConfig}, Conditions: []Condition{CondNoThemeHook}, Delivered: true},
		{Family: "themes.activate", Class: ClassHandsOff, Activations: activates, RunCode: []string{"themes", "hooks"}, Delivered: true},
		{Family: "hooks.write", Class: ClassHandsOff, Activations: passive, RunCode: []string{"hooks"}, Delivered: true},
		{Family: "packages.install-official", Class: ClassHandsOff, Activations: passive, RunCode: []string{"packages"}, Privileged: true, Conditions: []Condition{CondSignedRepoHelper, CondNoninteractive}, Delivered: true},
		{Family: "config.write", Class: ClassHandsOff, Activations: passive, RunCode: []string{"config"}, Delivered: false, Increment: "I2"},
	}}
}

func effect(family string, change model.EffectChange, payload model.EffectPayload, source model.EffectSource) *model.Effect {
	return &model.Effect{Family: family, Change: change, Payload: payload, Source: source, Activation: model.ActivationNone, Privilege: model.PrivilegeUser, Interaction: model.InteractionNone, Recovery: model.RecoveryNone, Verifiable: true}
}

func themeCopy(id string) OperationInput {
	return OperationInput{Operation: model.Operation{ID: "themes.copy." + id, Provider: "themes", Resource: "theme:" + id, Effect: effect("themes.copy-local", model.EffectAdd, model.PayloadData, model.SourceArtifact)}, Category: "themes", Targets: []string{"theme:" + id}, RestoreApply: map[string]bool{"theme:" + id: true}}
}

func defaultSet(kind string) OperationInput {
	return OperationInput{Operation: model.Operation{ID: "defaults.set." + kind, Provider: "defaults", Resource: "default:" + kind, Effect: effect("defaults.set", model.EffectUpdate, model.PayloadConfig, model.SourceClosedSet)}, Category: "defaults", Targets: []string{"default:" + kind}, RestoreApply: map[string]bool{"default:" + kind: true}}
}

func hookWrite(path string) OperationInput {
	return OperationInput{Operation: model.Operation{ID: "hooks.write." + path, Provider: "hooks", Resource: "hook:" + path, Effect: effect("hooks.write", model.EffectAdd, model.PayloadExecLater, model.SourceArtifact)}, Category: "hooks", Targets: []string{"hook:" + path}, RestoreApply: map[string]bool{"hook:" + path: true}}
}

// approved returns an active grant whose authority is already confirmed.
func approved(g Grant) Grant {
	g.Approved = g.AuthorityDigest()
	return g
}

func activeGrant(root string, preset Preset, categories ...string) Grant {
	g := DefaultGrant()
	g.Setting, g.Preset, g.Identity = SettingOn, preset, testIdentity(root)
	g.Categories = PresetScope(categories)
	return approved(g)
}

type evidenceFor map[Condition]bool

func (e evidenceFor) Refine(_ model.Operation, described model.Effect) (model.Effect, map[Condition]bool) {
	return described, e
}

// inertActivation proves an activation path non-executable.
type inertActivation struct{}

func (inertActivation) Refine(op model.Operation, described model.Effect) (model.Effect, map[Condition]bool) {
	if described.Family == "themes.activate" {
		described.Payload = model.PayloadConfig
		return described, map[Condition]bool{CondNoThemeHook: true, CondPositiveEvidence: true}
	}
	return described, nil
}

type confirmer struct {
	interactive, answer bool
	seen                []Change
}

func (c *confirmer) Interactive() bool { return c.interactive }
func (c *confirmer) ConfirmAutoSync(change Change) (bool, error) {
	c.seen = append(c.seen, change)
	return c.answer, nil
}
