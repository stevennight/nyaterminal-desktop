package updatecheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/nyaterminal/nyaterminal-desktop/internal/version"
)

const (
	defaultBaseURL          = "https://api.github.com"
	releasePrefix           = "v"
	maxReleaseMetadataBytes = 2 * 1024 * 1024
	maxChecksumBytes        = 1024 * 1024
	maxInstallerBytes       = 256 * 1024 * 1024
)

var stableVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type Result struct {
	Repository       string     `json:"repository"`
	CurrentVersion   string     `json:"currentVersion"`
	LatestVersion    string     `json:"latestVersion,omitempty"`
	UpdateAvailable  bool       `json:"updateAvailable"`
	ReleaseName      string     `json:"releaseName,omitempty"`
	ReleaseURL       string     `json:"releaseUrl,omitempty"`
	ReleaseNotes     string     `json:"releaseNotes,omitempty"`
	PublishedAt      *time.Time `json:"publishedAt,omitempty"`
	CanAutoUpdate    bool       `json:"canAutoUpdate"`
	AutoUpdateReason string     `json:"autoUpdateReason,omitempty"`
	CheckedAt        time.Time  `json:"checkedAt"`
}

type Checker struct {
	HTTPClient     *http.Client
	BaseURL        string
	Repository     string
	CurrentVersion string
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	Body        string               `json:"body"`
	HTMLURL     string               `json:"html_url"`
	Draft       bool                 `json:"draft"`
	Prerelease  bool                 `json:"prerelease"`
	PublishedAt time.Time            `json:"published_at"`
	Assets      []githubReleaseAsset `json:"assets"`
}

func DefaultChecker() Checker {
	return Checker{
		HTTPClient:     &http.Client{Timeout: 10 * time.Minute},
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
	if err := validateRepository(repository); err != nil {
		return result, err
	}

	latest, ok, err := c.latestRelease(ctx, false)
	if err != nil {
		return result, err
	}
	if !ok {
		return result, nil
	}

	result.LatestVersion = latest.TagName
	result.ReleaseName = latest.Name
	result.ReleaseURL = latest.HTMLURL
	result.ReleaseNotes = truncateText(strings.TrimSpace(latest.Body), 4_000)
	if !latest.PublishedAt.IsZero() {
		result.PublishedAt = &latest.PublishedAt
	}
	result.UpdateAvailable = version.Compare(currentVersion, latest.TagName) < 0
	result.AutoUpdateReason = automaticUpdateSupportReason(currentVersion)
	if result.AutoUpdateReason == "" && result.UpdateAvailable {
		if _, _, err := releaseInstallerAssets(latest); err != nil {
			result.AutoUpdateReason = "installer-unavailable"
		}
	}
	result.CanAutoUpdate = result.AutoUpdateReason == ""
	return result, nil
}

func (c Checker) DownloadAndInstall(ctx context.Context, requestedVersion string) error {
	if reason := automaticUpdateSupportReason(c.CurrentVersion); reason != "" {
		return autoUpdateSupportError(reason)
	}
	requested, ok := canonicalStableVersion(requestedVersion)
	if !ok {
		return errors.New("the requested update version is not a stable semantic version")
	}
	current, ok := canonicalStableVersion(c.CurrentVersion)
	if !ok || version.Compare(current, requested) >= 0 {
		return errors.New("the requested update is not newer than the installed version")
	}
	if err := validateRepository(c.Repository); err != nil {
		return err
	}

	latest, ok, err := c.latestRelease(ctx, true)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no stable desktop release is available")
	}
	latestVersion, ok := canonicalStableVersion(latest.TagName)
	if !ok || latestVersion != requested {
		return errors.New("the selected update is no longer the latest stable release; check again before installing")
	}

	installer, checksums, err := releaseInstallerAssets(latest)
	if err != nil {
		return err
	}
	if err := validateReleaseAsset(c.Repository, latest.TagName, installer, maxInstallerBytes); err != nil {
		return err
	}
	if err := validateReleaseAsset(c.Repository, latest.TagName, checksums, maxChecksumBytes); err != nil {
		return err
	}

	checksumBytes, err := c.downloadBytes(ctx, checksums.BrowserDownloadURL, maxChecksumBytes, "release checksums")
	if err != nil {
		return err
	}
	expectedChecksum, ok := checksumForFile(string(checksumBytes), installer.Name)
	if !ok {
		return fmt.Errorf("the release checksum file has no SHA-256 entry for %s", installer.Name)
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("find the update download folder: %w", err)
	}
	updateDir := filepath.Join(cacheDir, "NyaTerminal", "updates")
	installerPath, err := c.downloadInstaller(ctx, installer, expectedChecksum, updateDir, requested)
	if err != nil {
		return err
	}
	if err := launchInstaller(installerPath); err != nil {
		return err
	}
	return nil
}

