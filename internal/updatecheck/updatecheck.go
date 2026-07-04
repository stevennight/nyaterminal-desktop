package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nyaterminal/nyaterminal-desktop/internal/version"
)

const (
	defaultBaseURL = "https://api.github.com"
	releasePrefix  = "v"
)

type Result struct {
	Repository      string     `json:"repository"`
	CurrentVersion  string     `json:"currentVersion"`
	LatestVersion   string     `json:"latestVersion,omitempty"`
	UpdateAvailable bool       `json:"updateAvailable"`
	ReleaseName     string     `json:"releaseName,omitempty"`
	ReleaseURL      string     `json:"releaseUrl,omitempty"`
	PublishedAt     *time.Time `json:"publishedAt,omitempty"`
	CheckedAt       time.Time  `json:"checkedAt"`
}

type Checker struct {
	HTTPClient     *http.Client
	BaseURL        string
	Repository     string
	CurrentVersion string
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

func DefaultChecker() Checker {
	return Checker{
		HTTPClient:     &http.Client{Timeout: 15 * time.Second},
		BaseURL:        defaultBaseURL,
		Repository:     version.UpdateRepository,
		CurrentVersion: version.Version,
	}
}

func (c Checker) Check(ctx context.Context) (result Result, err error) {
	repository := strings.TrimSpace(c.Repository)
	currentVersion := strings.TrimSpace(c.CurrentVersion)
	result = Result{
		Repository:     repository,
		CurrentVersion: currentVersion,
		CheckedAt:      time.Now().UTC(),
	}
	if repository == "" {
		return result, errors.New("update repository is not configured")
	}
	owner, name, ok := strings.Cut(repository, "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(name) == "" {
		return result, fmt.Errorf("invalid update repository %q", repository)
	}

	endpoint, err := url.JoinPath(baseURL(c.BaseURL), "repos", owner, name, "releases")
	if err != nil {
		return result, err
	}
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return result, err
	}
	query := requestURL.Query()
	query.Set("per_page", "30")
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return result, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "NyaTerminal/"+currentVersion)

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return result, err
	}
	defer func() {
		if closeErr := response.Body.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, fmt.Errorf("github releases returned HTTP %d", response.StatusCode)
	}

	var releases []githubRelease
	if err := json.NewDecoder(response.Body).Decode(&releases); err != nil {
		return result, err
	}
	latest, ok := latestDesktopRelease(releases)
	if !ok {
		return result, nil
	}
	result.LatestVersion = latest.TagName
	result.ReleaseName = latest.Name
	result.ReleaseURL = latest.HTMLURL
	result.PublishedAt = &latest.PublishedAt
	result.UpdateAvailable = version.Compare(currentVersion, latest.TagName) < 0
	return result, nil
}

func baseURL(value string) string {
	if strings.TrimSpace(value) == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func latestDesktopRelease(releases []githubRelease) (githubRelease, bool) {
	var latest githubRelease
	var found bool
	for _, release := range releases {
		if release.Draft || release.Prerelease || !strings.HasPrefix(release.TagName, releasePrefix) {
			continue
		}
		if !found || version.Compare(latest.TagName, release.TagName) < 0 {
			latest = release
			found = true
		}
	}
	return latest, found
}
