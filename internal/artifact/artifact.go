package artifact

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Artifact represents a GoReleaser release artifact
type Artifact struct {
	Name     string // e.g., "myapp_1.0.0_darwin_arm64.tar.gz"
	Path     string // local path to the file
	OS       string // "darwin", "linux"
	Arch     string // "amd64", "arm64"
	Checksum string // SHA256
}

// Source is the interface for artifact sources
type Source interface {
	// List returns all available artifacts
	List(ctx context.Context) ([]Artifact, error)
	// Fetch downloads/copies an artifact to the target directory
	Fetch(ctx context.Context, artifact Artifact, targetDir string) (string, error)
	// Close releases any resources
	Close() error
}

// ParseArtifactName extracts OS and Arch from a GoReleaser artifact filename
// Expected formats:
//   - {name}_{version}_{os}_{arch}.tar.gz
//   - {name}_{version}_{os}_{arch}.zip
//   - Handles various archive extensions
func ParseArtifactName(filename string) (os, arch string, ok bool) {
	// Remove extension(s) to get the base name
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	// Handle double extensions like .tar.gz
	if before, ok0 := strings.CutSuffix(base, ".tar"); ok0 {
		base = before
	}

	// Try common GoReleaser pattern: name_version_os_arch
	// Split by underscore and look for OS and arch patterns
	parts := strings.Split(base, "_")
	if len(parts) < 2 {
		return "", "", false
	}

	// Scan from the end for arch and os
	// Common patterns: darwin_amd64, linux_arm64, etc.
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]

		// Check if this looks like an architecture
		if arch == "" {
			if a, ok := normalizeArch(part); ok {
				arch = a
				continue
			}
		}

		// Check if this looks like an OS (and we already have arch)
		if arch != "" && os == "" {
			if o, ok := normalizeOS(part); ok {
				os = o
				return os, arch, true
			}
		}
	}

	return "", "", false
}

// normalizeOS converts various OS naming conventions to standard Go GOOS values
func normalizeOS(s string) (string, bool) {
	s = strings.ToLower(s)
	switch s {
	case "darwin", "macos", "osx":
		return "darwin", true
	case "linux":
		return "linux", true
	case "windows", "win":
		return "windows", true
	case "freebsd":
		return "freebsd", true
	default:
		return "", false
	}
}

// normalizeArch converts various architecture naming conventions to standard Go GOARCH values
func normalizeArch(s string) (string, bool) {
	s = strings.ToLower(s)
	switch s {
	case "amd64", "x86_64", "x64":
		return "amd64", true
	case "arm64", "aarch64":
		return "arm64", true
	case "386", "i386", "x86":
		return "386", true
	case "arm":
		return "arm", true
	default:
		return "", false
	}
}

// ParseChecksumsFile parses a checksums.txt file (SHA256SUMS format)
// Format: <checksum>  <filename>
func ParseChecksumsFile(content string) map[string]string {
	checksums := make(map[string]string)

	// Regular expression to match checksum lines
	// Supports both single and double spaces between checksum and filename
	re := regexp.MustCompile(`(?m)^([a-fA-F0-9]{64})\s+(.+)$`)

	matches := re.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) == 3 {
			checksum := strings.ToLower(match[1])
			filename := strings.TrimSpace(match[2])
			// Remove leading ./ or * from filename
			filename = strings.TrimPrefix(filename, "./")
			filename = strings.TrimPrefix(filename, "*")
			checksums[filename] = checksum
		}
	}

	return checksums
}

// ValidateChecksum verifies that a file's checksum matches the expected value
func ValidateChecksum(actual, expected string) error {
	actual = strings.ToLower(strings.TrimSpace(actual))
	expected = strings.ToLower(strings.TrimSpace(expected))

	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}

	return nil
}

// FilterArtifacts filters artifacts by OS and/or Arch
func FilterArtifacts(artifacts []Artifact, os, arch string) []Artifact {
	var filtered []Artifact

	for _, a := range artifacts {
		if os != "" && a.OS != os {
			continue
		}
		if arch != "" && a.Arch != arch {
			continue
		}
		filtered = append(filtered, a)
	}

	return filtered
}

// IsArchive checks if a filename appears to be an archive. The accepted
// set must stay in sync with util.ExtractArchive: accepting a format we
// cannot extract turns "no artifacts found" into a cryptic decode error.
func IsArchive(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tgz") ||
		strings.HasSuffix(lower, ".tar.bz2") ||
		strings.HasSuffix(lower, ".zip")
}
