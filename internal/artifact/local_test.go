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
