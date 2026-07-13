package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LocalSource reads artifacts from a local directory (typically dist/)
type LocalSource struct {
	distPath   string
	checksums  map[string]string
	artifacts  []Artifact
	binaries   map[string][]resolvedBinary // artifact name -> raw binaries (artifacts.json mode)
	discovered bool
}

// resolvedBinary is one goreleaser-built raw binary on disk.
type resolvedBinary struct {
	name string // final binary name (extra.Binary)
	path string // absolute path to the built binary
}

// distArtifact is the subset of goreleaser's dist/artifacts.json we consume.
type distArtifact struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Goos   string `json:"goos"`
	Goarch string `json:"goarch"`
	Type   string `json:"type"`
	Extra  struct {
		Binary string `json:"Binary"`
	} `json:"extra"`
}

// NewLocalSource creates a new local artifact source
func NewLocalSource(distPath string) (*LocalSource, error) {
	// Verify the directory exists
	info, err := os.Stat(distPath)
	if err != nil {
		return nil, fmt.Errorf("failed to access dist directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", distPath)
	}

	// Make path absolute
	absPath, err := filepath.Abs(distPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	return &LocalSource{
		distPath:  absPath,
		checksums: make(map[string]string),
	}, nil
}

// List returns all available artifacts from the local directory.
// When goreleaser's dist/artifacts.json is present, the raw built binaries
// it describes are preferred over archives: no extraction round-trip, and
// the bottled bytes are exactly the (attestable) build outputs. Archives
// remain the fallback for hand-rolled dist directories. Note raw binaries
// are not covered by goreleaser's checksum manifest (archives only) —
// within-job filesystem trust applies.
func (s *LocalSource) List(ctx context.Context) ([]Artifact, error) {
	if s.discovered {
		return s.artifacts, nil
	}

	if artifacts, ok := s.listBinaries(); ok {
		s.artifacts = artifacts
		s.discovered = true
		return artifacts, nil
	}

	// Load checksums first
	if err := s.loadChecksums(); err != nil {
		// Non-fatal: checksums are optional
		// Could log here if we had a logger
	}

	// Read directory
	entries, err := os.ReadDir(s.distPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read dist directory: %w", err)
	}

	var artifacts []Artifact

	for _, entry := range entries {
		// Skip directories and checksum files
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Skip checksum files themselves
		if strings.HasSuffix(name, ".sha256") ||
			strings.HasSuffix(name, ".sha256sum") ||
			name == "checksums.txt" ||
			name == "SHA256SUMS" {
			continue
		}

		// Only process archives
		if !IsArchive(name) {
			continue
		}

		// Try to parse OS and Arch from filename
		os, arch, ok := ParseArtifactName(name)
		if !ok {
			// Skip files we can't parse
			continue
		}

		// Build full path
		fullPath := filepath.Join(s.distPath, name)

		// Get checksum
		checksum, err := s.getChecksum(name, fullPath)
		if err != nil {
			// Non-fatal: continue without checksum
			checksum = ""
		}

		artifacts = append(artifacts, Artifact{
			Name:     name,
			Path:     fullPath,
			OS:       os,
			Arch:     arch,
			Checksum: checksum,
		})
	}

	s.artifacts = artifacts
	s.discovered = true

	return artifacts, nil
}

// listBinaries reads goreleaser's artifacts.json and groups its Binary
// entries into one artifact per platform. Returns ok=false when the
// manifest is absent, unreadable, or lists no binaries.
func (s *LocalSource) listBinaries() ([]Artifact, bool) {
	data, err := os.ReadFile(filepath.Join(s.distPath, "artifacts.json"))
	if err != nil {
		return nil, false
	}
	var entries []distArtifact
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, false
	}

	grouped := make(map[string][]resolvedBinary)
	var order []string
	for _, e := range entries {
		if e.Type != "Binary" || e.Goos == "" || e.Goarch == "" {
			continue
		}
		path, err := s.resolveDistPath(e.Path)
		if err != nil {
			continue
		}
		name := e.Extra.Binary
		if name == "" {
			name = e.Name
		}
		key := e.Goos + "_" + e.Goarch
		if _, seen := grouped[key]; !seen {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], resolvedBinary{name: name, path: path})
	}
	if len(grouped) == 0 {
		return nil, false
	}

	sort.Strings(order)
	s.binaries = make(map[string][]resolvedBinary, len(grouped))
	artifacts := make([]Artifact, 0, len(grouped))
	for _, key := range order {
		osName, arch, _ := strings.Cut(key, "_")
		name := "binaries_" + key
		s.binaries[name] = grouped[key]
		artifacts = append(artifacts, Artifact{
			Name: name,
			OS:   osName,
			Arch: arch,
		})
	}
	return artifacts, true
}

