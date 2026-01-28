// Package git provides git repository operations using go-git.
package git

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Repository wraps go-git repository operations.
type Repository struct {
	repo *git.Repository
}

// Open opens the git repository at the given path.
// If path is empty or ".", it opens the repository in the current directory.
// It will search parent directories for a .git folder.
func Open(path string) (*Repository, error) {
	if path == "" {
		path = "."
	}
	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open git repository: %w", err)
	}
	return &Repository{repo: repo}, nil
}

// GetRemoteURL returns the URL of the "origin" remote.
func (r *Repository) GetRemoteURL() (string, error) {
	remote, err := r.repo.Remote("origin")
	if err != nil {
		return "", fmt.Errorf("failed to get origin remote: %w", err)
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", fmt.Errorf("no URLs configured for origin")
	}
	return urls[0], nil
}

// GetLatestTag returns the most recent semver tag reachable from HEAD.
// Returns empty string if no valid semver tags are found.
func (r *Repository) GetLatestTag() (string, error) {
	// Get HEAD reference
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	headCommit := head.Hash()

	// Get all tags
	tags, err := r.repo.Tags()
	if err != nil {
		return "", fmt.Errorf("failed to get tags: %w", err)
	}

	type tagInfo struct {
		name    string
		version *semver.Version
	}

	var validTags []tagInfo

	err = tags.ForEach(func(ref *plumbing.Reference) error {
		tagName := ref.Name().Short()

		// Check if this tag is valid semver
		if !IsValidSemver(tagName) {
			return nil
		}

		// Parse the version for sorting
		v, err := semver.NewVersion(tagName)
		if err != nil {
			return nil
		}

		// Check if this tag points to HEAD (directly or via annotated tag)
		tagHash := ref.Hash()

		// Try to dereference if it's an annotated tag
		tagObj, err := r.repo.TagObject(tagHash)
		if err == nil {
			// It's an annotated tag, get the commit it points to
			tagHash = tagObj.Target
		}

		if tagHash == headCommit {
			validTags = append(validTags, tagInfo{name: tagName, version: v})
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to iterate tags: %w", err)
	}

	if len(validTags) == 0 {
		return "", nil
	}

	// Sort by semver (highest first)
	sort.Slice(validTags, func(i, j int) bool {
		return validTags[i].version.GreaterThan(validTags[j].version)
	})

	return validTags[0].name, nil
}

// GetLatestReachableTag returns the most recent semver tag reachable from HEAD,
// including tags on ancestor commits. This is similar to `git describe --tags --abbrev=0`.
func (r *Repository) GetLatestReachableTag() (string, error) {
	// Get HEAD reference
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	// Build a map of commit hash -> tag for all valid semver tags
	tagMap := make(map[plumbing.Hash][]struct {
		name    string
		version *semver.Version
	})

	tags, err := r.repo.Tags()
	if err != nil {
		return "", fmt.Errorf("failed to get tags: %w", err)
	}

	err = tags.ForEach(func(ref *plumbing.Reference) error {
		tagName := ref.Name().Short()

		if !IsValidSemver(tagName) {
			return nil
		}

		v, err := semver.NewVersion(tagName)
		if err != nil {
			return nil
		}

		// Get the commit this tag points to
		tagHash := ref.Hash()
		tagObj, err := r.repo.TagObject(tagHash)
		if err == nil {
			tagHash = tagObj.Target
		}

		tagMap[tagHash] = append(tagMap[tagHash], struct {
			name    string
			version *semver.Version
		}{name: tagName, version: v})

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to iterate tags: %w", err)
	}

	if len(tagMap) == 0 {
		return "", nil
	}

	// Walk commit history from HEAD looking for tagged commits
	commitIter, err := r.repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		return "", fmt.Errorf("failed to get commit log: %w", err)
	}

	var bestTag string
	var bestVersion *semver.Version

	for {
		commit, err := commitIter.Next()
		if err != nil {
			break
		}

		if tagsOnCommit, ok := tagMap[commit.Hash]; ok {
			// Found a tagged commit, pick the highest version
			for _, t := range tagsOnCommit {
				if bestVersion == nil || t.version.GreaterThan(bestVersion) {
					bestTag = t.name
					bestVersion = t.version
				}
			}
			// Stop at the first tagged commit we find
			break
		}
	}

	return bestTag, nil
}

// IsClean returns true if the working tree has no uncommitted changes.
func (r *Repository) IsClean() (bool, error) {
	wt, err := r.repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("failed to get worktree: %w", err)
	}
	status, err := wt.Status()
	if err != nil {
		return false, fmt.Errorf("failed to get status: %w", err)
	}
	return status.IsClean(), nil
}

// GetOwnerRepo extracts owner and repo from the origin remote URL.
func (r *Repository) GetOwnerRepo() (owner, repo string, err error) {
	url, err := r.GetRemoteURL()
	if err != nil {
		return "", "", err
	}
	owner, repo = ParseRemoteURL(url)
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("could not parse owner/repo from URL: %s", url)
	}
	return owner, repo, nil
}

// HasUncommittedChanges returns true if there are staged or unstaged changes.
func (r *Repository) HasUncommittedChanges() (bool, error) {
	clean, err := r.IsClean()
	if err != nil {
		return false, err
	}
	return !clean, nil
}

// GetCurrentBranch returns the name of the current branch.
func (r *Repository) GetCurrentBranch() (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	if !head.Name().IsBranch() {
		return "", fmt.Errorf("HEAD is not a branch (detached HEAD)")
	}
	return strings.TrimPrefix(head.Name().String(), "refs/heads/"), nil
}
