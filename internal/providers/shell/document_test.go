package shell

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseDocumentCanonicalHashIgnoresWhitespaceAndKeyOrder(t *testing.T) {
	a, err := ParseDocument([]byte(`{"version":1,"bar":{"position":"top"}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseDocument([]byte(`{
		"bar": {"position": "top"},
		"version": 1
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash != b.Hash {
		t.Fatalf("canonical hashes differ: %s != %s", a.Hash, b.Hash)
	}
	if a.RawHash == b.RawHash {
		t.Fatal("raw hashes should preserve representation differences")
	}
}

func TestParseDocumentRejectsMissingOrNonIntegerVersion(t *testing.T) {
	cases := map[string]string{
		"missing version": `{"bar":{}}`,
		"string version":  `{"version":"1"}`,
		"decimal version": `{"version":1.5}`,
		"boolean version": `{"version":true}`,
		"null version":    `{"version":null}`,
		"non-object root": `[1,2]`,
		"invalid JSON":    `{`,
		"trailing value":  `{"version":1} {}`,
	}
	for name, raw := range cases {
		if _, err := ParseDocument([]byte(raw)); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
}

func TestParseDocumentPreservesUnknownFieldsInCanonicalHash(t *testing.T) {
	a, err := ParseDocument([]byte(`{"version":1,"future":{"x":[1,2,{"deep":true}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseDocument([]byte(`{"version":1,"future":{"x":[1,2,{"deep":3}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash == b.Hash {
		t.Fatal("unknown field changes must alter the canonical hash")
	}
	if len(a.Value) != 2 {
		t.Fatalf("value fields = %v", a.Value)
	}
}

func TestThirdPartyReferencesFindsBarLayoutAndPluginInstances(t *testing.T) {
	doc, err := ParseDocument([]byte(`{
		"version": 1,
		"bar": {
			"id": "acme.fullbar",
			"layout": {
				"left": [{"id":"omarchy.menu"},{"id":"acme.weather"}],
				"center": [{"id":"omarchy.clock"}],
				"right": ["acme.string-widget", {"id":"acme.weather"}]
			}
		},
		"plugins": [
			{"id":"acme.panel"},
			{"id":"omarchy.menu"}
		],
		"disabledPlugins": ["omarchy.notifications"]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	got := ThirdPartyReferences(doc)
	want := []string{"acme.fullbar", "acme.panel", "acme.string-widget", "acme.weather"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("references = %v, want %v", got, want)
	}
}

func TestReadDocumentRejectsSymlinkAndNonRegularFile(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if err := os.WriteFile(real, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDocument(link); err == nil {
		t.Fatal("symlink must be rejected")
	}
	if !strings.Contains(mustErrText(t, link), "symlink") {
		t.Fatal("error must mention symlink")
	}
}

func mustErrText(t *testing.T, path string) string {
	t.Helper()
	_, err := ReadDocument(path)
	if err == nil {
		t.Fatal("expected error")
	}
	return err.Error()
}