// resolveDistPath maps an artifacts.json path (relative to goreleaser's
// working directory, e.g. "dist/mytool_linux_amd64_v1/mytool") onto the
// configured dist directory, which may have been renamed or copied.
func (s *LocalSource) resolveDistPath(p string) (string, error) {
	// As written: relative to the dist dir's parent.
	candidate := filepath.Join(filepath.Dir(s.distPath), filepath.FromSlash(p))
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	// Otherwise strip the leading component (the original dist dir name)
	// and anchor the remainder at the configured dist path.
	if _, rest, found := strings.Cut(p, "/"); found {
		candidate = filepath.Join(s.distPath, filepath.FromSlash(rest))
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("artifact path %s not found under %s", p, s.distPath)
}

// Fetch copies an artifact to the target directory. Archive artifacts are
// copied as a single file; binary artifacts (artifacts.json mode) are
// staged as a DIRECTORY of correctly-named binaries, and the directory
// path is returned.
func (s *LocalSource) Fetch(ctx context.Context, artifact Artifact, targetDir string) (string, error) {
	// Ensure target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create target directory: %w", err)
	}

	if bins, ok := s.binaries[artifact.Name]; ok {
		dir := filepath.Join(targetDir, artifact.Name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create binaries directory: %w", err)
		}
		for _, b := range bins {
			if err := copyFile(b.path, filepath.Join(dir, b.name), 0755); err != nil {
				return "", fmt.Errorf("failed to stage binary %s: %w", b.name, err)
			}
		}
		return dir, nil
	}

	// Source path
	srcPath := artifact.Path
	if !filepath.IsAbs(srcPath) {
		srcPath = filepath.Join(s.distPath, artifact.Name)
	}

	// Target path
	targetPath := filepath.Join(targetDir, artifact.Name)

	// If source and target are the same, no need to copy
	if srcPath == targetPath {
		return targetPath, nil
	}

	// Open source file
	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer src.Close()

	// Create target file
	dst, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to create target file: %w", err)
	}
	defer dst.Close()

	// Copy contents
	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	// Verify checksum if available
	if artifact.Checksum != "" {
		actualChecksum, err := computeFileSHA256(targetPath)
		if err != nil {
			return "", fmt.Errorf("failed to compute checksum: %w", err)
		}
		if err := ValidateChecksum(actualChecksum, artifact.Checksum); err != nil {
			// Clean up the copied file
			os.Remove(targetPath)
			return "", err
		}
	}

	return targetPath, nil
}

// Close releases any resources (no-op for local source)
func (s *LocalSource) Close() error {
	return nil
}

// loadChecksums attempts to load checksums from various sources
func (s *LocalSource) loadChecksums() error {
	// Try checksums.txt first (GoReleaser default)
	checksumFiles := []string{
		"checksums.txt",
		"SHA256SUMS",
		"checksums.sha256",
	}

	for _, filename := range checksumFiles {
		path := filepath.Join(s.distPath, filename)
		if content, err := os.ReadFile(path); err == nil {
			s.checksums = ParseChecksumsFile(string(content))
			return nil
		}
	}

	entries, err := os.ReadDir(s.distPath)
	if err != nil {
		return err
	}

	// Versioned manifests next: goreleaser's conventional checksum template
	// is "<name>_<version>_SHA256SUMS" (os.ReadDir returns sorted entries,
	// so the pick is deterministic).
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		lower := strings.ToLower(entry.Name())
		if strings.HasSuffix(lower, "sha256sums") ||
			(strings.HasPrefix(lower, "checksums") && strings.HasSuffix(lower, ".txt")) {
			if content, err := os.ReadFile(filepath.Join(s.distPath, entry.Name())); err == nil {
				s.checksums = ParseChecksumsFile(string(content))
				return nil
			}
		}
	}

	// If no checksums file found, try individual .sha256 files

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, ".sha256") {
			// Read the checksum file
			checksumPath := filepath.Join(s.distPath, name)
			content, err := os.ReadFile(checksumPath)
			if err != nil {
				continue
			}

			// The filename is the .sha256 file without the extension
			artifactName := strings.TrimSuffix(name, ".sha256")
			checksum := strings.TrimSpace(string(content))
			// Handle format: "checksum  filename" or just "checksum"
			parts := strings.Fields(checksum)
			if len(parts) > 0 {
				s.checksums[artifactName] = parts[0]
			}
		}
	}

	return nil
}

// getChecksum retrieves the checksum for a file
func (s *LocalSource) getChecksum(filename, fullPath string) (string, error) {
	// First check if we have it from checksums file
	if checksum, ok := s.checksums[filename]; ok {
		return checksum, nil
	}

	// Try to read a .sha256 file
	checksumPath := fullPath + ".sha256"
	if content, err := os.ReadFile(checksumPath); err == nil {
		checksum := strings.TrimSpace(string(content))
		// Handle format: "checksum  filename" or just "checksum"
		parts := strings.Fields(checksum)
		if len(parts) > 0 {
			return parts[0], nil
		}
	}

	// Compute checksum from the file itself
	return computeFileSHA256(fullPath)
}

// copyFile copies src to dst with the given mode.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// computeFileSHA256 calculates the SHA256 checksum of a file
func computeFileSHA256(filepath string) (string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
