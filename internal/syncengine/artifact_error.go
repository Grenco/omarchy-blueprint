package syncengine

// ArtifactError names the provider-owned target whose committed artifact does
// not verify against its committed metadata. It carries no paths, bytes or
// wrapped provider errors, so it is safe to show in Sync diagnostics.
type ArtifactError struct {
	Target string
	// Missing means the committed snapshot has no artifact for the target.
	Missing bool
}

func (e *ArtifactError) Error() string {
	if e.Missing {
		return "sync: committed artifact is missing"
	}
	return "sync: committed artifact does not match its metadata"
}
