package util

import (
	"archive/zip"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bz2Fixture is a tar.bz2 containing one file, "mytool", with the content
// "hello from bz2\n" (stdlib can decompress bzip2 but not compress it).
const bz2Fixture = `QlpoOTFBWSZTWfE0g1kAAD3bkNIQQAB/hAAQc0aeMAQAEAggAHUZnqT1GjTTRoGT1DygklDJkAAAA7Nn/IncCIbuCUQ9FESpiaq3beEWQ9hDYMtnTLNzBP4rJZNluH4AAigzyBtTQ6BQOnn6iKhrql+LuSKcKEh4mkGsgA==`

func assertExtracted(t *testing.T, destDir, name, wantContent string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(destDir, name))
	if err != nil {
		t.Fatalf("extracted file missing: %v", err)
	}
	if string(data) != wantContent {
		t.Errorf("extracted content = %q, want %q", data, wantContent)
	}
}

func TestExtractArchiveTarGz(t *testing.T) {
	src := filepath.Join(t.TempDir(), "mytool")
	if err := os.WriteFile(src, []byte("hello from tar.gz\n"), 0755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "mytool_1.0.0_darwin_arm64.tar.gz")
	if err := CreateTarGz(archive, map[string]string{"mytool": src}); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := ExtractArchive(archive, dest); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	assertExtracted(t, dest, "mytool", "hello from tar.gz\n")
}

func TestExtractArchiveZip(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "mytool_1.0.0_darwin_arm64.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("mytool")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello from zip\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := ExtractArchive(archive, dest); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	assertExtracted(t, dest, "mytool", "hello from zip\n")
}

func TestExtractArchiveTarBz2(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(bz2Fixture)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "mytool_1.0.0_linux_amd64.tar.bz2")
	if err := os.WriteFile(archive, data, 0644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := ExtractArchive(archive, dest); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	assertExtracted(t, dest, "mytool", "hello from bz2\n")
}

func TestExtractArchiveUnsupported(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "mytool.tar.xz")
	if err := os.WriteFile(archive, []byte("not really"), 0644); err != nil {
		t.Fatal(err)
	}
	err := ExtractArchive(archive, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unsupported archive format") {
		t.Fatalf("want unsupported-format error, got %v", err)
	}
}

func TestExtractZipRejectsTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "evil.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("escape\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := ExtractZip(archive, t.TempDir()); err == nil {
		t.Fatal("expected path-traversal rejection")
	}
}
