package bottle

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/isometry/gobottle/internal/platform"
)

// artifactEntry is one tar entry for makeArtifactEntries: a regular file
// named Name with content Value, unless Value starts with "-> ", in which
// case it is a symlink entry with that prefix stripped as its target.
type artifactEntry struct {
	Name  string
	Value string
}

// makeArtifact writes a minimal artifact tar.gz containing the named
// "binaries" at its root and returns its path.
func makeArtifact(t *testing.T, binaries ...string) string {
	t.Helper()
	entries := make([]artifactEntry, len(binaries))
	for i, name := range binaries {
		entries[i] = artifactEntry{Name: name, Value: "#!/bin/sh\necho " + name + "\n"}
	}
	return makeArtifactEntries(t, entries)
}

// makeArtifactEntries writes a minimal artifact tar.gz from an ordered list
// of entries and returns its path. See artifactEntry for the symlink
// convention.
func makeArtifactEntries(t *testing.T, entries []artifactEntry) string {
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
	for _, e := range entries {
		if target, ok := strings.CutPrefix(e.Value, "-> "); ok {
			if err := tw.WriteHeader(&tar.Header{
				Name:     e.Name,
				Typeflag: tar.TypeSymlink,
				Linkname: target,
				Mode:     0777,
				ModTime:  time.Unix(1700000000, 0),
			}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		content := []byte(e.Value)
		if err := tw.WriteHeader(&tar.Header{
			Name: e.Name, Mode: 0755, Size: int64(len(content)),
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
	opts.Binaries = []BinaryInstall{{Name: "mytool", InstallPath: "bin", Links: []string{"kubectl-mytool"}}}

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

// TestBuildBinaryLinks covers the symlink entries a configured link produces:
// the default bin install path and a custom install_path, correct Linkname,
// host-independent mode, and that the entry's parent directory exists too.
func TestBuildBinaryLinks(t *testing.T) {
	tests := []struct {
		name        string
		installPath string
		wantPath    string
		wantDir     string
	}{
		{
			name:        "default bin install path",
			installPath: "",
			wantPath:    "mytool/1.2.3/bin/kubectl-mytool",
			wantDir:     "mytool/1.2.3/bin/",
		},
		{
			name:        "custom install path",
			installPath: "libexec",
			wantPath:    "mytool/1.2.3/libexec/kubectl-mytool",
			wantDir:     "mytool/1.2.3/libexec/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifact := makeArtifact(t, "mytool")
			opts := testBuildOptions(artifact)
			opts.Binaries = []BinaryInstall{
				{Name: "mytool", InstallPath: tt.installPath, Links: []string{"kubectl-mytool"}},
			}

			b := buildOnce(t, opts)
			headers := readTarHeaders(t, b.Path)

			hdr, ok := headers[tt.wantPath]
			if !ok {
				t.Fatalf("bottle missing link entry %s (have %v)", tt.wantPath, headerNames(headers))
			}
			if hdr.Typeflag != tar.TypeSymlink {
				t.Errorf("typeflag = %v, want TypeSymlink", hdr.Typeflag)
			}
			if hdr.Linkname != "mytool" {
				t.Errorf("linkname = %q, want %q", hdr.Linkname, "mytool")
			}
			if hdr.Mode != 0777 {
				t.Errorf("mode = %o, want 0777", hdr.Mode)
			}
			if _, ok := headers[tt.wantDir]; !ok {
				t.Errorf("bottle missing parent dir entry %s (have %v)", tt.wantDir, headerNames(headers))
			}
		})
	}
}

// TestBuildBinaryLinksAcrossPlatforms builds the same directory artifact
// twice, for two different platforms, with links configured. Both builds
// reuse the artifact directory as extractDir, so this exercises the same
// on-disk-symlink-avoidance path TestBuildIsolation covers for plain
// binaries: the tar writer must not attempt os.Symlink on the shared
// artifact.
func TestBuildBinaryLinksAcrossPlatforms(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mytool"), []byte("#!/bin/sh\necho mytool\n"), 0755); err != nil {
		t.Fatal(err)
	}

	builder, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = builder.Close() })

	optsA := testBuildOptions(dir)
	optsA.Binaries = []BinaryInstall{{Name: "mytool", Links: []string{"kubectl-mytool"}}}

	optsB := testBuildOptions(dir)
	optsB.Binaries = []BinaryInstall{{Name: "mytool", Links: []string{"kubectl-mytool"}}}
	optsB.Platform = platform.Platform{Tag: "x86_64_linux", OS: "linux", Arch: "amd64"}

	bA, err := builder.Build(context.Background(), optsA)
	if err != nil {
		t.Fatalf("Build() for platform A: %v", err)
	}
	bB, err := builder.Build(context.Background(), optsB)
	if err != nil {
		t.Fatalf("Build() for platform B: %v", err)
	}

	for _, b := range []*Bottle{bA, bB} {
		headers := readTarHeaders(t, b.Path)
		hdr, ok := headers["mytool/1.2.3/bin/kubectl-mytool"]
		if !ok {
			t.Fatalf("bottle %s missing link entry (have %v)", b.Path, headerNames(headers))
		}
		if hdr.Typeflag != tar.TypeSymlink {
			t.Errorf("bottle %s: typeflag = %v, want TypeSymlink", b.Path, hdr.Typeflag)
		}
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

func TestBuildFromDirectoryArtifact(t *testing.T) {
	// Raw-binary sources stage a directory instead of an archive.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mytool"), []byte("#!/bin/sh\necho mytool\n"), 0755); err != nil {
		t.Fatal(err)
	}
	opts := testBuildOptions(dir)

	b := buildOnce(t, opts)

	entries := readTarGz(t, b.Path)
	if got := entries["mytool/1.2.3/bin/mytool"]; !strings.Contains(got, "echo mytool") {
		t.Errorf("dir-sourced binary content = %q (have %v)", got, keys(entries))
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

// readTarHeaders reads every entry's header from a bottle tarball, keyed by
// name. Unlike readTarGz, it keeps entry metadata (type, mode, link target)
// instead of just regular-file content.
func readTarHeaders(t *testing.T, path string) map[string]*tar.Header {
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
	headers := map[string]*tar.Header{}
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		h := *hdr
		headers[hdr.Name] = &h
	}
	return headers
}

func headerNames(m map[string]*tar.Header) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestBuildArtifactSymlinks covers release archives that carry symlinks
// alongside the configured binaries: an alias link that isn't itself
// configured must be dropped, and a configured binary that is a symlink
// must be resolved to a regular file. Dangling and escaping targets must
// fail the build (extraction creates such links as-is; ResolveBinary rejects
// them when a configured binary uses one).
func TestBuildArtifactSymlinks(t *testing.T) {
	tests := []struct {
		name        string
		entries     []artifactEntry
		wantErr     string
		wantAbsent  []string
		wantContent string
	}{
		{
			name: "unconfigured alias symlink is dropped",
			entries: []artifactEntry{
				{Name: "mytool", Value: "#!/bin/sh\necho mytool\n"},
				{Name: "kubectl-mytool", Value: "-> mytool"},
			},
			wantContent: "#!/bin/sh\necho mytool\n",
			wantAbsent:  []string{"mytool/1.2.3/bin/kubectl-mytool"},
		},
		{
			name: "in-tree symlink dereferenced to a regular file",
			entries: []artifactEntry{
				{Name: "real/mytool", Value: "#!/bin/sh\necho real\n"},
				{Name: "mytool", Value: "-> real/mytool"},
			},
			wantContent: "#!/bin/sh\necho real\n",
		},
		{
			name:    "dangling symlink fails the build",
			entries: []artifactEntry{{Name: "mytool", Value: "-> nonexistent"}},
			wantErr: "mytool",
		},
		{
			name:    "escaping relative symlink fails the build",
			entries: []artifactEntry{{Name: "mytool", Value: "-> ../../x"}},
			wantErr: "mytool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifact := makeArtifactEntries(t, tt.entries)
			opts := testBuildOptions(artifact)

			builder, err := NewBuilder()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = builder.Close() })

			b, err := builder.Build(context.Background(), opts)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Build() succeeded, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Build() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}

			headers := readTarHeaders(t, b.Path)
			hdr, ok := headers["mytool/1.2.3/bin/mytool"]
			if !ok {
				t.Fatalf("bottle missing bin/mytool (have %v)", headerNames(headers))
			}
			if hdr.Typeflag != tar.TypeReg {
				t.Errorf("bin/mytool typeflag = %v, want TypeReg", hdr.Typeflag)
			}
			for _, absent := range tt.wantAbsent {
				if _, ok := headers[absent]; ok {
					t.Errorf("bottle unexpectedly contains %s", absent)
				}
			}
			if tt.wantContent != "" {
				entries := readTarGz(t, b.Path)
				if got := entries["mytool/1.2.3/bin/mytool"]; got != tt.wantContent {
					t.Errorf("bin/mytool content = %q, want %q", got, tt.wantContent)
				}
			}
		})
	}
}

