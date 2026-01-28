package bottle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isometry/gobottle/internal/platform"
	"github.com/isometry/gobottle/internal/util"
)

// Builder transforms GoReleaser artifacts into Homebrew bottles
type Builder struct {
	workDir string
}

// BuildOptions contains the parameters for building a bottle
type BuildOptions struct {
	Formula      string
	Version      string
	Platform     platform.Platform
	ArtifactPath string   // Path to GoReleaser archive
	Binaries     []string // Binary names to include
	Cellar       string
	Rebuild      int
}

// NewBuilder creates a new bottle builder with a temp work directory
func NewBuilder() (*Builder, error) {
	workDir, err := os.MkdirTemp("", "gobottle-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
	}

	return &Builder{
		workDir: workDir,
	}, nil
}

// Build creates a bottle from a GoReleaser artifact
// The artifact should be the path to a tar.gz containing binaries
func (b *Builder) Build(ctx context.Context, opts BuildOptions) (*Bottle, error) {
	// Step 1: Extract GoReleaser archive to temp dir
	extractDir := filepath.Join(b.workDir, "extract")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create extract directory: %w", err)
	}

	if err := util.ExtractTarGz(opts.ArtifactPath, extractDir); err != nil {
		return nil, fmt.Errorf("failed to extract artifact: %w", err)
	}

	// Step 2: Create Homebrew structure
	bottleDir := filepath.Join(b.workDir, "bottle")
	homebrewPrefix := filepath.Join(bottleDir, opts.Formula, opts.Version)
	binDir := filepath.Join(homebrewPrefix, "bin")

	if err := os.MkdirAll(binDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Copy binaries to the bottle structure
	files := make(map[string]string)
	for _, binary := range opts.Binaries {
		srcPath := filepath.Join(extractDir, binary)

		// Check if the binary exists
		if _, err := os.Stat(srcPath); err != nil {
			return nil, fmt.Errorf("binary %s not found in artifact: %w", binary, err)
		}

		// Archive path in the bottle
		archivePath := filepath.Join(opts.Formula, opts.Version, "bin", binary)
		files[archivePath] = srcPath
	}

	// Step 3: Create .brew/formula.rb stub file
	brewDir := filepath.Join(homebrewPrefix, ".brew")
	if err := os.MkdirAll(brewDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .brew directory: %w", err)
	}

	stubPath := filepath.Join(brewDir, opts.Formula+".rb")
	stubContent := fmt.Sprintf("# Homebrew formula stub for %s\n", opts.Formula)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to create formula stub: %w", err)
	}

	// Add the stub to the files map
	stubArchivePath := filepath.Join(opts.Formula, opts.Version, ".brew", opts.Formula+".rb")
	files[stubArchivePath] = stubPath

	// Step 4: Create bottle tarball with proper naming
	bottleName := fmt.Sprintf("%s--%s.%s.bottle.tar.gz", opts.Formula, opts.Version, opts.Platform.Tag)
	bottlePath := filepath.Join(b.workDir, bottleName)

	if err := util.CreateTarGz(bottlePath, files); err != nil {
		return nil, fmt.Errorf("failed to create bottle tarball: %w", err)
	}

	// Step 5: Compute SHA256
	sha256sum, err := util.ComputeSHA256(bottlePath)
	if err != nil {
		return nil, fmt.Errorf("failed to compute SHA256: %w", err)
	}

	// Create and return the Bottle struct
	bottle := &Bottle{
		Formula:  opts.Formula,
		Version:  opts.Version,
		Platform: opts.Platform,
		SHA256:   sha256sum,
		Path:     bottlePath,
		Binaries: opts.Binaries,
		Cellar:   opts.Cellar,
		Rebuild:  opts.Rebuild,
	}

	return bottle, nil
}

// Close cleans up the work directory
func (b *Builder) Close() error {
	if b.workDir != "" {
		return os.RemoveAll(b.workDir)
	}
	return nil
}
