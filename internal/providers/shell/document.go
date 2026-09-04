package shell

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/content"
)

// SupportedVersion is the only Omarchy Shell JSON version this milestone
// understands.
const SupportedVersion = 1

// Document is a parsed Omarchy Shell JSON file: semantically inspected, with
// the exact raw bytes retained for byte-exact capture and restore.
type Document struct {
	Version    int
	Hash       string
	RawHash    string
	Raw        []byte
	Value      map[string]any
	References []string
}

// ReadDocument reads path only when it is a regular, non-symlink file.
func ReadDocument(path string) (Document, error) {
	f, _, err := content.OpenRegularFile(path)
	if err != nil {
		return Document{}, err
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return Document{}, err
	}
	return ParseDocument(raw)
}

// ParseDocument decodes a Shell JSON document with number precision
// preserved, computes canonical (whitespace/key-order independent) and raw
// SHA-256 hashes, and extracts third-party plugin references.
func ParseDocument(raw []byte) (Document, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return Document{}, fmt.Errorf("parse shell JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Document{}, fmt.Errorf("parse shell JSON: trailing JSON value")
		}
		return Document{}, fmt.Errorf("parse shell JSON: %w", err)
	}
	root, ok := value.(map[string]any)
	if !ok {
		return Document{}, fmt.Errorf("shell JSON root must be an object")
	}
	version, err := documentVersion(root)
	if err != nil {
		return Document{}, err
	}
	canonical, err := json.Marshal(root)
	if err != nil {
		return Document{}, fmt.Errorf("canonicalize shell JSON: %w", err)
	}
	return Document{
		Version:    version,
		Hash:       hashBytes(canonical),
		RawHash:    hashBytes(raw),
		Raw:        append([]byte(nil), raw...),
		Value:      root,
		References: references(root),
	}, nil
}

// EncodeDocument serializes a generated merged document deterministically,
// retaining the same semantic/raw hash and version validation as disk input.
func EncodeDocument(value map[string]any) (Document, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return Document{}, fmt.Errorf("encode shell JSON: %w", err)
	}
	raw = append(raw, '\n')
	return ParseDocument(raw)
}

// ThirdPartyReferences returns the document's unique sorted plugin references,
// excluding first-party `omarchy.*` entries and the built-in bar.
func ThirdPartyReferences(doc Document) []string {
	return doc.References
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func documentVersion(root map[string]any) (int, error) {
	raw, ok := root["version"]
	if !ok {
		return 0, fmt.Errorf("shell JSON version is required")
	}
	number, ok := raw.(json.Number)
	if !ok {
		return 0, fmt.Errorf("shell JSON version must be an integer")
	}
	version, err := strconv.Atoi(number.String())
	if err != nil {
		return 0, fmt.Errorf("shell JSON version must be an integer: %w", err)
	}
	return version, nil
}

// references collects unique, sorted string IDs from bar.id, every
// bar.layout section, and plugins[]. IDs under the omarchy. prefix are
// first-party and excluded; disabledPlugins is deliberately not a source
// dependency.
func references(root map[string]any) []string {
	seen := map[string]bool{}
	collect := func(raw any) {
		if id, ok := raw.(string); ok && id != "" && !strings.HasPrefix(id, "omarchy.") {
			seen[id] = true
		}
	}
	if bar, ok := root["bar"].(map[string]any); ok {
		collect(bar["id"])
		if layout, ok := bar["layout"].(map[string]any); ok {
			for _, section := range []string{"left", "center", "right"} {
				entries, ok := layout[section].([]any)
				if !ok {
					continue
				}
				for _, entry := range entries {
					widget, ok := entry.(map[string]any)
					if !ok {
						continue
					}
					collect(widget["id"])
				}
			}
		}
	}
	if plugins, ok := root["plugins"].([]any); ok {
		for _, entry := range plugins {
			widget, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			collect(widget["id"])
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