func (c Checker) latestRelease(ctx context.Context, requireFallbackAssets bool) (githubRelease, bool, error) {
	releases, apiErr := c.fetchReleases(ctx)
	if apiErr == nil {
		latest, ok := latestDesktopRelease(releases)
		return latest, ok, nil
	}

	latest, webErr := c.fetchLatestReleaseFromWeb(ctx)
	if webErr != nil {
		return githubRelease{}, false, fmt.Errorf(
			"could not determine the latest release; GitHub API: %v; release page: %w",
			apiErr,
			webErr,
		)
	}
	if assetErr := c.populateAssetsFromChecksums(ctx, &latest); assetErr != nil && requireFallbackAssets {
		return githubRelease{}, false, fmt.Errorf(
			"GitHub API was unavailable and release assets could not be resolved from SHA256SUMS: %w",
			assetErr,
		)
	}
	return latest, true, nil
}

func (c Checker) fetchReleases(ctx context.Context) (releases []githubRelease, err error) {
	owner, name, _ := strings.Cut(strings.TrimSpace(c.Repository), "/")
	endpoint, err := url.JoinPath(baseURL(c.BaseURL), "repos", owner, name, "releases")
	if err != nil {
		return nil, err
	}
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	query := requestURL.Query()
	query.Set("per_page", "30")
	requestURL.RawQuery = query.Encode()

	response, err := c.get(ctx, requestURL.String(), "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := response.Body.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("github releases returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxReleaseMetadataBytes {
		return nil, errors.New("github release metadata exceeds the allowed size")
	}
	limited := io.LimitReader(response.Body, maxReleaseMetadataBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(payload) > maxReleaseMetadataBytes {
		return nil, errors.New("github release metadata exceeds the allowed size")
	}
	if err := json.Unmarshal(payload, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func (c Checker) fetchLatestReleaseFromWeb(ctx context.Context) (release githubRelease, err error) {
	repository := strings.TrimSpace(c.Repository)
	response, err := c.get(ctx, "https://github.com/"+repository+"/releases/latest", "text/html")
	if err != nil {
		return githubRelease{}, err
	}
	defer func() {
		if closeErr := response.Body.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return githubRelease{}, fmt.Errorf("GitHub release page returned HTTP %d", response.StatusCode)
	}
	return releaseFromWebURL(repository, response.Request.URL)
}

func (c Checker) populateAssetsFromChecksums(ctx context.Context, release *githubRelease) error {
	checksumURL := fmt.Sprintf(
		"https://github.com/%s/releases/download/%s/SHA256SUMS",
		strings.TrimSpace(c.Repository),
		url.PathEscape(release.TagName),
	)
	checksumBytes, err := c.downloadBytes(ctx, checksumURL, maxChecksumBytes, "release checksums")
	if err != nil {
		return err
	}
	assets := make([]githubReleaseAsset, 0)
	for _, name := range checksumFileNames(string(checksumBytes)) {
		assets = append(assets, githubReleaseAsset{
			Name: name,
			BrowserDownloadURL: fmt.Sprintf(
				"https://github.com/%s/releases/download/%s/%s",
				strings.TrimSpace(c.Repository),
				url.PathEscape(release.TagName),
				url.PathEscape(name),
			),
		})
	}
	if len(assets) == 0 {
		return errors.New("the release checksum file contains no valid assets")
	}
	assets = append(assets, githubReleaseAsset{
		Name:               "SHA256SUMS",
		BrowserDownloadURL: checksumURL,
		Size:               int64(len(checksumBytes)),
	})
	release.Assets = assets
	return nil
}

func (c Checker) downloadBytes(ctx context.Context, downloadURL string, maximum int64, label string) (body []byte, err error) {
	response, err := c.get(ctx, downloadURL, "application/octet-stream")
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", label, err)
	}
	defer func() {
		if closeErr := response.Body.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("download %s: HTTP %d", label, response.StatusCode)
	}
	if response.ContentLength > maximum {
		return nil, fmt.Errorf("%s exceeds the allowed size", label)
	}
	body, err = io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", label, err)
	}
	if int64(len(body)) > maximum {
		return nil, fmt.Errorf("%s exceeds the allowed size", label)
	}
	return body, nil
}

func (c Checker) downloadInstaller(
	ctx context.Context,
	asset githubReleaseAsset,
	expectedChecksum string,
	directory string,
	requestedVersion string,
) (path string, err error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("prepare the update download folder: %w", err)
	}
	file, err := os.CreateTemp(directory, "NyaTerminal-"+requestedVersion+"-*.exe")
	if err != nil {
		return "", fmt.Errorf("create the update installer file: %w", err)
	}
	installerPath := file.Name()
	path = installerPath
	keep := false
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
			keep = false
		}
		if !keep {
			_ = os.Remove(installerPath)
		}
	}()

	response, err := c.get(ctx, asset.BrowserDownloadURL, "application/octet-stream")
	if err != nil {
		return "", fmt.Errorf("download the update installer: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("download the update installer: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxInstallerBytes {
		return "", errors.New("the update installer exceeds the allowed size")
	}

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(response.Body, maxInstallerBytes+1))
	if err != nil {
		return "", fmt.Errorf("download the update installer: %w", err)
	}
	if written == 0 || written > maxInstallerBytes {
		return "", errors.New("the update installer is empty or exceeds the allowed size")
	}
	if asset.Size > 0 && written != asset.Size {
		return "", errors.New("the downloaded installer size does not match the release metadata")
	}
	if !checksumMatches(hasher, expectedChecksum) {
		return "", errors.New("the downloaded installer failed SHA-256 verification")
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("finish writing the update installer: %w", err)
	}
	keep = true
	return path, nil
}

