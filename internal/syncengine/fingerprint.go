package syncengine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Fingerprint hashes a provider-normalized value. JSON sorts object keys;
// providers must normalize unordered lists and omit volatile fields first.
// Neither the value nor encoding errors are returned in diagnostics.
func Fingerprint(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("sync: semantic value cannot be encoded")
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// TargetSet builds deterministic projections, rejecting duplicate identities.
// The absent value carries provenance; omission means Unmanaged, not absence.
type TargetSet struct {
	category string
	targets  map[string]DesiredTarget
}

func NewTargetSet(category string) *TargetSet {
	return &TargetSet{category: category, targets: map[string]DesiredTarget{}}
}
func (s *TargetSet) Add(key string, kind StateKind, value any) error {
	if key == "" {
		return fmt.Errorf("sync: empty target identity")
	}
	if _, ok := s.targets[key]; ok {
		return fmt.Errorf("sync: duplicate target identity in %s", s.category)
	}
	hash, err := Fingerprint(value)
	if err != nil {
		return err
	}
	s.targets[key] = DesiredTarget{Key: TargetKey{Category: s.category, Target: key}, State: State{Kind: kind, Fingerprint: hash}}
	return nil
}
func (s *TargetSet) Targets() []DesiredTarget {
	out := make([]DesiredTarget, 0, len(s.targets))
	for _, v := range s.targets {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key.Target < out[j].Key.Target })
	return out
}
