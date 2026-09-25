package bottle

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	Links       []string // symlinked alias names, installed alongside Name
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

	// Directory artifacts (raw binaries staged by the source) are used
	// as-is; file artifacts are archives to extract.
	if info, err := os.Stat(opts.ArtifactPath); err == nil && info.IsDir() {
		extractDir = opts.ArtifactPath
	} else if err := util.ExtractArchive(opts.ArtifactPath, extractDir); err != nil {
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
	// links is a separate archive path -> linkname map: each entry becomes a
	// relative symlink in the bottle tarball rather than a copied file.
	keg := path.Join(opts.Formula, opts.Version)
	files := make(map[string]string)
	links := make(map[string]string)

	// declared collects every name Build itself accounts for at the artifact
	// root (binaries and their links), so the ignored-symlink scan below
	// knows what *not* to report.
	binaryNames := make([]string, 0, len(opts.Binaries))
	declared := make(map[string]bool, len(opts.Binaries))
	for _, binary := range opts.Binaries {
		resolved, err := ResolveBinary(extractDir, binary.Name)
		if err != nil {
			return nil, err
		}

		installPath := binary.InstallPath
		if installPath == "" {
			installPath = "bin"
		}
		files[path.Join(keg, installPath, binary.Name)] = resolved
		binaryNames = append(binaryNames, binary.Name)
		declared[binary.Name] = true

		for _, l := range binary.Links {
			links[path.Join(keg, installPath, l)] = binary.Name
			declared[l] = true
		}
	}

	// Symlinks the artifact ships at its root but that binaries[].links
	// doesn't declare (e.g. a goreleaser alias like kubectl-foo -> mytool)
	// are never bottled: the config is the single source of truth for the
	// bottle's layout. They're surfaced here so the caller can warn instead.
	ignoredSymlinks, err := findIgnoredSymlinks(extractDir, declared)
	if err != nil {
		return nil, err
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

	res, err := writeTarGz(bottlePath, files, links, sourceDate)
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
		IgnoredSymlinks:    ignoredSymlinks,
	}, nil
}

// findIgnoredSymlinks scans the top level of dir (not recursively: binaries
// are only ever looked up at the root) for symlink entries whose name isn't
// in declared, returning them sorted by name.
func findIgnoredSymlinks(dir string, declared map[string]bool) ([]IgnoredSymlink, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read artifact directory: %w", err)
	}

	var ignored []IgnoredSymlink
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink == 0 || declared[entry.Name()] {
			continue
		}
		target, err := os.Readlink(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read symlink %s: %w", entry.Name(), err)
		}
		ignored = append(ignored, IgnoredSymlink{Name: entry.Name(), Target: target})
	}
	sort.Slice(ignored, func(i, j int) bool { return ignored[i].Name < ignored[j].Name })
	return ignored, nil
}

// ResolveBinary resolves name within dir (an extracted artifact directory,
// or a directory artifact used as-is) to a regular file, dereferencing any
// symlink so callers always see the same content a poured bottle would
// contain. A configured binary may itself be a symlink inside the artifact
// (a release archive can ship a dereferenced copy under an unpredictable
// versioned name, e.g. mytool -> mytool_1.2.3, or an alias such as
// kubectl-mytool -> mytool). It is an error for name to be missing,
// dangling, to resolve outside dir, or to resolve to anything but a regular
// file.
func ResolveBinary(dir, name string) (string, error) {
	// Resolve dir itself before using it as the containment boundary below:
	// on macOS $TMPDIR sits under /var, a symlink to /private/var, so
	// comparing raw paths would reject every legitimate binary.
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve artifact directory: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(filepath.Join(dir, name))
	if err != nil {
		return "", fmt.Errorf("binary %s not found in artifact: %w", name, err)
	}

	// A resolved path that isn't strictly below resolvedDir either escapes
	// it (rel starts with "..") or *is* resolvedDir (rel == "."), which the
	// regular-file check below rejects anyway, so no separate equality case
	// is needed here.
	rel, err := filepath.Rel(resolvedDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("binary %s resolves outside the artifact: %s", name, resolved)
	}

	info, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("binary %s not found in artifact: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("binary %s is not a regular file (resolved to %s)", name, resolved)
	}

	return resolved, nil
}

// Close cleans up the work directory
func (b *Builder) Close() error {
	if b.workDir != "" {
		return os.RemoveAll(b.workDir)
	}
	return nil
}