func (c Checker) get(ctx context.Context, requestURL, accept string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "NyaTerminal/"+strings.TrimSpace(c.CurrentVersion))
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(request)
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
		if release.Draft || release.Prerelease {
			continue
		}
		if _, ok := stableReleaseTagVersion(release.TagName); !ok {
			continue
		}
		if !found || version.Compare(latest.TagName, release.TagName) < 0 {
			latest = release
			found = true
		}
	}
	return latest, found
}

func releaseFromWebURL(repository string, releaseURL *url.URL) (githubRelease, error) {
	if releaseURL == nil || releaseURL.Scheme != "https" || !strings.EqualFold(releaseURL.Host, "github.com") {
		return githubRelease{}, errors.New("GitHub redirected the release page to an untrusted address")
	}
	segments := strings.Split(strings.Trim(releaseURL.EscapedPath(), "/"), "/")
	repositoryParts := strings.Split(repository, "/")
	if len(segments) != 5 || len(repositoryParts) != 2 ||
		!strings.EqualFold(segments[0], repositoryParts[0]) ||
		!strings.EqualFold(segments[1], repositoryParts[1]) ||
		segments[2] != "releases" || segments[3] != "tag" {
		return githubRelease{}, errors.New("GitHub returned an unexpected latest-release page")
	}
	tag, err := url.PathUnescape(segments[4])
	if err != nil {
		return githubRelease{}, errors.New("GitHub returned an invalid release tag")
	}
	if _, ok := stableReleaseTagVersion(tag); !ok {
		return githubRelease{}, errors.New("the latest GitHub release is not a stable desktop version")
	}
	return githubRelease{
		TagName: tag,
		Name:    tag,
		HTMLURL: releaseURL.String(),
	}, nil
}

func releaseInstallerAssets(release githubRelease) (githubReleaseAsset, githubReleaseAsset, error) {
	installer, ok := selectInstaller(release.Assets, runtime.GOARCH)
	if !ok {
		return githubReleaseAsset{}, githubReleaseAsset{}, errors.New("the latest release has no compatible Windows installer")
	}
	for _, asset := range release.Assets {
		if asset.Name == "SHA256SUMS" {
			return installer, asset, nil
		}
	}
	return githubReleaseAsset{}, githubReleaseAsset{}, errors.New("the latest release has no SHA256SUMS asset")
}

