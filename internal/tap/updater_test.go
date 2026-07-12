package tap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"
)

// newTestUpdater returns an Updater backed by an httptest GitHub API stub.
func newTestUpdater(t *testing.T, branch string, mux *http.ServeMux) *Updater {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	base, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	client.BaseURL = base

	return &Updater{client: client, owner: "acme", repo: "homebrew-tap", branch: branch}
}

func TestBranchExists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/homebrew-tap/git/ref/heads/master", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(github.Reference{Ref: new("refs/heads/master")})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})

	u := newTestUpdater(t, "master", mux)
	exists, err := u.BranchExists(context.Background())
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if !exists {
		t.Error("existing branch reported as missing")
	}

	// A missing branch must be (false, nil), not an error - the caller
	// renders a helpful hint from the false path.
	u.branch = "main"
	exists, err = u.BranchExists(context.Background())
	if err != nil {
		t.Fatalf("BranchExists on missing branch must not error: %v", err)
	}
	if exists {
		t.Error("missing branch reported as existing")
	}
}

func TestUpdateFormulaResolvesDefaultBranch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/homebrew-tap", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(github.Repository{DefaultBranch: new("master")})
	})
	mux.HandleFunc("GET /repos/acme/homebrew-tap/git/ref/heads/master", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(github.Reference{Ref: new("refs/heads/master")})
	})
	mux.HandleFunc("GET /repos/acme/homebrew-tap/contents/Formula/mytool.rb", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	mux.HandleFunc("PUT /repos/acme/homebrew-tap/contents/Formula/mytool.rb", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Branch *string `json:"branch"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Branch == nil || *body.Branch != "master" {
			t.Errorf("commit targeted branch %v, want master", body.Branch)
		}
		_ = json.NewEncoder(w).Encode(github.RepositoryContentResponse{
			Commit: github.Commit{SHA: new("abc123")},
		})
	})

	u := newTestUpdater(t, "", mux) // empty branch: resolve the default
	sha, err := u.UpdateFormula(context.Background(), "Formula/mytool.rb", []byte("class Mytool < Formula\nend\n"), "update")
	if err != nil {
		t.Fatalf("UpdateFormula: %v", err)
	}
	if sha != "abc123" {
		t.Errorf("commit SHA = %q, want abc123", sha)
	}
	if u.Branch() != "master" {
		t.Errorf("resolved branch = %q, want master", u.Branch())
	}
}

func TestUpdateFormulaMissingBranchHint(t *testing.T) {
	mux := http.NewServeMux()
	// Branch ref lookup: 404 (missing branch)
	mux.HandleFunc("GET /repos/acme/homebrew-tap/git/ref/heads/topic", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	})
	// Non-empty repository
	mux.HandleFunc("GET /repos/acme/homebrew-tap/commits", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"sha":"abc"}]`)
	})

	u := newTestUpdater(t, "topic", mux)
	_, err := u.UpdateFormula(context.Background(), "Formula/mytool.rb", []byte("x"), "update")
	if err == nil {
		t.Fatal("expected error for missing branch on non-empty repo")
	}
	want := `branch "topic" does not exist`
	if got := err.Error(); !strings.Contains(got, want) {
		t.Errorf("error %q missing hint %q", got, want)
	}
}
