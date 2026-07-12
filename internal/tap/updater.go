package tap

import (
	"context"
	"fmt"

	"github.com/google/go-github/v88/github"
)

// Updater updates formulas in a Homebrew tap repository
type Updater struct {
	client *github.Client
	owner  string
	repo   string
	branch string
}

// NewUpdater creates a new tap updater
func NewUpdater(token, owner, repo, branch string) (*Updater, error) {
	var opts []github.ClientOptionsFunc
	if token != "" {
		opts = append(opts, github.WithAuthToken(token))
	}
	client, err := github.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	return &Updater{
		client: client,
		owner:  owner,
		repo:   repo,
		branch: branch,
	}, nil
}

// UpdateFormula creates or updates a formula file in the tap and returns the
// commit SHA. path is the file path within the repository (e.g.
// "Formula/myapp.rb"); content is the complete formula; message is the
// commit message.
func (u *Updater) UpdateFormula(ctx context.Context, path string, content []byte, message string) (string, error) {
	// An unconfigured branch means the repository's default branch — never
	// assume a name; taps predating the main-branch convention use master.
	if u.branch == "" {
		repo, _, err := u.client.Repositories.Get(ctx, u.owner, u.repo)
		if err != nil {
			return "", fmt.Errorf("failed to resolve default branch of %s/%s: %w", u.owner, u.repo, err)
		}
		u.branch = repo.GetDefaultBranch()
	}

	// Check if the target branch exists
	branchExists, err := u.BranchExists(ctx)
	if err != nil {
		return "", err
	}

	var sha *string
	var branchPtr *string

	if !branchExists {
		// Branch doesn't exist - check if repo is empty
		isEmpty, err := u.IsRepoEmpty(ctx)
		if err != nil {
			return "", err
		}

		if isEmpty {
			// Empty repo: create file without specifying branch
			// GitHub will create the initial commit on the default branch
			branchPtr = nil
			sha = nil
		} else {
			// Non-empty repo but branch doesn't exist
			return "", fmt.Errorf("branch %q does not exist in %s/%s\n"+
				"  Hint: create the branch first, or use --tap-branch to specify an existing branch",
				u.branch, u.owner, u.repo)
		}
	} else {
		// Branch exists - check if file exists to get its SHA
		branchPtr = new(u.branch)
		currentFile, _, resp, err := u.client.Repositories.GetContents(ctx, u.owner, u.repo, path, &github.RepositoryContentGetOptions{
			Ref: u.branch,
		})
		if err != nil {
			// If 404, file doesn't exist (this is fine, we'll create it)
			if resp != nil && resp.StatusCode == 404 {
				sha = nil
			} else {
				return "", fmt.Errorf("failed to check if formula exists: %w", err)
			}
		} else {
			// File exists, we need the SHA to update it
			sha = currentFile.SHA
		}
	}

	// Create or update the file
	opts := &github.RepositoryContentFileOptions{
		Message: new(message),
		Content: content,
		Branch:  branchPtr,
		SHA:     sha,
	}

	resp, _, err := u.client.Repositories.CreateFile(ctx, u.owner, u.repo, path, opts)
	if err != nil {
		return "", fmt.Errorf("failed to update formula: %w", err)
	}

	commitSHA := ""
	if resp != nil && resp.Commit.SHA != nil {
		commitSHA = *resp.Commit.SHA
	}
	return commitSHA, nil
}

// GetFormula fetches the current formula content at the given repo path.
// Returns the file content or an error if the file doesn't exist.
func (u *Updater) GetFormula(ctx context.Context, path string) ([]byte, error) {
	fileContent, _, _, err := u.client.Repositories.GetContents(ctx, u.owner, u.repo, path, &github.RepositoryContentGetOptions{
		Ref: u.branch,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get formula: %w", err)
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return nil, fmt.Errorf("failed to decode formula content: %w", err)
	}

	return []byte(content), nil
}

// FormulaExists checks if a formula exists in the tap at the given repo path
func (u *Updater) FormulaExists(ctx context.Context, path string) (bool, error) {
	_, err := u.GetFormula(ctx, path)
	if err != nil {
		// Check if it's a 404 error (not found)
		if errResp, ok := err.(*github.ErrorResponse); ok {
			if errResp.Response.StatusCode == 404 {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

// Branch returns the branch targeted for formula commits; when constructed
// with an empty branch it is resolved by UpdateFormula.
func (u *Updater) Branch() string {
	return u.branch
}

// BranchExists checks if the specified branch exists in the repository.
// Git.GetRef is used deliberately: Repositories.GetBranch follows redirects
// itself and returns a plain error (not *github.ErrorResponse) on 404,
// which would turn "branch missing" into a hard failure.
func (u *Updater) BranchExists(ctx context.Context) (bool, error) {
	_, _, err := u.client.Git.GetRef(ctx, u.owner, u.repo, "heads/"+u.branch)
	if err != nil {
		if errResp, ok := err.(*github.ErrorResponse); ok {
			if errResp.Response.StatusCode == 404 {
				return false, nil
			}
		}
		return false, fmt.Errorf("failed to check branch: %w", err)
	}
	return true, nil
}

// IsRepoEmpty checks if the repository has any commits
func (u *Updater) IsRepoEmpty(ctx context.Context) (bool, error) {
	_, resp, err := u.client.Repositories.ListCommits(ctx, u.owner, u.repo, &github.CommitsListOptions{
		ListOptions: github.ListOptions{PerPage: 1},
	})
	if err != nil {
		// 409 Conflict means empty repository
		if resp != nil && resp.StatusCode == 409 {
			return true, nil
		}
		return false, fmt.Errorf("failed to check repository status: %w", err)
	}
	return false, nil
}
