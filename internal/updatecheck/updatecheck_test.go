package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
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
