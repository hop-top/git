package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hop.top/upgrade"
)

func TestNewUpgradeChecker_ReturnsChecker(t *testing.T) {
	if newUpgradeChecker() == nil {
		t.Fatal("newUpgradeChecker returned nil")
	}
}

func TestUpgrade_NoUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"version": "0.0.0",
			"url":     "http://example.com/git-hop",
		})
	}))
	defer srv.Close()

	c := upgrade.New(
		upgrade.WithBinary("git-hop", "99.0.0"),
		upgrade.WithReleaseURL(srv.URL),
		upgrade.WithStateDir(t.TempDir()),
	)

	var out strings.Builder
	if err := upgrade.RunCLI(context.Background(), c, upgrade.CLIOptions{Quiet: true, Out: &out}); err != nil {
		t.Fatal(err)
	}
}

func TestUpgrade_UpdateAvail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"version": "99.0.0",
			"url":     "http://example.com/git-hop",
			"notes":   "Many improvements",
		})
	}))
	defer srv.Close()

	c := upgrade.New(
		upgrade.WithBinary("git-hop", "1.0.0"),
		upgrade.WithReleaseURL(srv.URL),
		upgrade.WithStateDir(t.TempDir()),
	)

	r := c.Check(context.Background())
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if !r.UpdateAvail {
		t.Error("expected update available")
	}
}
