package compatibility

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func TestBuildCategorySupportedNeedsAffirmativeEvidence(t *testing.T) {
	if _, err := BuildCategory("resources", true, nil, nil); err == nil {
		t.Fatal("an empty applicable assessment must not become supported")
	}
	evidence := []model.CompatibilityEvidence{
		{Kind: "strategy", Summary: "git revision recorded"},
		{Kind: "baseline", Summary: "current surface supported"},
	}
	got, err := BuildCategory("resources", true, evidence, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.CompatibilitySupported || got.Authority != model.CompatibilityUnchanged {
		t.Fatalf("state=%q authority=%q", got.State, got.Authority)
	}
	if got.Evidence[0].Kind != "baseline" || evidence[0].Kind != "strategy" {
		t.Fatalf("builder did not sort a defensive copy: got=%v input=%v", got.Evidence, evidence)
	}
}

func validReport() model.CompatibilityReport {
	return model.CompatibilityReport{
		Target: model.CompatibilityEnvironment{Known: true, OmarchyVersion: "4.0.4"},
		Categories: []model.CompatibilityCategory{{
			Category: "resources", Applies: true, State: model.CompatibilitySupported, Authority: model.CompatibilityUnchanged,
			Evidence: []model.CompatibilityEvidence{{Kind: "strategy", Summary: "portable copy representation"}},
		}},
	}
}

func resourceTargets() map[string]map[string]struct{} {
	return map[string]map[string]struct{}{"resources": {"resource:helper": {}}}
}

func TestValidateReportRequiresKnownTargetEnvironment(t *testing.T) {
	report := validReport()
	report.Target.Known = false
	if err := ValidateReport(report, []string{"resources"}, resourceTargets(), nil); err == nil {
		t.Fatal("missing target environment should be rejected")
	}
	report.Target.Known = true
	report.Target.OmarchyVersion = ""
	if err := ValidateReport(report, []string{"resources"}, resourceTargets(), nil); err == nil {
		t.Fatal("known target without version should be rejected")
	}
}

func TestValidateReportAllowsUnknownProfileLastCapture(t *testing.T) {
	report := validReport() // omitted historical metadata remains explicit unknown context
	if err := ValidateReport(report, []string{"resources"}, resourceTargets(), nil); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	last := decoded["profile_last_capture"].(map[string]any)
	if last["known"] != false || len(last) != 1 {
		t.Fatalf("unknown last capture should only encode known:false: %s", encoded)
	}
}

func TestValidateReportRejectsDuplicateCategories(t *testing.T) {
	report := validReport()
	report.Categories = append(report.Categories, report.Categories[0])
	if err := ValidateReport(report, []string{"resources"}, resourceTargets(), nil); err == nil {
		t.Fatal("duplicate provider category should be rejected")
	}
}

func TestValidateReportRejectsDuplicateTargetCode(t *testing.T) {
	report := validReport()
	finding := model.CompatibilityFinding{Code: "origin.unknown", Target: "resource:helper", State: model.CompatibilityUnknown, Authority: model.CompatibilityReduced, Summary: "origin unknown"}
	report.Categories[0].Findings = []model.CompatibilityFinding{finding, finding}
	report.Categories[0].State, report.Categories[0].Authority = model.CompatibilityUnknown, model.CompatibilityReduced
	if err := ValidateReport(report, []string{"resources"}, resourceTargets(), nil); err == nil {
		t.Fatal("duplicate (target, code) should be rejected")
	}
}

func TestValidateReportRejectsUnknownRequirementReference(t *testing.T) {
	report := validReport()
	report.Categories[0].State, report.Categories[0].Authority = model.CompatibilityUnknown, model.CompatibilityBlocked
	report.Categories[0].Findings = []model.CompatibilityFinding{{Code: "metadata.unknown", Target: "resource:helper", State: model.CompatibilityUnknown, Authority: model.CompatibilityBlocked, Summary: "metadata unavailable", RequirementID: "packages.metadata"}}
	if err := ValidateReport(report, []string{"resources"}, resourceTargets(), nil); err == nil {
		t.Fatal("unlinked requirement should be rejected")
	}
	if err := ValidateReport(report, []string{"resources"}, resourceTargets(), []model.Requirement{{ID: "packages.metadata"}}); err != nil {
		t.Fatalf("linked requirement rejected: %v", err)
	}
}

func TestValidateReportRejectsUnknownTargetKey(t *testing.T) {
	report := validReport()
	report.Categories[0].State = model.CompatibilityUnknown
	report.Categories[0].Findings = []model.CompatibilityFinding{{Code: "origin.unknown", Target: "resource:uninspected", State: model.CompatibilityUnknown, Authority: model.CompatibilityUnchanged, Summary: "origin unavailable"}}
	if err := ValidateReport(report, []string{"resources"}, resourceTargets(), nil); err == nil {
		t.Fatal("uninspected target should be rejected")
	}
	report.Categories[0].Findings[0].Target = ""
	if err := ValidateReport(report, []string{"resources"}, resourceTargets(), nil); err != nil {
		t.Fatalf("category-wide finding should be permitted: %v", err)
	}
}

func TestValidateReportRequiresEverySelectedCategory(t *testing.T) {
	report := validReport()
	if err := ValidateReport(report, []string{"resources", "packages"}, resourceTargets(), nil); err == nil {
		t.Fatal("selected provider cannot omit assessment")
	}
	if err := ValidateReport(report, []string{"packages"}, resourceTargets(), nil); err == nil {
		t.Fatal("unselected provider cannot inject an unrelated blocking category")
	}
}

func TestNormalizeReportIsDeterministic(t *testing.T) {
	report := validReport()
	report.Categories = append(report.Categories, model.CompatibilityCategory{
		Category: "config", Applies: true, State: model.CompatibilityUnknown, Authority: model.CompatibilityReduced,
		Findings: []model.CompatibilityFinding{
			{Code: "z", Target: "config:z", State: model.CompatibilityUnknown, Authority: model.CompatibilityReduced, Summary: "z", Evidence: []model.CompatibilityEvidence{{Kind: "z", Summary: "z"}, {Kind: "a", Summary: "a"}}},
			{Code: "a", Target: "config:a", State: model.CompatibilityUnknown, Authority: model.CompatibilityUnchanged, Summary: "a"},
		},
	})
	original := report.Categories[1].Findings[0].Evidence[0].Kind
	first := NormalizeReport(report)
	second := NormalizeReport(first)
	if !reflect.DeepEqual(first, second) || first.Categories[0].Category != "config" || first.Categories[0].Findings[0].Target != "config:a" || first.Categories[0].Findings[1].Evidence[0].Kind != "a" {
		t.Fatalf("normalization is not stable/ordered: %+v", first)
	}
	if report.Categories[1].Findings[0].Evidence[0].Kind != original {
		t.Fatal("normalization mutated input")
	}
	if NormalizeReport(model.CompatibilityReport{}).Categories == nil {
		t.Fatal("nil categories should serialize as [] rather than null")
	}
}

func TestValidateReportRejectsInconsistentAggregation(t *testing.T) {
	for name, alter := range map[string]func(*model.CompatibilityCategory){
		"unsupported state":     func(c *model.CompatibilityCategory) { c.State = model.CompatibilityState("maybe") },
		"unsupported authority": func(c *model.CompatibilityCategory) { c.Authority = model.CompatibilityAuthority("override") },
		"empty support":         func(c *model.CompatibilityCategory) { c.Evidence = nil },
		"false support":         func(c *model.CompatibilityCategory) { c.State = model.CompatibilityUnknown },
		"unapplied finding":     func(c *model.CompatibilityCategory) { c.Applies = false; c.State = "" },
	} {
		t.Run(name, func(t *testing.T) {
			report := validReport()
			alter(&report.Categories[0])
			if err := ValidateReport(report, []string{"resources"}, resourceTargets(), nil); err == nil {
				t.Fatalf("invalid category accepted: %+v", report.Categories[0])
			}
		})
	}
}

func TestBlockingFindings(t *testing.T) {
	report := validReport()
	report.Categories[0].State, report.Categories[0].Authority = model.CompatibilityUnknown, model.CompatibilityBlocked
	report.Categories[0].Findings = []model.CompatibilityFinding{
		{Code: "nonblocking", Target: "resource:a", State: model.CompatibilityUnknown, Authority: model.CompatibilityReduced, Summary: "reduced"},
		{Code: "blocking", Target: "resource:z", State: model.CompatibilityUnknown, Authority: model.CompatibilityBlocked, Summary: "blocked"},
	}
	got := BlockingFindings(report)
	if len(got) != 1 || got[0].Code != "blocking" {
		t.Fatalf("blocking findings = %+v", got)
	}
}

func TestBuildCategoryUnknownReducedAggregatesConservatively(t *testing.T) {
	findings := []model.CompatibilityFinding{
		{Code: "origin.unknown", Target: "official:z", State: model.CompatibilityUnknown, Authority: model.CompatibilityReduced, Summary: "origin not established"},
		{Code: "known", Target: "official:a", State: model.CompatibilitySupported, Authority: model.CompatibilityUnchanged, Summary: "origin established"},
	}
	got, err := BuildCategory("packages", true, nil, findings)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.CompatibilityUnknown || got.Authority != model.CompatibilityReduced {
		t.Fatalf("state=%q authority=%q", got.State, got.Authority)
	}
	if got.Findings[0].Target != "official:a" || findings[0].Target != "official:z" {
		t.Fatalf("builder did not sort a defensive copy: got=%v input=%v", got.Findings, findings)
	}
}

func TestBuildCategoryIncompatibleMustBlock(t *testing.T) {
	finding := model.CompatibilityFinding{Code: "shell.schema.unsupported", Target: "state", State: model.CompatibilityIncompatible, Authority: model.CompatibilityUnchanged, Summary: "schema unsupported"}
	if _, err := BuildCategory("shell", true, nil, []model.CompatibilityFinding{finding}); err == nil {
		t.Fatal("incompatible intent must not have unchanged authority")
	}
	finding.Authority = model.CompatibilityBlocked
	got, err := BuildCategory("shell", true, nil, []model.CompatibilityFinding{finding})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.CompatibilityIncompatible || got.Authority != model.CompatibilityBlocked {
		t.Fatalf("state=%q authority=%q", got.State, got.Authority)
	}
}

func TestBuildCategoryNotApplicableHasNoStateOrFindings(t *testing.T) {
	got, err := BuildCategory("hooks", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Applies || got.State != "" || got.Authority != model.CompatibilityUnchanged || len(got.Findings) != 0 || len(got.Evidence) != 0 {
		t.Fatalf("not-applicable category = %+v", got)
	}
	if _, err := BuildCategory("hooks", false, nil, []model.CompatibilityFinding{{Code: "lifecycle"}}); err == nil {
		t.Fatal("not-applicable category must not carry an effective finding")
	}
	if !reflect.DeepEqual(got, model.CompatibilityCategory{Category: "hooks", Authority: model.CompatibilityUnchanged}) {
		t.Fatalf("not-applicable category changed shape: %+v", got)
	}
}
