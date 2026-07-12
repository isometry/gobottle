package bottle

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isometry/gobottle/internal/platform"
)

// makeArtifact writes a minimal artifact tar.gz containing the named
// "binaries" at its root and returns its path.
func makeArtifact(t *testing.T, binaries ...string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "artifact.tar.gz")

	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)
	for _, name := range binaries {
		content := []byte("#!/bin/sh\necho " + name + "\n")
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0755, Size: int64(len(content)),
			ModTime: time.Unix(1700000000, 0),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func testBuildOptions(artifact string) BuildOptions {
	return BuildOptions{
		Formula:      "mytool",
		Version:      "1.2.3",
		Platform:     platform.Platform{Tag: "arm64_sonoma", OS: "darwin", Arch: "arm64", OSVersion: "sonoma", OSVersionMajor: 14},
		ArtifactPath: artifact,
		Binaries:     []BinaryInstall{{Name: "mytool", InstallPath: "bin"}},
		Cellar:       ":any_skip_relocation",
		Tap:          "acme/homebrew-tap",
		FormulaRb:    "class Mytool < Formula\nend\n",
		SourceDate:   time.Unix(1700000000, 0),
	}
}

func buildOnce(t *testing.T, opts BuildOptions) *Bottle {
	t.Helper()
	builder, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = builder.Close() })

	b, err := builder.Build(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBuildDeterministic(t *testing.T) {
	artifact := makeArtifact(t, "mytool")
	opts := testBuildOptions(artifact)

	b1 := buildOnce(t, opts)
	b2 := buildOnce(t, opts)

	if b1.SHA256 != b2.SHA256 {
		t.Errorf("bottle SHA256 not deterministic: %s vs %s", b1.SHA256, b2.SHA256)
	}
	if b1.UncompressedSHA256 != b2.UncompressedSHA256 {
		t.Errorf("uncompressed SHA256 not deterministic: %s vs %s", b1.UncompressedSHA256, b2.UncompressedSHA256)
	}
	if b1.UncompressedSize == 0 {
		t.Error("uncompressed size not recorded")
	}
}

func TestBuildContents(t *testing.T) {
	artifact := makeArtifact(t, "mytool")
	b := buildOnce(t, testBuildOptions(artifact))

	if b.BottleName() != "mytool--1.2.3.arm64_sonoma.bottle.tar.gz" {
		t.Errorf("unexpected bottle name %s", b.BottleName())
	}

	entries := readTarGz(t, b.Path)
	for _, want := range []string{
		"mytool/1.2.3/bin/mytool",
		"mytool/1.2.3/.brew/mytool.rb",
		"mytool/1.2.3/.brew/INSTALL_RECEIPT.json",
	} {
		if _, ok := entries[want]; !ok {
			t.Errorf("bottle missing entry %s (have %v)", want, keys(entries))
		}
	}

	if rb := entries["mytool/1.2.3/.brew/mytool.rb"]; !strings.Contains(rb, "class Mytool < Formula") {
		t.Errorf("embedded formula wrong: %q", rb)
	}
	receipt := entries["mytool/1.2.3/.brew/INSTALL_RECEIPT.json"]
	for _, want := range []string{`"built_as_bottle":true`, `"tap":"acme/homebrew-tap"`, `"stable":"1.2.3"`} {
		if !strings.Contains(receipt, want) {
			t.Errorf("receipt missing %s: %s", want, receipt)
		}
	}
}

func TestBuildInstallPaths(t *testing.T) {
	artifact := makeArtifact(t, "mytool", "helper")
	opts := testBuildOptions(artifact)
	opts.Binaries = []BinaryInstall{
		{Name: "mytool"}, // empty InstallPath defaults to bin
		{Name: "helper", InstallPath: "libexec"},
	}

	b := buildOnce(t, opts)

	entries := readTarGz(t, b.Path)
	for _, want := range []string{
		"mytool/1.2.3/bin/mytool",
		"mytool/1.2.3/libexec/helper",
	} {
		if _, ok := entries[want]; !ok {
			t.Errorf("bottle missing entry %s (have %v)", want, keys(entries))
		}
	}
}

// makeZipArtifact writes a minimal zip artifact (goreleaser's other
// archive format) containing the named binaries at its root.
func makeZipArtifact(t *testing.T, binaries ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "artifact.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, name := range binaries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("#!/bin/sh\necho " + name + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildFromZipArtifact(t *testing.T) {
	artifact := makeZipArtifact(t, "mytool")
	opts := testBuildOptions(artifact)

	b := buildOnce(t, opts)

	entries := readTarGz(t, b.Path)
	if got := entries["mytool/1.2.3/bin/mytool"]; !strings.Contains(got, "echo mytool") {
		t.Errorf("zip-sourced binary content = %q (have %v)", got, keys(entries))
	}
}

func TestBuildExtraFiles(t *testing.T) {
	artifact := makeArtifact(t, "mytool")
	opts := testBuildOptions(artifact)

	comp := filepath.Join(t.TempDir(), "mytool.bash")
	if err := os.WriteFile(comp, []byte("completion-for-bash\n"), 0644); err != nil {
		t.Fatal(err)
	}
	opts.ExtraFiles = map[string]string{"etc/bash_completion.d/mytool": comp}

	b1 := buildOnce(t, opts)
	entries := readTarGz(t, b1.Path)
	if got := entries["mytool/1.2.3/etc/bash_completion.d/mytool"]; got != "completion-for-bash\n" {
		t.Errorf("extra file content = %q (have %v)", got, keys(entries))
	}

	b2 := buildOnce(t, opts)
	if b1.SHA256 != b2.SHA256 {
		t.Errorf("bottle with extra files not deterministic: %s vs %s", b1.SHA256, b2.SHA256)
	}
}

func TestBuildRebuildNaming(t *testing.T) {
	artifact := makeArtifact(t, "mytool")
	opts := testBuildOptions(artifact)
	opts.Rebuild = 2

	b := buildOnce(t, opts)

	if b.BottleName() != "mytool--1.2.3.arm64_sonoma.bottle.2.tar.gz" {
		t.Errorf("rebuild missing from filename: %s", b.BottleName())
	}
	if b.VersionRebuild() != "1.2.3-2" {
		t.Errorf("VersionRebuild = %s, want 1.2.3-2", b.VersionRebuild())
	}
	if b.RefName() != "1.2.3.arm64_sonoma.2" {
		t.Errorf("RefName = %s, want 1.2.3.arm64_sonoma.2", b.RefName())
	}
}

func TestBuildIsolation(t *testing.T) {
	// Two artifacts with same binary name but different contents must not
	// bleed into each other's bottles.
	artifactA := makeArtifact(t, "mytool")
	dir := t.TempDir()
	pB := filepath.Join(dir, "artifact-b.tar.gz")
	f, _ := os.Create(pB)
	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)
	content := []byte("#!/bin/sh\necho DIFFERENT\n")
	_ = tw.WriteHeader(&tar.Header{Name: "mytool", Mode: 0755, Size: int64(len(content)), ModTime: time.Unix(1700000000, 0)})
	_, _ = tw.Write(content)
	_ = tw.Close()
	_ = gzw.Close()
	_ = f.Close()

	builder, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = builder.Close() }()

	optsA := testBuildOptions(artifactA)
	optsB := testBuildOptions(pB)
	optsB.Platform = platform.Platform{Tag: "x86_64_linux", OS: "linux", Arch: "amd64"}

	bA, err := builder.Build(context.Background(), optsA)
	if err != nil {
		t.Fatal(err)
	}
	bB, err := builder.Build(context.Background(), optsB)
	if err != nil {
		t.Fatal(err)
	}

	entriesB := readTarGz(t, bB.Path)
	if got := entriesB["mytool/1.2.3/bin/mytool"]; !strings.Contains(got, "DIFFERENT") {
		t.Errorf("second bottle contains first artifact's binary: %q", got)
	}
	if bA.SHA256 == bB.SHA256 {
		t.Error("different artifacts produced identical bottles")
	}
}

func readTarGz(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			entries[hdr.Name] = string(data)
		}
	}
	return entries
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
