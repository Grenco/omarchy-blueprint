package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// GitHubReleases reads the latest published release of a GitHub
// repository. GitHub's "latest" endpoint already excludes drafts and
// prereleases; both are rejected again here so a misbehaving response can
// never announce one.
type GitHubReleases struct {
	Repo    string // owner/name
	Client  *http.Client
	BaseURL string // defaults to https://api.github.com
}

const maxReleaseResponse = 1 << 20

func (g GitHubReleases) ID() string { return "github:" + g.Repo }

func (g GitHubReleases) Latest(ctx context.Context) (Release, error) {
	base := g.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+"/repos/"+g.Repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "omarchy-blueprint-update-check")
	response, err := client.Do(request)
	if err != nil {
		return Release{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github releases: %s", response.Status)
	}
	var body struct {
		TagName    string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxReleaseResponse)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("github releases: %w", err)
	}
	if body.Draft || body.Prerelease {
		return Release{}, fmt.Errorf("github releases: latest release %q is not a published release", body.TagName)
	}
	if !strings.HasPrefix(body.TagName, "v") {
		return Release{}, fmt.Errorf("github releases: tag %q is not a vMAJOR.MINOR.PATCH release", body.TagName)
	}
	version, err := ParseVersion(body.TagName)
	if err != nil {
		return Release{}, err
	}
	if len(version.Prerelease) > 0 {
		return Release{}, fmt.Errorf("github releases: tag %q is a prerelease", body.TagName)
	}
	details := "https://github.com/" + g.Repo + "/releases/tag/" + url.PathEscape(body.TagName)
	if parsed, err := url.Parse(body.HTMLURL); err == nil && parsed.Scheme == "https" && parsed.Host == "github.com" && strings.HasPrefix(parsed.Path, "/"+g.Repo+"/releases/") {
		details = parsed.String()
	}
	return Release{
		Version:      version,
		DetailsURL:   details,
		Instructions: "Download this release's PKGBUILD from the release page and build it with makepkg -si, as in the README.",
	}, nil
}
