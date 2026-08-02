package updatecheck

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestCheckFiltersDesktopReleases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/NyaTerminal/releases" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name":"backend-v9.0.0","name":"ignored","html_url":"https://example.test/backend","draft":false,"prerelease":false,"published_at":"2026-01-03T00:00:00Z"},
			{"tag_name":"9.0.0","name":"unprefixed","html_url":"https://example.test/unprefixed","draft":false,"prerelease":false,"published_at":"2026-01-05T00:00:00Z"},
			{"tag_name":"v0.3.0","name":"Desktop 0.3.0","html_url":"https://example.test/desktop-030","draft":false,"prerelease":false,"published_at":"2026-01-02T00:00:00Z"},
			{"tag_name":"v0.4.0","name":"Desktop prerelease","html_url":"https://example.test/desktop-040","draft":false,"prerelease":true,"published_at":"2026-01-04T00:00:00Z"},
			{"tag_name":"v0.2.0","name":"Desktop 0.2.0","html_url":"https://example.test/desktop-020","draft":false,"prerelease":false,"published_at":"2026-01-01T00:00:00Z"}
		]`))
	}))
	defer server.Close()

	result, err := (Checker{
		HTTPClient:     server.Client(),
		BaseURL:        server.URL,
		Repository:     "acme/NyaTerminal",
		CurrentVersion: "v0.2.0",
	}).Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpdateAvailable {
		t.Fatal("expected an update")
	}
	if result.LatestVersion != "v0.3.0" {
		t.Fatalf("latest version = %q, want v0.3.0", result.LatestVersion)
	}
	if result.ReleaseURL != "https://example.test/desktop-030" {
		t.Fatalf("release URL = %q", result.ReleaseURL)
	}
}

func TestCheckReportsNoUpdateForCurrentVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"tag_name":"v0.3.0","name":"Desktop 0.3.0","html_url":"https://example.test/desktop-030","draft":false,"prerelease":false,"published_at":"2026-01-02T00:00:00Z"}
		]`))
	}))
	defer server.Close()

	result, err := (Checker{
		HTTPClient:     server.Client(),
		BaseURL:        server.URL,
		Repository:     "acme/NyaTerminal",
		CurrentVersion: "v0.3.0",
	}).Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.UpdateAvailable {
		t.Fatal("expected no update")
	}
}

func TestSelectInstallerMatchesArchitectureAndSetupNaming(t *testing.T) {
	assets := []githubReleaseAsset{
		{Name: "NyaTerminal_1.2.3_arm64-setup.exe"},
		{Name: "NyaTerminal_1.2.3_x64-setup.exe"},
		{Name: "NyaTerminal_1.2.3_windows_x64.zip"},
	}
	asset, ok := selectInstaller(assets, "amd64")
	if !ok || asset.Name != "NyaTerminal_1.2.3_x64-setup.exe" {
		t.Fatalf("selected installer = %q, %v", asset.Name, ok)
	}
	if _, ok := selectInstaller(assets, "386"); ok {
		t.Fatal("unexpected installer for unsupported architecture")
	}
}

func TestChecksumForFileRequiresExactFilename(t *testing.T) {
	hash := strings.Repeat("a", 64)
	checksums := hash + "  NyaTerminal_1.2.3_x64-setup.exe\n" +
		strings.Repeat("b", 64) + " *portable.zip\r\n"
	value, ok := checksumForFile(checksums, "NyaTerminal_1.2.3_x64-setup.exe")
	if !ok || value != hash {
		t.Fatalf("checksum = %q, %v", value, ok)
	}
	if _, ok := checksumForFile(checksums, "NyaTerminal_1.2.3_x64-setup.exe.bak"); ok {
		t.Fatal("matched a non-exact asset name")
	}
}

