package model

import (
	"encoding/json"
	"testing"
)

func TestOperationPreservesMediumRiskInPlanJSON(t *testing.T) {
	operation := Operation{ID: "config.write", Risk: RiskMedium, File: &FileWrite{Source: "saved", Destination: "live", SourceHash: "hash", ExpectedMissing: true}}
	b, err := json.Marshal(operation)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" || !containsJSONValue(b, "risk", "medium") {
		t.Fatalf("operation JSON did not preserve medium risk: %s", b)
	}
}

func containsJSONValue(b []byte, field, value string) bool {
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return false
	}
	return decoded[field] == value
}
