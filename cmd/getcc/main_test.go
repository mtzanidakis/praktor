package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchBaseURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `#!/bin/sh`)
		fmt.Fprintln(w, `DOWNLOAD_DIR="$HOME/.claude/downloads"`)
		fmt.Fprintln(w, `GCS_BUCKET="https://storage.example.com/bucket/releases"`)
		fmt.Fprintln(w, `echo "hello"`)
	}))
	defer ts.Close()

	got, err := fetchBaseURL(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://storage.example.com/bucket/releases"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFetchBaseURLSingleQuotes(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "GCS_BUCKET='https://example.com/bucket'")
	}))
	defer ts.Close()

	got, err := fetchBaseURL(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/bucket" {
		t.Errorf("got %q", got)
	}
}

func TestFetchBaseURLMissing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `#!/bin/sh`)
		fmt.Fprintln(w, `echo "no bucket here"`)
	}))
	defer ts.Close()

	_, err := fetchBaseURL(ts.URL)
	if err == nil {
		t.Fatal("expected error when GCS_BUCKET is missing")
	}
}

func TestFetchBaseURLHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	_, err := fetchBaseURL(ts.URL)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestIsValidPlatform(t *testing.T) {
	valid := []string{
		"linux-x64",
		"linux-arm64",
		"linux-x64-musl",
		"linux-arm64-musl",
		"darwin-x64",
		"darwin-arm64",
	}
	for _, p := range valid {
		if !isValidPlatform(p) {
			t.Errorf("expected %q to be valid", p)
		}
	}

	invalid := []string{
		"windows-x64",
		"linux-386",
		"",
		"linux",
		"darwin-arm64-musl",
	}
	for _, p := range invalid {
		if isValidPlatform(p) {
			t.Errorf("expected %q to be invalid", p)
		}
	}
}

func TestDefaultPlatform(t *testing.T) {
	p := defaultPlatform()
	if p == "" {
		t.Fatal("defaultPlatform() returned empty string")
	}
	// On any supported CI/dev machine the default should be valid.
	if !isValidPlatform(p) {
		t.Logf("defaultPlatform() = %q (may not be in available list on this OS/arch)", p)
	}
}

func TestFetchVersion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("1.2.3\n"))
	}))
	defer ts.Close()

	version, err := fetchVersion(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.2.3" {
		t.Errorf("got version %q, want %q", version, "1.2.3")
	}
}

func TestFetchVersionHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	_, err := fetchVersion(ts.URL)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func testManifest() manifest {
	var m manifest
	m.Platforms = map[string]struct {
		Checksum string `json:"checksum"`
	}{
		"linux-x64":    {Checksum: "abc123"},
		"darwin-arm64": {Checksum: "def456"},
	}
	return m
}

func TestFetchChecksum(t *testing.T) {
	m := testManifest()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.2.3/manifest.json" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(m)
	}))
	defer ts.Close()

	checksum, err := fetchChecksum(ts.URL, "1.2.3", "linux-x64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if checksum != "abc123" {
		t.Errorf("got checksum %q, want %q", checksum, "abc123")
	}
}

func TestFetchChecksumPlatformNotFound(t *testing.T) {
	m := testManifest()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(m)
	}))
	defer ts.Close()

	_, err := fetchChecksum(ts.URL, "1.2.3", "windows-x64")
	if err == nil {
		t.Fatal("expected error for missing platform")
	}
}

func TestFetchChecksumHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	_, err := fetchChecksum(ts.URL, "9.9.9", "linux-x64")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestFetchChecksumInvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer ts.Close()

	_, err := fetchChecksum(ts.URL, "1.2.3", "linux-x64")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestOutputJSON(t *testing.T) {
	o := output{
		Version:     "1.2.3",
		DownloadURL: "https://example.com/1.2.3/linux-x64/claude",
		SHA256:      "abc123",
	}

	data, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got output
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != o {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, o)
	}

	// Verify JSON field names.
	var raw map[string]string
	json.Unmarshal(data, &raw)
	for _, key := range []string{"version", "download_url", "sha256"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q", key)
		}
	}
}

func TestDownloadAndVerify(t *testing.T) {
	content := []byte("fake claude binary content")
	h := sha256.Sum256(content)
	checksum := hex.EncodeToString(h[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer ts.Close()

	dest := filepath.Join(t.TempDir(), "claude")
	err := downloadAndVerify(ts.URL, checksum, dest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch")
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("expected executable permission")
	}
}

func TestDownloadAndVerifyBadChecksum(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("some content"))
	}))
	defer ts.Close()

	dest := filepath.Join(t.TempDir(), "claude")
	err := downloadAndVerify(ts.URL, "0000000000000000000000000000000000000000000000000000000000000000", dest)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestDownloadAndVerifyHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	dest := filepath.Join(t.TempDir(), "claude")
	err := downloadAndVerify(ts.URL, "abc", dest)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}