func TestValidateReleaseAssetRequiresConfiguredRepository(t *testing.T) {
	asset := githubReleaseAsset{
		Name:               "NyaTerminal_1.2.3_x64-setup.exe",
		BrowserDownloadURL: "https://github.com/acme/NyaTerminal/releases/download/v1.2.3/NyaTerminal_1.2.3_x64-setup.exe",
		Size:               42,
	}
	if err := validateReleaseAsset("acme/NyaTerminal", "v1.2.3", asset, 100); err != nil {
		t.Fatal(err)
	}
	asset.BrowserDownloadURL = "https://github.com/other/NyaTerminal/releases/download/v1.2.3/NyaTerminal_1.2.3_x64-setup.exe"
	if err := validateReleaseAsset("acme/NyaTerminal", "v1.2.3", asset, 100); err == nil {
		t.Fatal("accepted an asset from another repository")
	}
}

func TestCanonicalStableVersionRejectsPrereleases(t *testing.T) {
	for _, value := range []string{"v1.2.3-beta.1", "v01.2.3", "desktop-v1.2.3", "1.2"} {
		if _, ok := canonicalStableVersion(value); ok {
			t.Fatalf("accepted unstable version %q", value)
		}
	}
	if value, ok := canonicalStableVersion("v1.2.3"); !ok || value != "1.2.3" {
		t.Fatalf("stable version = %q, %v", value, ok)
	}
}

func TestDownloadInstallerVerifiesChecksumAndCleansFailures(t *testing.T) {
	payload := []byte("verified installer payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	checker := Checker{HTTPClient: server.Client(), CurrentVersion: "1.0.0"}
	directory := t.TempDir()
	asset := githubReleaseAsset{
		Name:               "NyaTerminal_1.1.0_x64-setup.exe",
		BrowserDownloadURL: server.URL,
		Size:               int64(len(payload)),
	}
	expected := fmt.Sprintf("%x", sha256.Sum256(payload))
	path, err := checker.downloadInstaller(context.Background(), asset, expected, directory, "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(downloaded) != string(payload) {
		t.Fatal("downloaded installer content differs")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if _, err := checker.downloadInstaller(context.Background(), asset, strings.Repeat("0", 64), directory, "1.1.0"); err == nil {
		t.Fatal("accepted an installer with the wrong checksum")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed download left %d temporary files", len(entries))
	}
}

func TestReleaseFromWebURLAcceptsOnlyConfiguredStableRelease(t *testing.T) {
	valid, _ := url.Parse("https://github.com/acme/NyaTerminal/releases/tag/v1.2.3")
	release, err := releaseFromWebURL("acme/NyaTerminal", valid)
	if err != nil || release.TagName != "v1.2.3" {
		t.Fatalf("release = %#v, error = %v", release, err)
	}
	for _, value := range []string{
		"https://example.com/acme/NyaTerminal/releases/tag/v1.2.3",
		"https://github.com/other/NyaTerminal/releases/tag/v1.2.3",
		"https://github.com/acme/NyaTerminal/releases/tag/v1.2.3-beta.1",
	} {
		candidate, _ := url.Parse(value)
		if _, err := releaseFromWebURL("acme/NyaTerminal", candidate); err == nil {
			t.Fatalf("accepted untrusted release URL %q", value)
		}
	}
	unprefixed, _ := url.Parse("https://github.com/acme/NyaTerminal/releases/tag/1.2.3")
	if _, err := releaseFromWebURL("acme/NyaTerminal", unprefixed); err == nil {
		t.Fatal("accepted a release tag without the required v prefix")
	}
}

func TestChecksumFileNamesRejectsPathsAndInvalidHashes(t *testing.T) {
	hash := strings.Repeat("a", 64)
	checksums := hash + "  NyaTerminal_1.2.3_x64-setup.exe\n" +
		hash + "  ../escape.exe\n" +
		strings.Repeat("z", 64) + "  invalid.exe\n"
	names := checksumFileNames(checksums)
	if len(names) != 1 || names[0] != "NyaTerminal_1.2.3_x64-setup.exe" {
		t.Fatalf("checksum file names = %#v", names)
	}
}
