package components

// IconSet keeps textual symbols consistent and lets callers retain readable
// ASCII fallbacks where colour or glyph support is unavailable.
type IconSet struct {
	Selected, Attention, Changed, Ready, Error, Unknown string
}

var Icons = IconSet{
	Selected:  ">",
	Attention: "!",
	Changed:   "~",
	Ready:     "✓",
	Error:     "×",
	Unknown:   "?",
}