// TestBuildArtifactEscapingSymlinkConfiguredBinary covers a configured binary
// that is an absolute symlink to a file that exists outside the artifact:
// extraction keeps the link, and ResolveBinary must refuse it at use time.
func TestBuildArtifactEscapingSymlinkConfiguredBinary(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret\n"), 0644); err != nil {
		t.Fatal(err)
	}
	artifact := makeArtifactEntries(t, []artifactEntry{{Name: "mytool", Value: "-> " + outside}})

	builder, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = builder.Close() })

	_, err = builder.Build(context.Background(), testBuildOptions(artifact))
	if err == nil {
		t.Fatal("Build() succeeded, want error for binary symlink escaping the artifact")
	}
	if !strings.Contains(err.Error(), "resolves outside the artifact") {
		t.Errorf("Build() error = %v, want containing %q", err, "resolves outside the artifact")
	}
}

// TestBuildArtifactAlreadyContainsLink covers a configured link whose name
// the release archive already occupies, either as a symlink (a goreleaser
// archives.files alias) or as a full duplicated regular-file copy: either
// way, gobottle's own generated symlink must win and the archive's copy must
// never surface under that name — exactly one bin/kubectl-mytool entry,
// which is the symlink.
func TestBuildArtifactAlreadyContainsLink(t *testing.T) {
	tests := []struct {
		name    string
		entries []artifactEntry
	}{
		{
			name: "archive ships a symlink under the link name",
			entries: []artifactEntry{
				{Name: "mytool", Value: "#!/bin/sh\necho mytool\n"},
				{Name: "kubectl-mytool", Value: "-> mytool"},
			},
		},
		{
			name: "archive ships a full regular-file copy under the link name",
			entries: []artifactEntry{
				{Name: "mytool", Value: "#!/bin/sh\necho mytool\n"},
				{Name: "kubectl-mytool", Value: "#!/bin/sh\necho mytool\n"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifact := makeArtifactEntries(t, tt.entries)
			opts := testBuildOptions(artifact)
			opts.Binaries = []BinaryInstall{{Name: "mytool", Links: []string{"kubectl-mytool"}}}

			b := buildOnce(t, opts)
			headers := readTarHeaders(t, b.Path)

			hdr, ok := headers["mytool/1.2.3/bin/kubectl-mytool"]
			if !ok {
				t.Fatalf("bottle missing bin/kubectl-mytool (have %v)", headerNames(headers))
			}
			if hdr.Typeflag != tar.TypeSymlink {
				t.Errorf("bin/kubectl-mytool typeflag = %v, want TypeSymlink (gobottle's own link)", hdr.Typeflag)
			}
			if hdr.Linkname != "mytool" {
				t.Errorf("bin/kubectl-mytool linkname = %q, want %q", hdr.Linkname, "mytool")
			}
		})
	}
}

// TestBuildIgnoredSymlinks covers artifact symlinks Build does not bottle: an
// alias a goreleaser archive ships without a matching binaries[].links entry
// is recorded on Bottle.IgnoredSymlinks, a declared link is not, and neither
// is a plain regular file.
func TestBuildIgnoredSymlinks(t *testing.T) {
	artifact := makeArtifactEntries(t, []artifactEntry{
		{Name: "mytool", Value: "#!/bin/sh\necho mytool\n"},
		{Name: "kubectl-mytool", Value: "-> mytool"},
		{Name: "kubectl_complete-mytool", Value: "-> mytool"},
		{Name: "LICENSE", Value: "MIT\n"},
	})
	opts := testBuildOptions(artifact)
	opts.Binaries = []BinaryInstall{{Name: "mytool", Links: []string{"kubectl-mytool"}}}

	b := buildOnce(t, opts)

	want := []IgnoredSymlink{{Name: "kubectl_complete-mytool", Target: "mytool"}}
	if !reflect.DeepEqual(b.IgnoredSymlinks, want) {
		t.Errorf("IgnoredSymlinks = %+v, want %+v", b.IgnoredSymlinks, want)
	}
}

// TestBuildIgnoredSymlinksBinaryItselfSymlink covers a configured binary that
// is itself a symlink inside the artifact (e.g. mytool -> real/mytool): it is
// resolved and bottled as usual, and must not also surface as an ignored
// symlink just because its artifact-root entry is one.
func TestBuildIgnoredSymlinksBinaryItselfSymlink(t *testing.T) {
	artifact := makeArtifactEntries(t, []artifactEntry{
		{Name: "real/mytool", Value: "#!/bin/sh\necho real\n"},
		{Name: "mytool", Value: "-> real/mytool"},
	})
	opts := testBuildOptions(artifact)

	b := buildOnce(t, opts)

	if len(b.IgnoredSymlinks) != 0 {
		t.Errorf("IgnoredSymlinks = %+v, want none", b.IgnoredSymlinks)
	}
}

// TestBuildIgnoredSymlinksDirectoryArtifact covers the extractDir ==
// ArtifactPath path: raw-binary sources staged as a directory rather than an
// archive.
func TestBuildIgnoredSymlinksDirectoryArtifact(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mytool"), []byte("#!/bin/sh\necho mytool\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("mytool", filepath.Join(dir, "kubectl-mytool")); err != nil {
		t.Fatal(err)
	}
	opts := testBuildOptions(dir)

	b := buildOnce(t, opts)

	want := []IgnoredSymlink{{Name: "kubectl-mytool", Target: "mytool"}}
	if !reflect.DeepEqual(b.IgnoredSymlinks, want) {
		t.Errorf("IgnoredSymlinks = %+v, want %+v", b.IgnoredSymlinks, want)
	}
}

