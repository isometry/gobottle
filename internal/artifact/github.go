package artifact

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v88/github"
)

// GitHubSource fetches artifacts from GitHub releases
type GitHubSource struct {
	client     *github.Client
	owner      string
	repo       string
	tag        string
	release    *github.RepositoryRelease
	checksums  map[string]string
	tempDir    string
	discovered bool
}

// GitHubSourceConfig configures a GitHub artifact source
type GitHubSourceConfig struct {
	Owner string // GitHub repository owner
	Repo  string // GitHub repository name
	Tag   string // Release tag (e.g., "v1.0.0")
	Token string // GitHub token (optional, for private repos or rate limiting)
}

// NewGitHubSource creates a new GitHub artifact source
func NewGitHubSource(cfg GitHubSourceConfig) (*GitHubSource, error) {
	if cfg.Owner == "" {
		return nil, fmt.Errorf("owner is required")
	}
	if cfg.Repo == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if cfg.Tag == "" {
		return nil, fmt.Errorf("tag is required")
	}

	// Authenticate when a token is provided (private repos, rate limits)
	var opts []github.ClientOptionsFunc
	if cfg.Token != "" {
		opts = append(opts, github.WithAuthToken(cfg.Token))
	}
	client, err := github.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Create temp directory for downloads
	tempDir, err := os.MkdirTemp("", "gobottle-github-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &GitHubSource{
		client:    client,
		owner:     cfg.Owner,
		repo:      cfg.Repo,
		tag:       cfg.Tag,
		tempDir:   tempDir,
		checksums: make(map[string]string),
	}, nil
}

// List returns all available artifacts from the GitHub release
func (s *GitHubSource) List(ctx context.Context) ([]Artifact, error) {
	if s.discovered && s.release != nil {
		return s.buildArtifactList(), nil
	}

	// Fetch the release
	release, _, err := s.client.Repositories.GetReleaseByTag(ctx, s.owner, s.repo, s.tag)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch release %s: %w", s.tag, err)
	}

	s.release = release

	// Load checksums from release assets
	if err := s.loadChecksums(ctx); err != nil {
		// Non-fatal: checksums are optional
	}

	s.discovered = true

	return s.buildArtifactList(), nil
}

// Fetch downloads a GitHub release asset to the target directory
func (s *GitHubSource) Fetch(ctx context.Context, artifact Artifact, targetDir string) (string, error) {
	if s.release == nil {
		return "", fmt.Errorf("release not loaded, call List first")
	}

	// Ensure target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create target directory: %w", err)
	}

	// Find the asset
	var asset *github.ReleaseAsset
	for _, a := range s.release.Assets {
		if a.GetName() == artifact.Name {
			asset = a
			break
		}
	}

	if asset == nil {
		return "", fmt.Errorf("asset %s not found in release", artifact.Name)
	}

	// Download to temp location first
	tempPath := filepath.Join(s.tempDir, artifact.Name)

	// Check if already downloaded
	if _, err := os.Stat(tempPath); err == nil {
		// Already downloaded, verify checksum if available
		if artifact.Checksum != "" {
			actualChecksum, err := computeFileSHA256(tempPath)
			if err != nil {
				return "", fmt.Errorf("failed to compute checksum: %w", err)
			}
			if err := ValidateChecksum(actualChecksum, artifact.Checksum); err != nil {
				// Checksum mismatch, re-download
				os.Remove(tempPath)
			} else {
				// Checksum matches, copy to target
				return s.copyToTarget(tempPath, targetDir, artifact.Name)
			}
		} else {
			// No checksum, just copy
			return s.copyToTarget(tempPath, targetDir, artifact.Name)
		}
	}

	// Download the asset
	if err := s.downloadAsset(ctx, asset, tempPath); err != nil {
		return "", fmt.Errorf("failed to download asset: %w", err)
	}

	// Verify checksum if available
	if artifact.Checksum != "" {
		actualChecksum, err := computeFileSHA256(tempPath)
		if err != nil {
			return "", fmt.Errorf("failed to compute checksum: %w", err)
		}
		if err := ValidateChecksum(actualChecksum, artifact.Checksum); err != nil {
			os.Remove(tempPath)
			return "", err
		}
	}

	// Copy to target directory
	return s.copyToTarget(tempPath, targetDir, artifact.Name)
}

