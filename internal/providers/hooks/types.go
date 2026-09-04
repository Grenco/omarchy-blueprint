package hooks

// DetectedHook is one runtime-relevant hook read from the live hook tree.
type DetectedHook struct {
	Path string
	Hash string
	Mode string
	Raw  []byte
}

type State struct {
	Items []DetectedHook
}

type Provider struct {
	UserDir    string
	ProfileDir string
}
