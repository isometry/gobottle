package platform

import (
	"fmt"
	"strings"
)

// Platform represents a Homebrew bottle platform tag
type Platform struct {
	Tag            string // e.g., "arm64_sonoma", "x86_64_linux"
	OS             string // "darwin" or "linux"
	Arch           string // "amd64" or "arm64"
	OSVersion      string // e.g., "sonoma", "ventura" (macOS codename), or empty for Linux
	OSVersionMajor int    // e.g., 14 for sonoma; 0 for Linux
}

func (p Platform) String() string {
	return p.Tag
}

// MacOSVersion represents a macOS version with its codename
type MacOSVersion struct {
	Major  int    // e.g., 15
	Symbol string // e.g., "sequoia"
}

func (v MacOSVersion) String() string {
	return fmt.Sprintf("%s (macOS %d)", v.Symbol, v.Major)
}

// PlatformInfo holds discovered platform information
type PlatformInfo struct {
	MacOSVersions []MacOSVersion // Supported macOS versions (newest first)
	LinuxArches   []string       // e.g., ["x86_64", "aarch64"]
}

// GetPlatformsForOS returns the Homebrew platform tag for a given OS/arch.
// For macOS, returns only the oldest supported version (e.g., monterey).
// Homebrew's fallback mechanism allows older bottles to install on newer macOS.
func (pi *PlatformInfo) GetPlatformsForOS(goos, goarch string) []Platform {
	switch goos {
	case "darwin":
		if len(pi.MacOSVersions) == 0 {
			return nil
		}
		// MacOSVersions is sorted newest-first, so last element is oldest
		oldest := pi.MacOSVersions[len(pi.MacOSVersions)-1]
		var tag string
		if goarch == "arm64" {
			tag = "arm64_" + oldest.Symbol
		} else {
			tag = oldest.Symbol
		}
		return []Platform{{
			Tag:            tag,
			OS:             goos,
			Arch:           goarch,
			OSVersion:      oldest.Symbol,
			OSVersionMajor: oldest.Major,
		}}
	case "linux":
		archTag := goArchToHomebrewArch(goarch)
		if archTag != "" {
			return []Platform{{
				Tag:  archTag + "_linux",
				OS:   goos,
				Arch: goarch,
			}}
		}
	}
	return nil
}

// AllPlatforms returns all possible platform tags
func (pi *PlatformInfo) AllPlatforms() []Platform {
	var platforms []Platform

	// macOS ARM64
	platforms = append(platforms, pi.GetPlatformsForOS("darwin", "arm64")...)

	// macOS Intel
	platforms = append(platforms, pi.GetPlatformsForOS("darwin", "amd64")...)

	// Linux
	for _, arch := range pi.LinuxArches {
		goarch := homebrewArchToGoArch(arch)
		if goarch != "" {
			platforms = append(platforms, Platform{
				Tag:  arch + "_linux",
				OS:   "linux",
				Arch: goarch,
			})
		}
	}

	return platforms
}

// goArchToHomebrewArch converts Go arch to Homebrew arch
func goArchToHomebrewArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return ""
	}
}

// homebrewArchToGoArch converts Homebrew arch to Go arch
func homebrewArchToGoArch(arch string) string {
	switch arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return ""
	}
}

// ParsePlatformTag parses a Homebrew platform tag into OS, arch, and version
func ParsePlatformTag(tag string) (os, arch, version string) {
	if strings.HasSuffix(tag, "_linux") {
		os = "linux"
		archPart := strings.TrimSuffix(tag, "_linux")
		arch = homebrewArchToGoArch(archPart)
		return
	}

	// macOS
	os = "darwin"
	if strings.HasPrefix(tag, "arm64_") {
		arch = "arm64"
		version = strings.TrimPrefix(tag, "arm64_")
	} else {
		arch = "amd64"
		version = tag
	}
	return
}

// FilterPlatforms filters platforms based on include/exclude lists
func FilterPlatforms(platforms []Platform, include, exclude []string) []Platform {
	// Build lookup maps
	includeMap := make(map[string]bool)
	for _, p := range include {
		includeMap[p] = true
	}
	excludeMap := make(map[string]bool)
	for _, p := range exclude {
		excludeMap[p] = true
	}

	var result []Platform
	for _, p := range platforms {
		// Skip if excluded
		if excludeMap[p.Tag] {
			continue
		}
		// If include list is specified, only include those
		if len(include) > 0 && !includeMap[p.Tag] {
			continue
		}
		result = append(result, p)
	}

	return result
}