func selectInstaller(assets []githubReleaseAsset, architecture string) (githubReleaseAsset, bool) {
	markers := []string{}
	switch architecture {
	case "amd64":
		markers = []string{"_x64-", "_amd64-", "-amd64-"}
	case "arm64":
		markers = []string{"_arm64-", "_aarch64-", "-arm64-"}
	default:
		return githubReleaseAsset{}, false
	}
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if !strings.HasPrefix(name, "nyaterminal_") || !strings.HasSuffix(name, "-setup.exe") {
			continue
		}
		for _, marker := range markers {
			if strings.Contains(name, marker) {
				return asset, true
			}
		}
	}
	return githubReleaseAsset{}, false
}

func validateRepository(repository string) error {
	parts := strings.Split(strings.TrimSpace(repository), "/")
	if len(parts) != 2 || !validRepositoryPart(parts[0]) || !validRepositoryPart(parts[1]) {
		return fmt.Errorf("invalid update repository %q", repository)
	}
	return nil
}

func validRepositoryPart(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') &&
			!(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') &&
			character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}

func validateReleaseAsset(repository, tag string, asset githubReleaseAsset, maximum int64) error {
	if !plainFileName(asset.Name) {
		return fmt.Errorf("the release contains an invalid asset name %q", asset.Name)
	}
	if asset.Size < 0 || asset.Size > maximum {
		return fmt.Errorf("the release asset %s has an invalid size", asset.Name)
	}
	parsed, err := url.Parse(asset.BrowserDownloadURL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") {
		return fmt.Errorf("the download URL for %s is not trusted", asset.Name)
	}
	segments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	repositoryParts := strings.Split(repository, "/")
	if len(segments) != 6 ||
		!strings.EqualFold(segments[0], repositoryParts[0]) ||
		!strings.EqualFold(segments[1], repositoryParts[1]) ||
		segments[2] != "releases" || segments[3] != "download" ||
		segments[4] != url.PathEscape(tag) || segments[5] != url.PathEscape(asset.Name) {
		return fmt.Errorf("the download URL for %s is not owned by the configured update repository", asset.Name)
	}
	return nil
}

func plainFileName(name string) bool {
	return name != "" && len(name) <= 240 && name != "." && name != ".." && !strings.ContainsAny(name, `/\\`)
}

func canonicalStableVersion(value string) (string, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, releasePrefix)
	if !stableVersionPattern.MatchString(value) {
		return "", false
	}
	return value, true
}

func stableReleaseTagVersion(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, releasePrefix) {
		return "", false
	}
	return canonicalStableVersion(value)
}

func automaticUpdateSupportReason(currentVersion string) string {
	if _, ok := canonicalStableVersion(currentVersion); !ok {
		return "development-build"
	}
	return platformAutomaticUpdateSupportReason()
}

func autoUpdateSupportError(reason string) error {
	switch reason {
	case "development-build":
		return errors.New("automatic updates are unavailable in development builds")
	case "not-installed":
		return errors.New("automatic updates are unavailable for portable copies")
	default:
		return errors.New("automatic updates are unavailable on this platform")
	}
}

func checksumForFile(checksums, fileName string) (string, bool) {
	for _, line := range strings.Split(checksums, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if len(line) < 66 || (line[64:66] != "  " && line[64:66] != " *") || line[66:] != fileName {
			continue
		}
		checksum := strings.ToLower(line[:64])
		if _, err := hex.DecodeString(checksum); err == nil {
			return checksum, true
		}
	}
	return "", false
}

func checksumFileNames(checksums string) []string {
	fileNames := make([]string, 0)
	for _, line := range strings.Split(checksums, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if len(line) < 66 || (line[64:66] != "  " && line[64:66] != " *") {
			continue
		}
		if _, err := hex.DecodeString(line[:64]); err != nil {
			continue
		}
		if name := line[66:]; plainFileName(name) {
			fileNames = append(fileNames, name)
		}
	}
	return fileNames
}

func checksumMatches(hasher hash.Hash, expected string) bool {
	return strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), strings.TrimSpace(expected))
}

func truncateText(value string, maximumCharacters int) string {
	characters := []rune(value)
	if len(characters) <= maximumCharacters {
		return value
	}
	return string(characters[:maximumCharacters])
}
