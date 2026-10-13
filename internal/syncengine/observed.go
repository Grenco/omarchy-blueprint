package syncengine

// ObservedMissing is invoked only after the provider establishes physical
// absence. Absence provenance is desired intent, not a property obtainable from
// a missing file/package: a matching historical tombstone retains its identity.
// Providers without tombstones report missing customization as unmanaged.
func ObservedMissing(supportsAbsence bool, alternatives []State) State {
	if !supportsAbsence {
		return State{Kind: StateUnmanaged}
	}
	for _, x := range alternatives {
		if x.Kind == StateAbsent {
			return x
		}
	}
	for _, x := range alternatives {
		if x.Kind == StateUnmanaged {
			return x
		}
	}
	hash, _ := Fingerprint(nil)
	return State{Kind: StateAbsent, Fingerprint: hash}
}
