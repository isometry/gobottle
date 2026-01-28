package git

import (
	"regexp"
	"strings"
)

// semverRegex matches valid semantic version strings with optional 'v' prefix.
// Supports: v1.2.3, 1.2.3, v1.2.3-beta, v1.2.3-rc.1, v1.2.3+build.123
var semverRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// IsValidSemver checks if a version string is valid semver.
// Accepts versions with or without 'v' prefix.
func IsValidSemver(version string) bool {
	return semverRegex.MatchString(version)
}

// ParseVersion extracts version from a tag, stripping the leading 'v' if present.
func ParseVersion(tag string) string {
	return strings.TrimPrefix(tag, "v")
}

// NormalizeVersion ensures the version string has a 'v' prefix.
func NormalizeVersion(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
