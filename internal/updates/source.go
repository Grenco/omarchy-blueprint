package updates

import "context"

// Release is the newest published release a Source knows of.
type Release struct {
	Version Version
	// DetailsURL is an https page describing the release and how to get it.
	DetailsURL string
	// Instructions tell a person how to update through this source's own
	// channel. Blueprint never performs the update.
	Instructions string
}

// Source finds the latest published release. GitHub Releases is the first
// source; a package-manager or AUR source can replace it without the
// interface changing. ID names the source so a cache from another one is
// never reused.
type Source interface {
	ID() string
	Latest(ctx context.Context) (Release, error)
}

// Notice is what an interface shows when a newer release exists. It holds
// no source-specific detail.
type Notice struct {
	Current, Latest string // display versions, e.g. "v0.1.1"
	DetailsURL      string
	Instructions    string
}
