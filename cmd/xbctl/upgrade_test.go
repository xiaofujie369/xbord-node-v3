package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseUpgradeArgs(t *testing.T) {
	opts, err := parseUpgradeArgs(nil)
	if err != nil || opts.version != "latest" || opts.repo != releaseRepo {
		t.Fatalf("defaults = %+v, %v", opts, err)
	}
	opts, err = parseUpgradeArgs([]string{"--version", "v1.2.3", "--repo", "owner/repo"})
	if err != nil || opts.version != "v1.2.3" || opts.repo != "owner/repo" {
		t.Fatalf("parsed = %+v, %v", opts, err)
	}
	for _, args := range [][]string{
		{"--version", "../../evil"},
		{"--version", "v1/../../x"},
		{"--version", "v1?x=1"},
		{"--repo", "evil.com/owner/repo"},
		{"--repo", "owner"},
		{"--sha256", "abc"},
		{"--sha256", strings.Repeat("a", 64)},
		{"--version"},
		{"--bogus"},
	} {
		if _, err := parseUpgradeArgs(args); err == nil {
			t.Errorf("parseUpgradeArgs(%q) accepted invalid input", args)
		}
	}
}

func TestParseChecksums(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("B", 64)
	sums, err := parseChecksums(strings.NewReader("# release\n" + a + "  xboard-node-linux-amd64\n" + b + " *dist/xbctl-linux-amd64\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sums["xboard-node-linux-amd64"] != a || sums["xbctl-linux-amd64"] != strings.ToLower(b) {
		t.Fatalf("sums = %v", sums)
	}
	if _, err := parseChecksums(strings.NewReader("nothex  file\n")); err == nil {
		t.Fatal("malformed checksum accepted")
	}
}

// Regression: upgrades executed the downloaded binary as root without any
// integrity check. A mismatching download must never reach the disk as an
// executable file.
func TestDownloadVerifiedRejectsChecksumMismatch(t *testing.T) {
	payload := []byte("#!/bin/sh\necho pwned\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	defer srv.Close()
	dir := t.TempDir()

	if _, err := downloadVerified(srv.Client(), srv.URL, dir, strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("rejected download left files behind: %v", entries)
	}

	sum := sha256.Sum256(payload)
	path, err := downloadVerified(srv.Client(), srv.URL, dir, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 != 0 || fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("verified download mode = %v, want private and not executable", fi.Mode().Perm())
	}
}

func TestDownloadVerifiedRejectsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := downloadVerified(srv.Client(), srv.URL, t.TempDir(), strings.Repeat("0", 64)); err == nil {
		t.Fatal("HTTP 404 accepted")
	}
}

func TestReplaceAndRestoreFile(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "xboard-node")
	src := filepath.Join(dir, "new")
	os.WriteFile(dst, []byte("old"), 0o755)
	os.WriteFile(src, []byte("new"), 0o755)

	backup, err := replaceFile(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "new" {
		t.Fatalf("dst = %q", got)
	}
	if got, _ := os.ReadFile(backup); string(got) != "old" {
		t.Fatalf("backup = %q", got)
	}
	if err := restoreFile(backup, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "old" {
		t.Fatalf("restored dst = %q", got)
	}
	if got, _ := os.ReadFile(backup); string(got) != "old" {
		t.Fatal("backup must survive a restore for manual recovery")
	}
}

func TestReleaseAssetURL(t *testing.T) {
	if got := releaseAssetURL("o/r", "latest", "a"); got != "https://github.com/o/r/releases/latest/download/a" {
		t.Fatal(got)
	}
	if got := releaseAssetURL("o/r", "v1", "a"); got != "https://github.com/o/r/releases/download/v1/a" {
		t.Fatal(got)
	}
}
