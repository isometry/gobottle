package git

import "strings"

// ParseRemoteURL extracts owner and repo from various git URL formats.
// Supports:
//   - SSH format: git@github.com:owner/repo.git
//   - HTTPS format: https://github.com/owner/repo.git
//   - HTTP format: http://github.com/owner/repo.git
func ParseRemoteURL(url string) (owner, repo string) {
	// Handle SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@") {
		parts := strings.Split(url, ":")
		if len(parts) == 2 {
			path := strings.TrimSuffix(parts[1], ".git")
			pathParts := strings.Split(path, "/")
			if len(pathParts) >= 2 {
				return pathParts[0], pathParts[1]
			}
		}
	}

	// Handle HTTPS/HTTP format: https://github.com/owner/repo.git
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimSuffix(url, ".git")

	parts := strings.Split(url, "/")
	if len(parts) >= 3 {
		// Skip the host (github.com, etc.)
		return parts[1], parts[2]
	}

	return "", ""
}
