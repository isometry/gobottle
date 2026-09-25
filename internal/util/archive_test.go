package util

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestExtractTarInTreeSymlink(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("hello from target\n")
	if err := tw.WriteHeader(&tar.Header{
		Name: "real/mytool", Mode: 0755, Size: int64(len(content)),
		ModTime: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: "mytool", Typeflag: tar.TypeSymlink, Linkname: "real/mytool",
		Mode: 0777, ModTime: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := extractTar(&buf, dest); err != nil {
		t.Fatalf("extractTar: %v", err)
	}

	linkPath := filepath.Join(dest, "mytool")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("symlink not created: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("mytool is not a symlink: mode = %v", info.Mode())
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if target != "real/mytool" {
		t.Errorf("symlink target = %q, want %q", target, "real/mytool")
	}
}

func TestExtractTarRejectsAbsoluteSymlink(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: "mytool", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd",
		Mode: 0777, ModTime: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := extractTar(&buf, t.TempDir()); err == nil {
		t.Fatal("expected rejection of absolute symlink target")
	}
}

func TestExtractTarRejectsEscapingSymlink(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: "mytool", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd",
		Mode: 0777, ModTime: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := extractTar(&buf, t.TempDir()); err == nil {
		t.Fatal("expected rejection of escaping symlink target")
	}
}

// TestExtractTarRejectsChainedSymlinkEscape covers a chain of symlinks that
// each look safe under a purely lexical check but whose combined resolution
// escapes destDir:
//
//   - p/q/s -> ../../z (z is a directory entry at the root; this lands back
//     inside destDir when checked lexically: destDir/p/q/../../z = destDir/z)
//   - u -> p/q/s/../../x (lexically destDir/p/x, since a naive check treats
//     "s" as literal text rather than a symlink to elsewhere; but once "s"
//     is actually followed to destDir/z, the remaining ".." components are
//     counted from there and land one level above destDir)
//   - a second entry also named "u", this time a regular file, overwriting
//     the symlink
//
// The last entry is what makes this a genuine proof of escape rather than
// an incidental collision: os.Mkdir/os.Symlink never follow a symlink at
// the final path component, so a nested write such as "u/evil" happens to
// fail safely (EEXIST on "u" itself) even without any escape awareness.
// os.OpenFile does follow it, though, so writing directly to the path "u"
// resolves through the whole chain and - under a lexical-check-only
// implementation - creates and writes the file one directory above destDir,
// no error returned. extractTar must reject this, and nothing may be
// created outside destDir.
func TestExtractTarRejectsChainedSymlinkEscape(t *testing.T) {
	dest := t.TempDir()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	write := func(hdr *tar.Header) {
		t.Helper()
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
	}
	write(&tar.Header{Name: "z", Typeflag: tar.TypeDir, Mode: 0755, ModTime: time.Unix(1700000000, 0)})
	write(&tar.Header{
		Name: "p/q/s", Typeflag: tar.TypeSymlink, Linkname: "../../z",
		Mode: 0777, ModTime: time.Unix(1700000000, 0),
	})
	write(&tar.Header{
		Name: "u", Typeflag: tar.TypeSymlink, Linkname: "p/q/s/../../x",
		Mode: 0777, ModTime: time.Unix(1700000000, 0),
	})
	content := []byte("evil\n")
	write(&tar.Header{
		Name: "u", Mode: 0644, Size: int64(len(content)),
		ModTime: time.Unix(1700000000, 0),
	})
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := extractTar(&buf, dest); err == nil {
		t.Fatal("expected rejection of chained symlink escape")
	}

	// Nothing must have escaped to dest's parent: "x" is where the chain
	// resolves to, once one directory above destDir.
	escaped := filepath.Join(filepath.Dir(dest), "x")
	if _, err := os.Stat(escaped); err == nil {
		t.Errorf("chained symlink escaped destDir: %s was created", escaped)
	}
}

// TestExtractTarAbsoluteMemberName covers an archive built with `tar -P`,
// whose member names carry a leading "/" (e.g. "/mytool" rather than
// "mytool"). That leading slash used to survive into relName and get handed
// to os.Root, which rejects absolute paths outright; extractTar must strip
// it and extract under destDir like any other member.
func TestExtractTarAbsoluteMemberName(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("hello from absolute member\n")
	if err := tw.WriteHeader(&tar.Header{
		Name: "/mytool", Mode: 0755, Size: int64(len(content)),
		ModTime: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := extractTar(&buf, dest); err != nil {
		t.Fatalf("extractTar: %v", err)
	}
	assertExtracted(t, dest, "mytool", string(content))
}