// Close releases any resources and cleans up temp directory
func (s *GitHubSource) Close() error {
	if s.tempDir != "" {
		return os.RemoveAll(s.tempDir)
	}
	return nil
}

// buildArtifactList builds the artifact list from release assets
func (s *GitHubSource) buildArtifactList() []Artifact {
	var artifacts []Artifact

	for _, asset := range s.release.Assets {
		name := asset.GetName()

		// Skip checksum files
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

		// Get checksum
		checksum := s.checksums[name]

		artifacts = append(artifacts, Artifact{
			Name:     name,
			Path:     "", // Will be set when fetched
			OS:       os,
			Arch:     arch,
			Checksum: checksum,
		})
	}

	return artifacts
}

// loadChecksums attempts to load checksums from release assets
func (s *GitHubSource) loadChecksums(ctx context.Context) error {
	if s.release == nil {
		return fmt.Errorf("release not loaded")
	}

	// Look for checksum files
	checksumFiles := []string{
		"checksums.txt",
		"SHA256SUMS",
		"checksums.sha256",
	}

	for _, filename := range checksumFiles {
		for _, asset := range s.release.Assets {
			if asset.GetName() == filename {
				// Download and parse checksum file
				content, err := s.downloadAssetToMemory(ctx, asset)
				if err != nil {
					continue
				}

				s.checksums = ParseChecksumsFile(content)
				return nil
			}
		}
	}

	// Try individual .sha256 files
	for _, asset := range s.release.Assets {
		name := asset.GetName()
		if strings.HasSuffix(name, ".sha256") {
			content, err := s.downloadAssetToMemory(ctx, asset)
			if err != nil {
				continue
			}

			// The filename is the .sha256 file without the extension
			artifactName := strings.TrimSuffix(name, ".sha256")
			checksum := strings.TrimSpace(content)
			// Handle format: "checksum  filename" or just "checksum"
			parts := strings.Fields(checksum)
			if len(parts) > 0 {
				s.checksums[artifactName] = parts[0]
			}
		}
	}

	return nil
}

// downloadAsset downloads a release asset to a file
func (s *GitHubSource) downloadAsset(ctx context.Context, asset *github.ReleaseAsset, targetPath string) error {
	// Get download URL
	rc, redirectURL, err := s.client.Repositories.DownloadReleaseAsset(ctx, s.owner, s.repo, asset.GetID(), http.DefaultClient)
	if err != nil {
		return fmt.Errorf("failed to get download URL: %w", err)
	}

	// If we got a redirect URL, download from there
	if redirectURL != "" {
		resp, err := http.Get(redirectURL)
		if err != nil {
			return fmt.Errorf("failed to download from redirect URL: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download failed with status: %s", resp.Status)
		}

		rc = resp.Body
	}
	defer rc.Close()

	// Create target file
	out, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Copy contents
	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// downloadAssetToMemory downloads a release asset to memory (for small files like checksums)
func (s *GitHubSource) downloadAssetToMemory(ctx context.Context, asset *github.ReleaseAsset) (string, error) {
	rc, redirectURL, err := s.client.Repositories.DownloadReleaseAsset(ctx, s.owner, s.repo, asset.GetID(), http.DefaultClient)
	if err != nil {
		return "", fmt.Errorf("failed to get download URL: %w", err)
	}

	// If we got a redirect URL, download from there
	if redirectURL != "" {
		resp, err := http.Get(redirectURL)
		if err != nil {
			return "", fmt.Errorf("failed to download from redirect URL: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("download failed with status: %s", resp.Status)
		}

		rc = resp.Body
	}
	defer rc.Close()

	// Read contents
	content, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("failed to read content: %w", err)
	}

	return string(content), nil
}

// copyToTarget copies a file from temp to target directory
func (s *GitHubSource) copyToTarget(srcPath, targetDir, filename string) (string, error) {
	targetPath := filepath.Join(targetDir, filename)

	// If paths are the same, no need to copy
	if srcPath == targetPath {
		return targetPath, nil
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to create target file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	return targetPath, nil
}
