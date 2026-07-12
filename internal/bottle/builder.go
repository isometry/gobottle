package bottle

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/isometry/gobottle/internal/platform"
	"github.com/isometry/gobottle/internal/util"
)

// Builder transforms build artifacts into Homebrew bottles
type Builder struct {
	workDir string
	seq     int
}

// BinaryInstall names a binary and the keg-relative directory it installs
// into (empty means "bin").
type BinaryInstall struct {
	Name        string
	InstallPath string
}

// BuildOptions contains the parameters for building a bottle
type BuildOptions struct {
	Formula      string
	Version      string
	Platform     platform.Platform
	ArtifactPath string          // Path to archive containing the binaries
	Binaries     []BinaryInstall // Binaries to include
	Cellar       string
	Rebuild      int
	Tap          string            // Tap name for INSTALL_RECEIPT.json (e.g., "user/homebrew-tap")
	FormulaRb    string            // Rendered formula source (sans bottle block) for .brew/<formula>.rb
	ExtraFiles   map[string]string // Keg-relative archive path -> local path (e.g. completions)
	SourceDate   time.Time
	OutputDir    string // Where to write the bottle (default: builder temp dir, removed on Close)
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

// Build creates a bottle from an artifact archive containing the binaries at
// its root. The output tarball is fully deterministic for identical inputs.
func (b *Builder) Build(ctx context.Context, opts BuildOptions) (*Bottle, error) {
	// Isolated staging area per build: extraction from different artifacts
	// must never collide.
	b.seq++
	stageDir := filepath.Join(b.workDir, fmt.Sprintf("build-%d", b.seq))
	extractDir := filepath.Join(stageDir, "extract")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create extract directory: %w", err)
	}

	if err := util.ExtractArchive(opts.ArtifactPath, extractDir); err != nil {
		return nil, fmt.Errorf("failed to extract artifact: %w", err)
	}

	// Resolve the reproducible timestamp: explicit option > SOURCE_DATE_EPOCH > epoch.
	sourceDate := opts.SourceDate
	if sourceDate.IsZero() {
		if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
			if secs, err := strconv.ParseInt(epoch, 10, 64); err == nil {
				sourceDate = time.Unix(secs, 0)
			}
		}
	}

	// Map of archive path -> local path, all rooted at <formula>/<version>/.
	keg := path.Join(opts.Formula, opts.Version)
	files := make(map[string]string)

	binaryNames := make([]string, 0, len(opts.Binaries))
	for _, binary := range opts.Binaries {
		srcPath := filepath.Join(extractDir, binary.Name)
		if _, err := os.Stat(srcPath); err != nil {
			return nil, fmt.Errorf("binary %s not found in artifact: %w", binary.Name, err)
		}
		installPath := binary.InstallPath
		if installPath == "" {
			installPath = "bin"
		}
		files[path.Join(keg, installPath, binary.Name)] = srcPath
		binaryNames = append(binaryNames, binary.Name)
	}

	for archivePath, localPath := range opts.ExtraFiles {
		files[path.Join(keg, archivePath)] = localPath
	}

	// .brew/<formula>.rb: the real formula source (without bottle block) so
	// Formulary can load the installed keg when the tap is unavailable.
	brewDir := filepath.Join(stageDir, ".brew")
	if err := os.MkdirAll(brewDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .brew directory: %w", err)
	}

	formulaPath := filepath.Join(brewDir, opts.Formula+".rb")
	if err := os.WriteFile(formulaPath, []byte(opts.FormulaRb), 0644); err != nil {
		return nil, fmt.Errorf("failed to write formula: %w", err)
	}
	files[path.Join(keg, ".brew", opts.Formula+".rb")] = formulaPath

	// .brew/INSTALL_RECEIPT.json (the "tab")
	tab := NewTab(TabOptions{
		Formula:   opts.Formula,
		Version:   opts.Version,
		Arch:      opts.Platform.Arch,
		OS:        opts.Platform.OS,
		OSVersion: opts.Platform.OSVersion,
		Tap:       opts.Tap,
		Compiler:  "go",
		Time:      sourceDate,
	})

	receiptContent, err := tab.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal INSTALL_RECEIPT.json: %w", err)
	}

	receiptPath := filepath.Join(brewDir, "INSTALL_RECEIPT.json")
	if err := os.WriteFile(receiptPath, receiptContent, 0644); err != nil {
		return nil, fmt.Errorf("failed to create INSTALL_RECEIPT.json: %w", err)
	}
	files[path.Join(keg, ".brew", "INSTALL_RECEIPT.json")] = receiptPath

	// Write the deterministic bottle tarball.
	outDir := opts.OutputDir
	if outDir == "" {
		outDir = b.workDir
	} else if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	bottleName := bottleFilename(opts.Formula, opts.Version, opts.Platform.Tag, opts.Rebuild)
	bottlePath := filepath.Join(outDir, bottleName)

	res, err := writeTarGz(bottlePath, files, sourceDate)
	if err != nil {
		return nil, fmt.Errorf("failed to create bottle tarball: %w", err)
	}

	return &Bottle{
		Formula:            opts.Formula,
		Version:            opts.Version,
		Platform:           opts.Platform,
		SHA256:             res.SHA256,
		UncompressedSHA256: res.UncompressedSHA256,
		UncompressedSize:   res.UncompressedSize,
		Path:               bottlePath,
		Binaries:           binaryNames,
		Cellar:             opts.Cellar,
		Rebuild:            opts.Rebuild,
		Tab:                tab,
	}, nil
}

// Close cleans up the work directory
func (b *Builder) Close() error {
	if b.workDir != "" {
		return os.RemoveAll(b.workDir)
	}
	return nil
}
