package artifact

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalSource reads artifacts from a local directory (typically dist/)
type LocalSource struct {
	distPath   string
	checksums  map[string]string
	artifacts  []Artifact
	discovered bool
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

// List returns all available artifacts from the local directory
func (s *LocalSource) List(ctx context.Context) ([]Artifact, error) {
	if s.discovered {
		return s.artifacts, nil
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

// Fetch copies an artifact to the target directory
// For local source, this is just a file copy
func (s *LocalSource) Fetch(ctx context.Context, artifact Artifact, targetDir string) (string, error) {
	// Ensure target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create target directory: %w", err)
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
