package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestGetHeadCommitTime(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := wt.Add("file.txt"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	when := time.Date(2026, 7, 1, 12, 34, 56, 0, time.UTC)
	sig := &object.Signature{Name: "Test", Email: "test@example.com", When: when}
	if _, err := wt.Commit("initial", &gogit.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := r.GetHeadCommitTime()
	if err != nil {
		t.Fatalf("GetHeadCommitTime: %v", err)
	}
	if !got.Equal(when) {
		t.Errorf("GetHeadCommitTime = %v, want %v", got, when)
	}
}

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "SSH format",
			url:       "git@github.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH format without .git",
			url:       "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS format",
			url:       "https://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS format without .git",
			url:       "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTP format",
			url:       "http://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "GitLab SSH format",
			url:       "git@gitlab.com:mygroup/myproject.git",
			wantOwner: "mygroup",
			wantRepo:  "myproject",
		},
		{
			name:      "Invalid URL",
			url:       "not-a-url",
			wantOwner: "",
			wantRepo:  "",
		},
		{
			name:      "Empty URL",
			url:       "",
			wantOwner: "",
			wantRepo:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOwner, gotRepo := ParseRemoteURL(tt.url)
			if gotOwner != tt.wantOwner {
				t.Errorf("ParseRemoteURL() owner = %q, want %q", gotOwner, tt.wantOwner)
			}
			if gotRepo != tt.wantRepo {
				t.Errorf("ParseRemoteURL() repo = %q, want %q", gotRepo, tt.wantRepo)
			}
		})
	}
}

func TestIsValidSemver(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"simple version", "1.2.3", true},
		{"version with v prefix", "v1.2.3", true},
		{"version with prerelease", "v1.2.3-beta", true},
		{"version with prerelease number", "v1.2.3-rc.1", true},
		{"version with build metadata", "v1.2.3+build.123", true},
		{"version with prerelease and build", "v1.2.3-beta+build.123", true},
		{"zero version", "v0.0.0", true},
		{"large numbers", "v100.200.300", true},

		// Invalid versions
		{"empty string", "", false},
		{"just v", "v", false},
		{"missing patch", "v1.2", false},
		{"missing minor", "v1", false},
		{"non-numeric major", "va.2.3", false},
		{"non-numeric minor", "v1.b.3", false},
		{"non-numeric patch", "v1.2.c", false},
		{"release-candidate", "release-candidate", false},
		{"arbitrary tag", "my-tag", false},
		{"date tag", "2024.01.15", true}, // technically valid semver (major.minor.patch)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidSemver(tt.version); got != tt.want {
				t.Errorf("IsValidSemver(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want string
	}{
		{"strip v prefix", "v1.2.3", "1.2.3"},
		{"no v prefix", "1.2.3", "1.2.3"},
		{"prerelease with v", "v1.2.3-beta", "1.2.3-beta"},
		{"prerelease without v", "1.2.3-beta", "1.2.3-beta"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseVersion(tt.tag); got != tt.want {
				t.Errorf("ParseVersion(%q) = %q, want %q", tt.tag, got, tt.want)
			}
		})
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"already has v", "v1.2.3", "v1.2.3"},
		{"needs v prefix", "1.2.3", "v1.2.3"},
		{"prerelease with v", "v1.2.3-beta", "v1.2.3-beta"},
		{"prerelease needs v", "1.2.3-beta", "v1.2.3-beta"},
		{"empty string", "", "v"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeVersion(tt.version); got != tt.want {
				t.Errorf("NormalizeVersion(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}
