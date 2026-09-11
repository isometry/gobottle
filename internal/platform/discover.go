package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// HomebrewVersionURL is the URL to Homebrew's macOS version definitions
	HomebrewVersionURL = "https://raw.githubusercontent.com/Homebrew/brew/HEAD/Library/Homebrew/macos_version.rb"

	// MinSupportedMacOSMajor is the minimum macOS version we generate bottles for
	// Apple supports ~3 years of macOS versions
	MinSupportedMacOSMajor = 12 // Monterey
)

var (
	commentRegex         = regexp.MustCompile(`(?m)#.*$`)
	symbolsLiteralRegex  = regexp.MustCompile(`SYMBOLS\s*=\s*(?:T\.let\()?\s*\{([^}]+)\}`)
	symbolsExceptRegex   = regexp.MustCompile(`SYMBOLS\s*=\s*(?:T\.let\()?\s*RELEASES\.except\(([^)]*)\)`)
	releasesLiteralRegex = regexp.MustCompile(`RELEASES\s*=\s*(?:T\.let\()?\s*\{([^}]+)\}`)
	symbolRegex          = regexp.MustCompile(`:(\w+)`)
	entryRegex           = regexp.MustCompile(`(\w+):\s*"([\d.]+)"`)
)

// Discoverer fetches current platform information from Homebrew's source
type Discoverer struct {
	httpClient *http.Client
	cache      *Cache
}

// NewDiscoverer creates a new platform discoverer
func NewDiscoverer(cacheDir string) (*Discoverer, error) {
	cache, err := NewCache(cacheDir)
	if err != nil {
		return nil, err
	}

	return &Discoverer{
		httpClient: &http.Client{},
		cache:      cache,
	}, nil
}

// Discover fetches current supported platforms from Homebrew
// If forceRefresh is false, returns cached data if available and not expired
func (d *Discoverer) Discover(ctx context.Context, forceRefresh bool) (*PlatformInfo, error) {
	// Check cache first
	if !forceRefresh {
		if cached, err := d.cache.Load(); err == nil {
			return cached, nil
		}
	}

	// Fetch from Homebrew
	info, err := d.fetchFromHomebrew(ctx)
	if err != nil {
		// If fetch fails, try to use cached data even if expired
		if cached, cacheErr := d.cache.LoadExpired(); cacheErr == nil {
			return cached, nil
		}
		return nil, fmt.Errorf("failed to fetch platform info: %w", err)
	}

	// Save to cache
	if err := d.cache.Save(info); err != nil {
		// Log but don't fail
		fmt.Printf("Warning: failed to cache platform info: %v\n", err)
	}

	return info, nil
}

// GetPlatformsForArtifact returns all Homebrew platforms compatible with a GOOS/GOARCH
func (d *Discoverer) GetPlatformsForArtifact(ctx context.Context, goos, goarch string, forceRefresh bool) ([]Platform, error) {
	info, err := d.Discover(ctx, forceRefresh)
	if err != nil {
		return nil, err
	}

	return info.GetPlatformsForOS(goos, goarch), nil
}

// fetchFromHomebrew fetches and parses the macOS version definitions
func (d *Discoverer) fetchFromHomebrew(ctx context.Context) (*PlatformInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, HomebrewVersionURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch version.rb: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return parseVersionRb(string(body))
}

// parseVersionRb parses Homebrew's macos_version.rb to extract the macOS
// versions brew currently supports. Two layouts have existed:
//
//	SYMBOLS = T.let({ tahoe: "26", sequoia: "15", … }.freeze, …)      (literal)
//	RELEASES = T.let({ golden_gate: "27", …, el_capitan: "10.11" }.freeze, …)
//	SYMBOLS = T.let(RELEASES.except(:catalina, :mojave, …).freeze, …)  (current)
//
// In the current layout RELEASES keeps every named release for labelling
// old data, and SYMBOLS is the supported subset.
func parseVersionRb(content string) (*PlatformInfo, error) {
	// Ruby comments inside the hashes carry prose; drop them before matching.
	content = commentRegex.ReplaceAllString(content, "")

	var symbolsContent string
	excluded := map[string]bool{}
	if m := symbolsLiteralRegex.FindStringSubmatch(content); m != nil {
		symbolsContent = m[1]
	} else if m := symbolsExceptRegex.FindStringSubmatch(content); m != nil {
		for _, sym := range symbolRegex.FindAllStringSubmatch(m[1], -1) {
			excluded[sym[1]] = true
		}
		r := releasesLiteralRegex.FindStringSubmatch(content)
		if r == nil {
			return nil, fmt.Errorf("SYMBOLS derives from RELEASES but no RELEASES hash was found in macos_version.rb")
		}
		symbolsContent = r[1]
	} else {
		return nil, fmt.Errorf("could not find SYMBOLS hash in macos_version.rb")
	}

	// Parse individual entries: "symbol: \"version\"" (handles both integer and decimal versions)
	entries := entryRegex.FindAllStringSubmatch(symbolsContent, -1)

	var versions []MacOSVersion
	for _, entry := range entries {
		symbol := entry[1]
		versionStr := entry[2]
		if excluded[symbol] {
			continue
		}

		// Parse version - could be "15" or "10.15"
		var major int
		if strings.Contains(versionStr, ".") {
			// Format like "10.15" - take the minor as the identifier
			parts := strings.Split(versionStr, ".")
			if len(parts) >= 2 {
				major, _ = strconv.Atoi(parts[1])
				// For 10.x versions, use negative to sort properly
				major = -major
			}
		} else {
			major, _ = strconv.Atoi(versionStr)
		}

		// Only include currently supported versions (macOS 12+)
		if major >= MinSupportedMacOSMajor {
			versions = append(versions, MacOSVersion{
				Major:  major,
				Symbol: symbol,
			})
		}
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("no supported macOS versions found in macos_version.rb")
	}

	// Sort by major version descending (newest first)
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Major > versions[j].Major
	})

	return &PlatformInfo{
		MacOSVersions: versions,
		LinuxArches:   []string{"x86_64", "arm64"},
	}, nil
}

// DefaultPlatformInfo returns a fallback platform info if discovery fails.
// Only includes macOS versions currently supported by Apple/Homebrew (past 3 releases).
func DefaultPlatformInfo() *PlatformInfo {
	return &PlatformInfo{
		MacOSVersions: []MacOSVersion{
			{Major: 15, Symbol: "sequoia"},
			{Major: 14, Symbol: "sonoma"},
		},
		LinuxArches: []string{"x86_64", "arm64"},
	}
}

// FormatPlatformList returns a human-readable list of platforms
func FormatPlatformList(info *PlatformInfo) string {
	var sb strings.Builder

	sb.WriteString("macOS (Apple Silicon - arm64):\n")
	for _, v := range info.MacOSVersions {
		sb.WriteString(fmt.Sprintf("  - arm64_%s (macOS %d)\n", v.Symbol, v.Major))
	}

	sb.WriteString("\nmacOS (Intel - amd64):\n")
	for _, v := range info.MacOSVersions {
		sb.WriteString(fmt.Sprintf("  - %s (macOS %d)\n", v.Symbol, v.Major))
	}

	sb.WriteString("\nLinux:\n")
	for _, arch := range info.LinuxArches {
		goarch := homebrewArchToGoArch(arch)
		sb.WriteString(fmt.Sprintf("  - %s_linux (%s)\n", arch, goarch))
	}

	return sb.String()
}
