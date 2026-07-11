package oci

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/isometry/gobottle/internal/bottle"
	"github.com/isometry/gobottle/internal/platform"
)

// newTestRegistry starts an in-memory OCI registry and returns its host.
func newTestRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// makeBottle builds a real (tiny) bottle via the bottle builder.
func makeBottle(t *testing.T, plat platform.Platform, rebuild int) *bottle.Bottle {
	t.Helper()
	artifact := makeTestArtifact(t)
	builder, err := bottle.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = builder.Close() })

	b, err := builder.Build(context.Background(), bottle.BuildOptions{
		Formula:      "mytool",
		Version:      "1.2.3",
		Platform:     plat,
		ArtifactPath: artifact,
		Binaries:     []bottle.BinaryInstall{{Name: "mytool", InstallPath: "bin"}},
		Cellar:       ":any_skip_relocation",
		Rebuild:      rebuild,
		Tap:          "acme/homebrew-tap",
		FormulaRb:    "class Mytool < Formula\nend\n",
		SourceDate:   time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func darwinArm() platform.Platform {
	return platform.Platform{Tag: "arm64_sonoma", OS: "darwin", Arch: "arm64", OSVersion: "sonoma", OSVersionMajor: 14}
}

func linuxAmd() platform.Platform {
	return platform.Platform{Tag: "x86_64_linux", OS: "linux", Arch: "amd64"}
}

func TestPushIndexStructure(t *testing.T) {
	host := newTestRegistry(t)
	bArm := makeBottle(t, darwinArm(), 0)
	bLinux := makeBottle(t, linuxAmd(), 0)

	pub, err := NewPublisher(host, "acme/tap", "mytool", "test-token", PublishOptions{
		SourceURL: "https://github.com/acme/homebrew-tap",
		Homepage:  "https://acme.dev",
		License:   "MIT",
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := pub.Push(context.Background(), []*bottle.Bottle{bArm, bLinux}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 index result, got %d", len(results))
	}
	if results[0].Tag != "1.2.3" {
		t.Errorf("tag = %s, want 1.2.3", results[0].Tag)
	}
	if want := host + "/acme/tap/mytool:1.2.3"; results[0].Ref != want {
		t.Errorf("ref = %s, want %s", results[0].Ref, want)
	}

	// Fetch back and verify the structure brew depends on.
	ref, err := name.ParseReference(results[0].Ref)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := remote.Index(ref)
	if err != nil {
		t.Fatalf("pushed tag is not an index: %v", err)
	}

	mt, err := idx.MediaType()
	if err != nil {
		t.Fatal(err)
	}
	if mt != types.OCIImageIndex {
		t.Errorf("index media type = %s, want %s", mt, types.OCIImageIndex)
	}

	manifest, err := idx.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Manifests) != 2 {
		t.Fatalf("expected 2 child manifests, got %d", len(manifest.Manifests))
	}
	if manifest.Annotations["com.github.package.type"] != "homebrew_bottle" {
		t.Error("index missing com.github.package.type=homebrew_bottle annotation")
	}

	byRef := map[string]v1.Descriptor{}
	for _, desc := range manifest.Manifests {
		byRef[desc.Annotations["org.opencontainers.image.ref.name"]] = desc
	}

	for _, b := range []*bottle.Bottle{bArm, bLinux} {
		desc, ok := byRef[b.RefName()]
		if !ok {
			t.Fatalf("no child manifest with ref.name %s", b.RefName())
		}
		if got := desc.Annotations["sh.brew.bottle.digest"]; got != b.SHA256 {
			t.Errorf("sh.brew.bottle.digest = %s, want %s", got, b.SHA256)
		}
		tabJSON := desc.Annotations["sh.brew.tab"]
		var tab map[string]any
		if err := json.Unmarshal([]byte(tabJSON), &tab); err != nil {
			t.Fatalf("sh.brew.tab is not valid JSON: %v", err)
		}
		if tab["built_as_bottle"] != true {
			t.Error("sh.brew.tab missing built_as_bottle=true")
		}
		if desc.Platform == nil {
			t.Fatal("child descriptor missing platform")
		}
		if desc.Platform.OS != b.Platform.OS {
			t.Errorf("platform os = %s, want %s", desc.Platform.OS, b.Platform.OS)
		}
	}

	// Darwin child carries the macOS version.
	if got := byRef[bArm.RefName()].Platform.OSVersion; got != "macOS 14" {
		t.Errorf("darwin os.version = %q, want %q", got, "macOS 14")
	}

	// The layer blob must be byte-addressable by the bottle SHA256 — this is
	// exactly how brew fetches it.
	repo, err := name.NewRepository(host + "/acme/tap/mytool")
	if err != nil {
		t.Fatal(err)
	}
	layer, err := remote.Layer(repo.Digest("sha256:" + bArm.SHA256))
	if err != nil {
		t.Fatal(err)
	}
	rc, err := layer.Compressed()
	if err != nil {
		t.Fatalf("blob for bottle sha256 not fetchable: %v", err)
	}
	defer rc.Close()
	blobBytes, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	fileBytes, err := readFile(bArm.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(blobBytes) != string(fileBytes) {
		t.Error("registry blob differs from bottle file bytes")
	}

	// Child image manifests: OCI media types and a real config blob.
	childImg, err := idx.Image(byRef[bArm.RefName()].Digest)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := childImg.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OS != "darwin" || cfg.Architecture != "arm64" {
		t.Errorf("config blob os/arch = %s/%s", cfg.OS, cfg.Architecture)
	}
	if len(cfg.RootFS.DiffIDs) != 1 || cfg.RootFS.DiffIDs[0].Hex != bArm.UncompressedSHA256 {
		t.Errorf("config rootfs diff_ids = %v, want [%s]", cfg.RootFS.DiffIDs, bArm.UncompressedSHA256)
	}
}

func TestPushRebuildTag(t *testing.T) {
	host := newTestRegistry(t)
	b := makeBottle(t, darwinArm(), 1)

	pub, err := NewPublisher(host, "acme/tap", "mytool", "t", PublishOptions{})
	if err != nil {
		t.Fatal(err)
	}
	results, err := pub.Push(context.Background(), []*bottle.Bottle{b}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Tag != "1.2.3-1" {
		t.Errorf("rebuild tag = %s, want 1.2.3-1", results[0].Tag)
	}

	// Single bottle must still be an index.
	ref, _ := name.ParseReference(results[0].Ref)
	idx, err := remote.Index(ref)
	if err != nil {
		t.Fatalf("single-platform push is not an index: %v", err)
	}
	m, _ := idx.IndexManifest()
	if len(m.Manifests) != 1 {
		t.Fatalf("expected 1 child, got %d", len(m.Manifests))
	}
	if got := m.Manifests[0].Annotations["org.opencontainers.image.ref.name"]; got != "1.2.3.arm64_sonoma.1" {
		t.Errorf("ref.name = %s, want 1.2.3.arm64_sonoma.1", got)
	}
}

func TestPushKeepOld(t *testing.T) {
	host := newTestRegistry(t)
	bArm := makeBottle(t, darwinArm(), 0)
	bLinux := makeBottle(t, linuxAmd(), 0)

	pub, err := NewPublisher(host, "acme/tap", "mytool", "t", PublishOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// First publish only darwin, then only linux at the same version.
	if _, err := pub.Push(context.Background(), []*bottle.Bottle{bArm}, nil); err != nil {
		t.Fatal(err)
	}
	results, err := pub.Push(context.Background(), []*bottle.Bottle{bLinux}, nil)
	if err != nil {
		t.Fatal(err)
	}

	ref, _ := name.ParseReference(results[0].Ref)
	idx, err := remote.Index(ref)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := idx.IndexManifest()
	if len(m.Manifests) != 2 {
		t.Fatalf("keep-old failed: expected 2 children after second push, got %d", len(m.Manifests))
	}
}

func TestImageFormulaName(t *testing.T) {
	for in, want := range map[string]string{
		"mytool":  "mytool",
		"go@1.22": "go/1.22",
		"libc++":  "libcxx",
		"a@b+c":   "a/bxc",
	} {
		if got := ImageFormulaName(in); got != want {
			t.Errorf("ImageFormulaName(%q) = %q, want %q", in, got, want)
		}
	}
}

func readFile(p string) ([]byte, error) {
	return os.ReadFile(p)
}

// makeTestArtifact writes a minimal artifact tar.gz with a "mytool" binary.
func makeTestArtifact(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "artifact.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)
	content := []byte("#!/bin/sh\necho mytool\n")
	if err := tw.WriteHeader(&tar.Header{
		Name: "mytool", Mode: 0755, Size: int64(len(content)),
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
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}