// TestBuildDirectoryArtifactSymlinkDereferenced runs the in-tree relative
// symlink case (row b of the release-archive table) against a directory
// artifact with a real on-disk symlink, covering the extractDir =
// ArtifactPath path that never goes through extractTar.
func TestBuildDirectoryArtifactSymlinkDereferenced(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "mytool"), []byte("#!/bin/sh\necho real\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "mytool"), filepath.Join(dir, "mytool")); err != nil {
		t.Fatal(err)
	}

	b := buildOnce(t, testBuildOptions(dir))

	headers := readTarHeaders(t, b.Path)
	hdr, ok := headers["mytool/1.2.3/bin/mytool"]
	if !ok {
		t.Fatalf("bottle missing bin/mytool (have %v)", headerNames(headers))
	}
	if hdr.Typeflag != tar.TypeReg {
		t.Errorf("bin/mytool typeflag = %v, want TypeReg", hdr.Typeflag)
	}

	entries := readTarGz(t, b.Path)
	if got := entries["mytool/1.2.3/bin/mytool"]; got != "#!/bin/sh\necho real\n" {
		t.Errorf("bin/mytool content = %q, want dereferenced target content", got)
	}
}

// TestBuildDirectoryArtifactSymlinkEscapesAbsolute covers the absolute-target
// case: a directory artifact holding an on-disk symlink that points outside
// the artifact directory entirely must fail the build.
func TestBuildDirectoryArtifactSymlinkEscapesAbsolute(t *testing.T) {
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret")
	if err := os.WriteFile(outsideFile, []byte("secret\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.Symlink(outsideFile, filepath.Join(dir, "mytool")); err != nil {
		t.Fatal(err)
	}

	builder, err := NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = builder.Close() })

	_, err = builder.Build(context.Background(), testBuildOptions(dir))
	if err == nil {
		t.Fatal("Build() succeeded, want error for symlink escaping the artifact directory")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("Build() error = %v, want mention of escaping the artifact", err)
	}
}

