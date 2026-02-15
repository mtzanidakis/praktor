package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const installScriptURL = "https://claude.ai/install.sh"

var availablePlatforms = []string{
	"linux-x64",
	"linux-arm64",
	"linux-x64-musl",
	"linux-arm64-musl",
	"darwin-x64",
	"darwin-arm64",
}

// goArchToManifest maps Go's runtime.GOARCH values to the manifest naming.
var goArchToManifest = map[string]string{
	"amd64": "x64",
	"arm64": "arm64",
}

type manifest struct {
	Platforms map[string]struct {
		Checksum string `json:"checksum"`
	} `json:"platforms"`
}

type output struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256"`
}

func detectMusl() bool {
	// Check for musl library files
	for _, path := range []string{
		"/lib/libc.musl-x86_64.so.1",
		"/lib/libc.musl-aarch64.so.1",
	} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	// Fallback: check ldd output
	out, err := exec.Command("ldd", "/bin/ls").CombinedOutput()
	if err == nil && strings.Contains(string(out), "musl") {
		return true
	}
	return false
}

func defaultPlatform() string {
	arch, ok := goArchToManifest[runtime.GOARCH]
	if !ok {
		return runtime.GOOS + "-" + runtime.GOARCH
	}
	platform := runtime.GOOS + "-" + arch
	if runtime.GOOS == "linux" && detectMusl() {
		platform += "-musl"
	}
	return platform
}

func main() {
	platform := flag.String("platform", "", "target platform (e.g. linux-x64-musl, darwin-arm64); auto-detected if omitted")
	listPlatforms := flag.Bool("list-platforms", false, "list available platforms and exit")
	savePath := flag.String("save", "", "download the binary to this path (verifies checksum)")
	flag.Parse()

	if *listPlatforms {
		for _, p := range availablePlatforms {
			fmt.Println(p)
		}
		return
	}

	if *platform == "" {
		*platform = defaultPlatform()
	}

	if !isValidPlatform(*platform) {
		fmt.Fprintf(os.Stderr, "error: unsupported platform %q\n", *platform)
		os.Exit(1)
	}

	baseURL, err := fetchBaseURL(installScriptURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetching base URL: %v\n", err)
		os.Exit(1)
	}

	version, err := fetchVersion(baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetching version: %v\n", err)
		os.Exit(1)
	}

	checksum, err := fetchChecksum(baseURL, version, *platform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetching checksum: %v\n", err)
		os.Exit(1)
	}

	downloadURL := fmt.Sprintf("%s/%s/%s/claude", baseURL, version, *platform)

	result := output{
		Version:     version,
		DownloadURL: downloadURL,
		SHA256:      checksum,
	}

	if *savePath != "" {
		if err := downloadAndVerify(downloadURL, checksum, *savePath); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "saved claude %s to %s\n", version, *savePath)
		return
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "error: encoding output: %v\n", err)
		os.Exit(1)
	}
}

func downloadAndVerify(url, expectedChecksum, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: unexpected status %d", resp.StatusCode)
	}

	f, err := os.CreateTemp("", "claude-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	hasher := sha256.New()
	w := io.MultiWriter(f, hasher)

	if _, err := io.Copy(w, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("download: %w", err)
	}
	f.Close()

	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expectedChecksum {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, expectedChecksum)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		// Cross-device rename; fall back to copy
		src, err2 := os.Open(tmpPath)
		if err2 != nil {
			return fmt.Errorf("open temp: %w", err2)
		}
		defer src.Close()

		dst, err2 := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err2 != nil {
			return fmt.Errorf("create dest: %w", err2)
		}
		if _, err2 := io.Copy(dst, src); err2 != nil {
			dst.Close()
			return fmt.Errorf("copy: %w", err2)
		}
		dst.Close()
	}

	return nil
}

// fetchBaseURL fetches the install script and extracts the GCS_BUCKET value.
func fetchBaseURL(scriptURL string) (string, error) {
	resp, err := http.Get(scriptURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d fetching install script", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "GCS_BUCKET=") {
			val := strings.TrimPrefix(line, "GCS_BUCKET=")
			val = strings.Trim(val, `"'`)
			if val == "" {
				return "", fmt.Errorf("GCS_BUCKET is empty in install script")
			}
			return val, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading install script: %w", err)
	}

	return "", fmt.Errorf("GCS_BUCKET not found in install script")
}

func isValidPlatform(p string) bool {
	for _, valid := range availablePlatforms {
		if p == valid {
			return true
		}
	}
	return false
}

func fetchVersion(baseURL string) (string, error) {
	resp, err := http.Get(baseURL + "/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d fetching latest version", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), nil
}

func fetchChecksum(baseURL, version, platform string) (string, error) {
	url := fmt.Sprintf("%s/%s/manifest.json", baseURL, version)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d fetching manifest", resp.StatusCode)
	}

	var m manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return "", fmt.Errorf("decoding manifest: %w", err)
	}

	p, ok := m.Platforms[platform]
	if !ok {
		return "", fmt.Errorf("platform %q not found in manifest", platform)
	}

	return p.Checksum, nil
}
