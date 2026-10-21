package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdminWebDir(t *testing.T) {
	web := t.TempDir()
	if got := adminWebDir(web, ""); got != "" {
		t.Fatalf("no bundled admin SPA should leave it unset, got %q", got)
	}
	spa := filepath.Join(web, "admin-spa")
	if err := os.MkdirAll(spa, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spa, "index.html"), []byte("<html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := adminWebDir(web, ""); got != spa {
		t.Fatalf("bundled admin SPA should be used, got %q", got)
	}
	if got := adminWebDir(web, "/explicit"); got != "/explicit" {
		t.Fatalf("explicit config should win, got %q", got)
	}
	if got := adminWebDir("", ""); got != "" {
		t.Fatalf("no web dir should leave it unset, got %q", got)
	}
}
