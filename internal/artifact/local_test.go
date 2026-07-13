package artifact

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDist creates a dist dir containing one fake archive and a
// goreleaser-style versioned checksum manifest whose entry is `sum`.
func writeDist(t *testing.T, sum string) (dir string, archiveName string) {
	t.Helper()
	dir = t.TempDir()
	archiveName = "mytool_1.2.3_darwin_arm64.tar.gz"
	if err := os.WriteFile(filepath.Join(dir, archiveName), []byte("archive bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("%s  %s\n", sum, archiveName)
	if err := os.WriteFile(filepath.Join(dir, "mytool_1.2.3_SHA256SUMS"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	return dir, archiveName
}

func TestLocalSourceVersionedChecksums(t *testing.T) {
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte("archive bytes")))
	dir, archiveName := writeDist(t, sum)

	src, err := NewLocalSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := src.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("got %d artifacts, want 1", len(artifacts))
	}
	if artifacts[0].Checksum != sum {
		t.Errorf("checksum = %q, want manifest value %q", artifacts[0].Checksum, sum)
	}

	if _, err := src.Fetch(context.Background(), artifacts[0], t.TempDir()); err != nil {
		t.Errorf("Fetch with matching checksum: %v", err)
	}
	_ = archiveName
}

// writeGoreleaserDist lays out a goreleaser-style dist with raw binaries
// and an artifacts.json describing them (plus an Archive entry that must
// be ignored in binary mode).
func writeGoreleaserDist(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	for dir, name := range map[string]string{
		"mytool_darwin_arm64_v8.0": "mytool",
		"helper_darwin_arm64_v8.0": "helper",
		"mytool_linux_amd64_v1":    "mytool",
	} {
		if err := os.MkdirAll(filepath.Join(dist, dir), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dist, dir, name), []byte("binary "+dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `[
	  {"name":"mytool","path":"dist/mytool_darwin_arm64_v8.0/mytool","goos":"darwin","goarch":"arm64","type":"Binary","extra":{"Binary":"mytool"}},
	  {"name":"helper","path":"dist/helper_darwin_arm64_v8.0/helper","goos":"darwin","goarch":"arm64","type":"Binary","extra":{"Binary":"helper"}},
	  {"name":"mytool","path":"dist/mytool_linux_amd64_v1/mytool","goos":"linux","goarch":"amd64","type":"Binary","extra":{"Binary":"mytool"}},
	  {"name":"mytool_1.2.3_darwin_arm64.zip","path":"dist/mytool_1.2.3_darwin_arm64.zip","goos":"darwin","goarch":"arm64","type":"Archive"}
	]`
	if err := os.WriteFile(filepath.Join(dist, "artifacts.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	return dist
}

func TestLocalSourceBinaryArtifacts(t *testing.T) {
	dist := writeGoreleaserDist(t)
	src, err := NewLocalSource(dist)
	if err != nil {
		t.Fatal(err)
	}

	artifacts, err := src.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("got %d artifacts, want 2 platforms: %+v", len(artifacts), artifacts)
	}
	if artifacts[0].OS != "darwin" || artifacts[0].Arch != "arm64" ||
		artifacts[1].OS != "linux" || artifacts[1].Arch != "amd64" {
		t.Errorf("unexpected platform grouping: %+v", artifacts)
	}

	dir, err := src.Fetch(context.Background(), artifacts[0], t.TempDir())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("Fetch must return a directory for binary artifacts: %v", err)
	}
	for name, want := range map[string]string{
		"mytool": "binary mytool_darwin_arm64_v8.0",
		"helper": "binary helper_darwin_arm64_v8.0",
	} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("staged binary %s missing: %v", name, err)
			continue
		}
		if string(data) != want {
			t.Errorf("staged %s = %q, want %q", name, data, want)
		}
	}
}

func TestLocalSourceArchiveFallbackWithoutBinaries(t *testing.T) {
	// artifacts.json with only Archive entries must fall back to archive
	// discovery.
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte("archive bytes")))
	dir, archiveName := writeDist(t, sum)
	manifest := fmt.Sprintf(`[{"name":%q,"path":"dist/%s","goos":"darwin","goarch":"arm64","type":"Archive"}]`, archiveName, archiveName)
	if err := os.WriteFile(filepath.Join(dir, "artifacts.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	src, err := NewLocalSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := src.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].Name != archiveName {
		t.Fatalf("archive fallback failed: %+v", artifacts)
	}
}

func TestLocalSourceChecksumMismatchRejected(t *testing.T) {
	// A manifest entry that doesn't match the file proves the cross-check
	// is active (not silently self-computed).
	dir, _ := writeDist(t, strings.Repeat("0", 64))

	src, err := NewLocalSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := src.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("got %d artifacts, want 1", len(artifacts))
	}
	if _, err := src.Fetch(context.Background(), artifacts[0], t.TempDir()); err == nil {
		t.Error("Fetch must fail when the manifest checksum mismatches")
	}
}
