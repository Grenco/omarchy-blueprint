package services

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Live equivalence measurements found 64 close to 128/unlimited batching,
// with smaller argument/output bounds. Shows remain serial within an inventory.
const userUnitBatchSize = 64

type unitRecord struct {
	properties map[string]string
	id         string
	names      []string
}

// systemctl show separates objects with blank lines. Identity comes from
// Id/Names, never from the order of the requested operands or returned records.
func parseUnitRecords(output string) ([]unitRecord, error) {
	var records []unitRecord
	properties := map[string]string{}
	finish := func() error {
		if len(properties) == 0 {
			return nil
		}
		id := properties["Id"]
		if !usableUnitIdentity(id) {
			return fmt.Errorf("user unit record has no usable Id")
		}
		names := strings.Fields(properties["Names"])
		for i, name := range names {
			// systemd's string-vector formatter shell-quotes names containing
			// backslashes. Decode that outer quoting exactly once; the inner
			// literal unit-name \xHH escape must remain intact for mapping.
			if strings.HasPrefix(name, "\"") {
				decoded, err := strconv.Unquote(name)
				if err != nil {
					return fmt.Errorf("user unit record has malformed quoted Names identity")
				}
				name = decoded
				names[i] = name
			}
			if !usableUnitIdentity(name) {
				return fmt.Errorf("user unit record has unusable Names identity")
			}
		}
		records = append(records, unitRecord{properties: properties, id: id, names: names})
		properties = map[string]string{}
		return nil
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			if err := finish(); err != nil {
				return nil, err
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.TrimSpace(key) != key {
			return nil, fmt.Errorf("malformed user unit property record")
		}
		if _, exists := properties[key]; exists {
			return nil, fmt.Errorf("duplicate user unit property %q", key)
		}
		properties[key] = value
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return records, nil
}

// This checks transport identity only, not portable Services ownership or kind.
// Systemd escapes path/whitespace bytes as literal \xHH tokens; preserve them.
// Runtime scopes, devices and other catalogue kinds must not be filtered out.
func usableUnitIdentity(name string) bool {
	if dot := strings.LastIndexByte(name, '.'); dot <= 0 || dot == len(name)-1 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '\\' {
			if i+3 >= len(name) || name[i+1] != 'x' || !hexDigit(name[i+2]) || !hexDigit(name[i+3]) {
				return false
			}
			i += 3
			continue
		}
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(":_.@-", rune(c))) {
			return false
		}
	}
	return true
}

func hexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func sameUnitFacts(a, b map[string]string) bool {
	// Catalogue fallback is operand-specific and is applied only after mapping.
	// Normalization already sorts/deduplicates topology lists. Raw list order and
	// alias-list differences are not conflicting machine facts.
	return reflect.DeepEqual(normalizeObservedUnit(a["Id"], "", a), normalizeObservedUnit(b["Id"], "", b))
}

func mapUnitRecords(requested []string, records []unitRecord) (map[string]map[string]string, error) {
	wanted := make(map[string]bool, len(requested))
	for _, name := range requested {
		wanted[name] = true
	}
	canonical := map[string]map[string]string{}
	identities := map[string]string{}
	for _, record := range records {
		names := append([]string{record.id}, record.names...)
		relevant := false
		for _, name := range names {
			if wanted[name] {
				relevant = true
			}
		}
		if !relevant {
			return nil, fmt.Errorf("user unit batch returned an unrelated record")
		}
		if previous, exists := canonical[record.id]; exists && !sameUnitFacts(previous, record.properties) {
			return nil, fmt.Errorf("user unit batch returned conflicting canonical facts")
		}
		canonical[record.id] = record.properties
		for _, name := range names {
			if previous, exists := identities[name]; exists && previous != record.id {
				return nil, fmt.Errorf("user unit batch returned ambiguous identity")
			}
			identities[name] = record.id
		}
	}
	result := make(map[string]map[string]string, len(requested))
	for _, name := range requested {
		id, exists := identities[name]
		if !exists {
			return nil, fmt.Errorf("user unit batch omitted a requested identity")
		}
		result[name] = canonical[id]
	}
	return result, nil
}