// TestResolveBinary exercises ResolveBinary directly, independent of Build,
// covering a plain regular file, an in-tree symlink, and every error case:
// missing, dangling, escaping, and not-a-regular-file.
func TestResolveBinary(t *testing.T) {
	t.Run("regular file resolves to itself", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "mytool"), []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}

		resolved, err := ResolveBinary(dir, "mytool")
		if err != nil {
			t.Fatal(err)
		}
		wantDir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		if resolved != filepath.Join(wantDir, "mytool") {
			t.Errorf("resolved = %q, want %q", resolved, filepath.Join(wantDir, "mytool"))
		}
	})

	t.Run("in-tree symlink dereferenced", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "real"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "real", "mytool"), []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("real", "mytool"), filepath.Join(dir, "mytool")); err != nil {
			t.Fatal(err)
		}

		resolved, err := ResolveBinary(dir, "mytool")
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(resolved)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Errorf("resolved %s is not a regular file", resolved)
		}
	})

	t.Run("missing binary errors", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := ResolveBinary(dir, "nope"); err == nil {
			t.Fatal("expected error for missing binary")
		}
	})

	t.Run("dangling symlink errors", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink("nonexistent", filepath.Join(dir, "mytool")); err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveBinary(dir, "mytool"); err == nil {
			t.Fatal("expected error for dangling symlink")
		}
	})

	t.Run("escaping symlink errors", func(t *testing.T) {
		outside := t.TempDir()
		outsideFile := filepath.Join(outside, "secret")
		if err := os.WriteFile(outsideFile, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err := os.Symlink(outsideFile, filepath.Join(dir, "mytool")); err != nil {
			t.Fatal(err)
		}

		_, err := ResolveBinary(dir, "mytool")
		if err == nil {
			t.Fatal("expected error for symlink escaping dir")
		}
		if !strings.Contains(err.Error(), "outside") {
			t.Errorf("error = %v, want mention of escaping the artifact", err)
		}
	})

	t.Run("directory target errors", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "mytool"), 0755); err != nil {
			t.Fatal(err)
		}
		_, err := ResolveBinary(dir, "mytool")
		if err == nil {
			t.Fatal("expected error for non-regular-file target")
		}
		if !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("error = %v, want mention of regular file", err)
		}
	})
}
