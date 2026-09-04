package hooks

// DetectedHook is one runtime-relevant hook read from the live hook tree.
type DetectedHook struct {
	Path string
	Hash string
	Mode string
	Raw  []byte
}

type State struct {
	Items     []DetectedHook
	Unmanaged []UnmanagedHook
}

// UnmanagedHook is a runtime-relevant symlink. Blueprint records no source
// from it and never follows, replaces, or verifies it as portable state.
type UnmanagedHook struct {
	Path   string
	Target string
	Broken bool
}

type Provider struct {
	UserDir    string
	ProfileDir string
}
