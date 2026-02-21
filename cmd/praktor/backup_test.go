package main

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestSplitVolumePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantVol string
		wantRel string
	}{
		{"simple file", "praktor-data/db.sqlite", "praktor-data", "db.sqlite"},
		{"nested path", "praktor-wk-agent1/subdir/file.txt", "praktor-wk-agent1", "subdir/file.txt"},
		{"directory with slash", "praktor-data/subdir/", "praktor-data", "subdir/"},
		{"volume root dir", "praktor-data/", "praktor-data", "./"},
		{"volume bare name", "praktor-data", "praktor-data", "./"},
		{"leading dot-slash", "./praktor-data/file.txt", "praktor-data", "file.txt"},
		{"leading slash", "/praktor-data/file.txt", "praktor-data", "file.txt"},
		{"non-praktor prefix", "other-volume/file.txt", "", ""},
		{"empty string", "", "", ""},
		{"just a slash", "/", "", ""},
		{"dot only", ".", "", ""},
		{"home volume", "praktor-home-myagent/config", "praktor-home-myagent", "config"},
		{"nix volume", "praktor-nix-myagent/store/path", "praktor-nix-myagent", "store/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVol, gotRel := splitVolumePath(tt.input)
			if gotVol != tt.wantVol {
				t.Errorf("splitVolumePath(%q) volName = %q, want %q", tt.input, gotVol, tt.wantVol)
			}
			if gotRel != tt.wantRel {
				t.Errorf("splitVolumePath(%q) relPath = %q, want %q", tt.input, gotRel, tt.wantRel)
			}
		})
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 bytes"},
		{512, "512 bytes"},
		{1023, "1023 bytes"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatSize(tt.bytes)
			if got != tt.want {
				t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

// createTestArchive builds a zstd-compressed tar with the given entries.
// Each entry is a path like "praktor-data/file.txt" with the given content.
func createTestArchive(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.tar.zst")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}

	tw := tar.NewWriter(zw)
	for name, content := range entries {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	zw.Close()

	return path
}

func TestScanArchiveVolumes(t *testing.T) {
	archivePath := createTestArchive(t, map[string]string{
		"praktor-data/db.sqlite":               "data",
		"praktor-data/nats/":                    "",
		"praktor-wk-agent1/workspace/file.go":   "code",
		"praktor-home-agent1/.bashrc":           "bashrc",
		"praktor-wk-agent1/workspace/other.go":  "more code",
	})

	volumes, err := scanArchiveVolumes(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	// Should find 3 unique volumes (order depends on map iteration, so use a set)
	if len(volumes) != 3 {
		t.Fatalf("expected 3 volumes, got %d: %v", len(volumes), volumes)
	}

	found := make(map[string]bool)
	for _, v := range volumes {
		found[v] = true
	}
	for _, want := range []string{"praktor-data", "praktor-wk-agent1", "praktor-home-agent1"} {
		if !found[want] {
			t.Errorf("expected volume %q not found in %v", want, volumes)
		}
	}
}

func TestScanArchiveVolumes_Empty(t *testing.T) {
	archivePath := createTestArchive(t, map[string]string{})

	volumes, err := scanArchiveVolumes(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(volumes) != 0 {
		t.Fatalf("expected 0 volumes, got %d: %v", len(volumes), volumes)
	}
}

func TestScanArchiveVolumes_NonPraktorEntries(t *testing.T) {
	archivePath := createTestArchive(t, map[string]string{
		"other-volume/file.txt":  "data",
		"random-file.txt":       "data",
		"praktor-data/db.sqlite": "data",
	})

	volumes, err := scanArchiveVolumes(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d: %v", len(volumes), volumes)
	}
	if volumes[0] != "praktor-data" {
		t.Errorf("expected praktor-data, got %q", volumes[0])
	}
}

func TestScanArchiveVolumes_InvalidFile(t *testing.T) {
	_, err := scanArchiveVolumes("/nonexistent/file.tar.zst")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestScanArchiveVolumes_InvalidZstd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.tar.zst")
	os.WriteFile(path, []byte("not zstd data"), 0644)

	_, err := scanArchiveVolumes(path)
	if err == nil {
		t.Fatal("expected error for invalid zstd data")
	}
}

// TestArchiveRoundTrip verifies that tar entries written with volume prefixes
// can be correctly scanned and split back into volume + relative path.
func TestArchiveRoundTrip(t *testing.T) {
	// Simulate what backupVolume produces: entries with volume prefix
	var buf bytes.Buffer
	zw, _ := zstd.NewWriter(&buf)
	tw := tar.NewWriter(zw)

	entries := []struct {
		name    string
		content string
		isDir   bool
	}{
		{"praktor-data/db.sqlite", "sqlite-data", false},
		{"praktor-data/subdir/", "", true},
		{"praktor-data/subdir/file.txt", "hello", false},
		{"praktor-wk-coder/main.go", "package main", false},
	}

	for _, e := range entries {
		hdr := &tar.Header{
			Name: e.name,
			Mode: 0644,
			Size: int64(len(e.content)),
		}
		if e.isDir {
			hdr.Typeflag = tar.TypeDir
			hdr.Size = 0
		}
		tw.WriteHeader(hdr)
		if len(e.content) > 0 {
			tw.Write([]byte(e.content))
		}
	}
	tw.Close()
	zw.Close()

	// Write to temp file for scanArchiveVolumes
	path := filepath.Join(t.TempDir(), "roundtrip.tar.zst")
	os.WriteFile(path, buf.Bytes(), 0644)

	// Scan volumes
	volumes, err := scanArchiveVolumes(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d: %v", len(volumes), volumes)
	}

	// Verify split on each entry
	zr, _ := zstd.NewReader(bytes.NewReader(buf.Bytes()))
	defer zr.Close()
	tr := tar.NewReader(zr)

	expected := []struct {
		vol string
		rel string
	}{
		{"praktor-data", "db.sqlite"},
		{"praktor-data", "subdir/"},
		{"praktor-data", "subdir/file.txt"},
		{"praktor-wk-coder", "main.go"},
	}

	for i, exp := range expected {
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		vol, rel := splitVolumePath(hdr.Name)
		if vol != exp.vol {
			t.Errorf("entry %d: vol = %q, want %q", i, vol, exp.vol)
		}
		if rel != exp.rel {
			t.Errorf("entry %d: rel = %q, want %q", i, rel, exp.rel)
		}

		// Verify content can be read
		if hdr.Size > 0 {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("entry %d: read content: %v", i, err)
			}
			if len(data) != int(hdr.Size) {
				t.Errorf("entry %d: content size = %d, want %d", i, len(data), hdr.Size)
			}
		}
	}

	// Should be EOF
	_, err = tr.Next()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}
