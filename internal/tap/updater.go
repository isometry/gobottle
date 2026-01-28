package tap

import (
	"context"
	"fmt"

	"github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
)

// Updater updates formulas in a Homebrew tap repository
type Updater struct {
	client *github.Client
	owner  string
	repo   string
	branch string
}

// NewUpdater creates a new tap updater
func NewUpdater(token, owner, repo, branch string) *Updater {
	var client *github.Client
	if token != "" {
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
		tc := oauth2.NewClient(context.Background(), ts)
		client = github.NewClient(tc)
	} else {
		client = github.NewClient(nil)
	}

	if branch == "" {
		branch = "main"
	}

	return &Updater{
		client: client,
		owner:  owner,
		repo:   repo,
		branch: branch,
	}
}

// UpdateFormula updates or creates a formula in the tap
// formulaName should be just the formula name (e.g., "myapp"), not the full path
// content is the complete formula content
// message is the commit message
func (u *Updater) UpdateFormula(ctx context.Context, formulaName string, content []byte, message string) error {
	path := fmt.Sprintf("Formula/%s.rb", formulaName)

	// Get the current file to check if it exists and get its SHA
	currentFile, _, resp, err := u.client.Repositories.GetContents(ctx, u.owner, u.repo, path, &github.RepositoryContentGetOptions{
		Ref: u.branch,
	})

	var sha *string
	if err != nil {
		// If 404, file doesn't exist (this is fine, we'll create it)
		if resp != nil && resp.StatusCode == 404 {
			sha = nil
		} else {
			return fmt.Errorf("failed to check if formula exists: %w", err)
		}
	} else {
		// File exists, we need the SHA to update it
		sha = currentFile.SHA
	}

	// Create or update the file
	opts := &github.RepositoryContentFileOptions{
		Message: github.String(message),
		Content: content,
		Branch:  github.String(u.branch),
		SHA:     sha,
	}

	_, _, err = u.client.Repositories.CreateFile(ctx, u.owner, u.repo, path, opts)
	if err != nil {
		return fmt.Errorf("failed to update formula: %w", err)
	}

	return nil
}

// GetFormula fetches the current formula content
// Returns the file content or an error if the file doesn't exist
func (u *Updater) GetFormula(ctx context.Context, formulaName string) ([]byte, error) {
	path := fmt.Sprintf("Formula/%s.rb", formulaName)

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

// FormulaExists checks if a formula exists in the tap
func (u *Updater) FormulaExists(ctx context.Context, formulaName string) (bool, error) {
	_, err := u.GetFormula(ctx, formulaName)
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
