package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func githubServer(t *testing.T, status int, body string) GitHubReleases {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Grenco/omarchy-blueprint/releases/latest" {
			t.Errorf("queried %s, want only the latest published release", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("request has no User-Agent; GitHub rejects those")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return GitHubReleases{Repo: "Grenco/omarchy-blueprint", Client: server.Client(), BaseURL: server.URL}
}

func TestGitHubLatestReadsThePublishedRelease(t *testing.T) {
	source := githubServer(t, 200, `{"tag_name":"v0.1.2","html_url":"https://github.com/Grenco/omarchy-blueprint/releases/tag/v0.1.2","draft":false,"prerelease":false}`)
	release, err := source.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if release.Version.String() != "0.1.2" || release.DetailsURL != "https://github.com/Grenco/omarchy-blueprint/releases/tag/v0.1.2" || !strings.Contains(release.Instructions, "makepkg -si") {
		t.Fatalf("release = %+v", release)
	}
}

func TestGitHubLatestRejectsDraftsPrereleasesAndMalformedData(t *testing.T) {
	for name, body := range map[string]string{
		"draft":           `{"tag_name":"v0.2.0","draft":true}`,
		"prerelease flag": `{"tag_name":"v0.2.0","prerelease":true}`,
		"prerelease tag":  `{"tag_name":"v0.2.0-rc.1"}`,
		"no v prefix":     `{"tag_name":"0.2.0"}`,
		"not semver":      `{"tag_name":"nightly"}`,
		"missing tag":     `{}`,
		"not json":        `<html>rate limited</html>`,
		"wrong shape":     `[{"tag_name":"v0.2.0"}]`,
		"truncated":       `{"tag_name":"v0.2`,
	} {
		t.Run(name, func(t *testing.T) {
			if release, err := githubServer(t, 200, body).Latest(context.Background()); err == nil {
				t.Fatalf("accepted %s: %+v", body, release)
			}
		})
	}
}

func TestGitHubLatestIgnoresAForeignDetailsURL(t *testing.T) {
	release, err := githubServer(t, 200, `{"tag_name":"v0.1.2","html_url":"http://evil.example/x"}`).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if release.DetailsURL != "https://github.com/Grenco/omarchy-blueprint/releases/tag/v0.1.2" {
		t.Fatalf("DetailsURL = %q, want the repository's own release page", release.DetailsURL)
	}
}

func TestGitHubLatestFailsOnRateLimitsAndNetworkErrors(t *testing.T) {
	for _, status := range []int{403, 404, 429, 500} {
		if _, err := githubServer(t, status, `{"message":"API rate limit exceeded"}`).Latest(context.Background()); err == nil {
			t.Errorf("status %d was treated as a release", status)
		}
	}
	offline := GitHubReleases{Repo: "Grenco/omarchy-blueprint", BaseURL: "http://127.0.0.1:1"}
	if _, err := offline.Latest(context.Background()); err == nil {
		t.Fatal("an unreachable server was treated as a release")
	}
}
