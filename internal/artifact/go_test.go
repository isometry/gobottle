package artifact

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGoSourceConfig_Defaults(t *testing.T) {
	// Create a temp directory with a go.mod
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := GoSourceConfig{
		Packages: []string{"./cmd/app"},
		ModDir:   tmpDir,
	}

	source, err := NewGoSource(cfg)
	if err != nil {
		t.Fatalf("NewGoSource failed: %v", err)
	}
	defer source.Close()

	// Check defaults were applied
	if source.config.Parallel <= 0 {
		t.Error("expected Parallel to be set")
	}
	if source.config.ModDir != tmpDir {
		t.Errorf("expected ModDir=%q, got %q", tmpDir, source.config.ModDir)
	}
}

func TestGoSourceConfig_NoGoMod(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := GoSourceConfig{
		Packages: []string{"./cmd/app"},
		ModDir:   tmpDir,
	}

	_, err := NewGoSource(cfg)
	if err == nil {
		t.Error("expected error when go.mod is missing")
	}
}

func TestGoSource_RenderLdflags(t *testing.T) {
	// Create a temp directory with a go.mod
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		ldflags  string
		version  string
		commit   string
		tag      string
		date     time.Time
		expected string
	}{
		{
			name:     "empty ldflags defaults to strip flags",
			ldflags:  "",
			expected: "-s -w",
		},
		{
			name:     "version only",
			ldflags:  "-X main.version={{.Version}}",
			version:  "1.0.0",
			expected: "-X main.version=1.0.0",
		},
		{
			name:     "all variables",
			ldflags:  "-X main.version={{.Version}} -X main.commit={{.Commit}} -X main.tag={{.Tag}}",
			version:  "1.0.0",
			commit:   "abc123",
			tag:      "v1.0.0",
			expected: "-X main.version=1.0.0 -X main.commit=abc123 -X main.tag=v1.0.0",
		},
		{
			name:     "strip and optimize",
			ldflags:  "-s -w -X main.version={{.Version}}",
			version:  "2.0.0",
			expected: "-s -w -X main.version=2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GoSourceConfig{
				Packages: []string{"./cmd/app"},
				ModDir:   tmpDir,
				Ldflags:  tt.ldflags,
				Version:  tt.version,
				Commit:   tt.commit,
				Tag:      tt.tag,
				Date:     tt.date,
			}

			source, err := NewGoSource(cfg)
			if err != nil {
				t.Fatalf("NewGoSource failed: %v", err)
			}
			defer source.Close()

			result, err := source.renderLdflags()
			if err != nil {
				t.Fatalf("renderLdflags failed: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGoSource_InvalidLdflagsTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := GoSourceConfig{
		Packages: []string{"./cmd/app"},
		ModDir:   tmpDir,
		Ldflags:  "-X main.version={{.InvalidField}}",
	}

	source, err := NewGoSource(cfg)
	if err != nil {
		t.Fatalf("NewGoSource failed: %v", err)
	}
	defer source.Close()

	_, err = source.renderLdflags()
	if err == nil {
		t.Error("expected error for invalid template field")
	}
}

func TestGoSource_AllBuildTargets(t *testing.T) {
	targets := AllBuildTargets()

	if len(targets) != 4 {
		t.Errorf("expected 4 build targets, got %d", len(targets))
	}

	expectedTargets := map[string]bool{
		"darwin/arm64": false,
		"darwin/amd64": false,
		"linux/amd64":  false,
		"linux/arm64":  false,
	}

	for _, target := range targets {
		key := target.OS + "/" + target.Arch
		if _, ok := expectedTargets[key]; !ok {
			t.Errorf("unexpected target: %s", key)
		}
		expectedTargets[key] = true
	}

	for key, found := range expectedTargets {
		if !found {
			t.Errorf("missing expected target: %s", key)
		}
	}
}

func TestGoSource_BinaryNames(t *testing.T) {
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		packages []string
		binaries []string
		expected []string
	}{
		{
			name:     "explicit binaries",
			packages: []string{"./cmd/app"},
			binaries: []string{"myapp"},
			expected: []string{"myapp"},
		},
		{
			name:     "derived from packages",
			packages: []string{"./cmd/server", "./cmd/client"},
			binaries: nil,
			expected: []string{"server", "client"},
		},
		{
			name:     "mixed",
			packages: []string{"./cmd/app1", "./cmd/app2"},
			binaries: []string{"custom"},
			expected: []string{"custom"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := GoSourceConfig{
				Packages: tt.packages,
				Binaries: tt.binaries,
				ModDir:   tmpDir,
			}

			source, err := NewGoSource(cfg)
			if err != nil {
				t.Fatalf("NewGoSource failed: %v", err)
			}
			defer source.Close()

			result := source.BinaryNames()

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d binaries, got %d", len(tt.expected), len(result))
				return
			}

			for i, exp := range tt.expected {
				if result[i] != exp {
					t.Errorf("expected binary[%d]=%q, got %q", i, exp, result[i])
				}
			}
		})
	}
}

func TestGoSource_SourceDateEpoch(t *testing.T) {
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Set SOURCE_DATE_EPOCH
	originalEpoch := os.Getenv("SOURCE_DATE_EPOCH")
	os.Setenv("SOURCE_DATE_EPOCH", "1609459200") // 2021-01-01 00:00:00 UTC
	defer os.Setenv("SOURCE_DATE_EPOCH", originalEpoch)

	cfg := GoSourceConfig{
		Packages: []string{"./cmd/app"},
		ModDir:   tmpDir,
	}

	source, err := NewGoSource(cfg)
	if err != nil {
		t.Fatalf("NewGoSource failed: %v", err)
	}
	defer source.Close()

	expected := time.Unix(1609459200, 0).UTC()
	if !source.config.Date.Equal(expected) {
		t.Errorf("expected date %v, got %v", expected, source.config.Date)
	}
}

func TestGoSource_CreateTarball(t *testing.T) {
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := GoSourceConfig{
		Packages: []string{"./cmd/app"},
		ModDir:   tmpDir,
		Date:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	source, err := NewGoSource(cfg)
	if err != nil {
		t.Fatalf("NewGoSource failed: %v", err)
	}
	defer source.Close()

	// Create a test binary
	sourceDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}

	binaryPath := filepath.Join(sourceDir, "testbin")
	binaryContent := []byte("#!/bin/sh\necho hello\n")
	if err := os.WriteFile(binaryPath, binaryContent, 0755); err != nil {
		t.Fatal(err)
	}

	// Create tarball
	tarballPath := filepath.Join(tmpDir, "test.tar.gz")
	if err := source.createTarball(tarballPath, sourceDir, []string{"testbin"}); err != nil {
		t.Fatalf("createTarball failed: %v", err)
	}

	// Verify tarball contents
	f, err := os.Open(tarballPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	header, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}

	// Check the binary is at root level
	if header.Name != "testbin" {
		t.Errorf("expected binary at root level, got %q", header.Name)
	}

	// Check the timestamp for reproducibility
	if !header.ModTime.Equal(cfg.Date) {
		t.Errorf("expected modtime %v, got %v", cfg.Date, header.ModTime)
	}

	// Verify content
	content, err := io.ReadAll(tr)
	if err != nil {
		t.Fatal(err)
	}

	if string(content) != string(binaryContent) {
		t.Errorf("content mismatch: expected %q, got %q", string(binaryContent), string(content))
	}
}

func TestGoSource_Close(t *testing.T) {
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := GoSourceConfig{
		Packages: []string{"./cmd/app"},
		ModDir:   tmpDir,
	}

	source, err := NewGoSource(cfg)
	if err != nil {
		t.Fatalf("NewGoSource failed: %v", err)
	}

	workDir := source.workDir

	// Verify work directory exists
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		t.Error("work directory should exist before Close")
	}

	// Close should remove the work directory
	if err := source.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify work directory was removed
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Error("work directory should be removed after Close")
	}
}

// TestGoSource_ListIntegration is an integration test that actually builds Go code.
// This test is skipped in short mode.
func TestGoSource_ListIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a complete Go module with a main package
	tmpDir := t.TempDir()

	// Create go.mod
	goModContent := `module test

go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create cmd/app directory and main.go
	cmdDir := filepath.Join(tmpDir, "cmd", "app")
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		t.Fatal(err)
	}

	mainGo := `package main

import "fmt"

var version = "dev"

func main() {
	fmt.Println("Hello from version", version)
}
`
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := GoSourceConfig{
		Packages: []string{"./cmd/app"},
		ModDir:   tmpDir,
		Ldflags:  "-X main.version={{.Version}}",
		Version:  "1.0.0",
		Trimpath: true,
		Binaries: []string{"testapp"},
		Parallel: 2, // Limit parallelism for testing
	}

	source, err := NewGoSource(cfg)
	if err != nil {
		t.Fatalf("NewGoSource failed: %v", err)
	}
	defer source.Close()

	ctx := context.Background()
	artifacts, err := source.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Should have 4 artifacts (darwin/arm64, darwin/amd64, linux/amd64, linux/arm64)
	if len(artifacts) != 4 {
		t.Errorf("expected 4 artifacts, got %d", len(artifacts))
	}

	// Verify each artifact
	for _, art := range artifacts {
		// Check artifact has valid OS/Arch
		if art.OS != "darwin" && art.OS != "linux" {
			t.Errorf("unexpected OS: %s", art.OS)
		}
		if art.Arch != "amd64" && art.Arch != "arm64" {
			t.Errorf("unexpected Arch: %s", art.Arch)
		}

		// Check artifact file exists
		if _, err := os.Stat(art.Path); os.IsNotExist(err) {
			t.Errorf("artifact file does not exist: %s", art.Path)
		}

		// Check checksum is set
		if art.Checksum == "" {
			t.Error("artifact checksum should be set")
		}
	}
}

// TestGoSource_FetchIntegration tests the Fetch method.
func TestGoSource_FetchIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a complete Go module with a main package
	tmpDir := t.TempDir()

	// Create go.mod
	goModContent := `module test

go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create cmd/app directory and main.go
	cmdDir := filepath.Join(tmpDir, "cmd", "app")
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		t.Fatal(err)
	}

	mainGo := `package main

func main() {
}
`
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := GoSourceConfig{
		Packages: []string{"./cmd/app"},
		ModDir:   tmpDir,
		Binaries: []string{"testapp"},
		Parallel: 2,
	}

	source, err := NewGoSource(cfg)
	if err != nil {
		t.Fatalf("NewGoSource failed: %v", err)
	}
	defer source.Close()

	ctx := context.Background()
	artifacts, err := source.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(artifacts) == 0 {
		t.Fatal("no artifacts returned")
	}

	// Fetch the first artifact
	targetDir := t.TempDir()
	fetchedPath, err := source.Fetch(ctx, artifacts[0], targetDir)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	// Verify fetched file exists
	if _, err := os.Stat(fetchedPath); os.IsNotExist(err) {
		t.Error("fetched file does not exist")
	}
}
