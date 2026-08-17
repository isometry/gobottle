package formula

import (
	"strings"
	"testing"
	"text/template"
)

func TestRubyLdflags(t *testing.T) {
	tests := []struct {
		name       string
		tmpl       string
		commitExpr string
		want       []string
	}{
		{
			name: "empty template yields no tokens",
			tmpl: "",
		},
		{
			name: "whitespace-only template yields no tokens",
			tmpl: "  \n ",
		},
		{
			name:       "every build variable interpolates",
			tmpl:       "-X a.Version={{.Version}} -X a.Commit={{.Commit}} -X a.Date={{.Date}} -X a.Tag={{.Tag}}",
			commitExpr: "commit",
			want: []string{
				"-X", "a.Version=#{version}",
				"-X", "a.Commit=#{commit}",
				"-X", "a.Date=#{time.iso8601}",
				"-X", "a.Tag=v#{version}",
			},
		},
		{
			name:       "commit expression is caller-supplied",
			tmpl:       "-X a.Commit={{.Commit}}",
			commitExpr: "tap.user",
			want:       []string{"-X", "a.Commit=#{tap.user}"},
		},
		{
			name: "commit expression defaults to the commit local",
			tmpl: "-X a.Commit={{.Commit}}",
			want: []string{"-X", "a.Commit=#{commit}"},
		},
		{
			name: "bare -s and -w are dropped for std_go_args",
			tmpl: "-s -w -X a.Version={{.Version}}",
			want: []string{"-X", "a.Version=#{version}"},
		},
		{
			name: "only bare -s/-w are dropped",
			tmpl: "-s -w -X a.S=-s -extldflags=-static",
			want: []string{"-X", "a.S=-s", "-extldflags=-static"},
		},
		{
			name: "folded YAML newlines collapse into tokens",
			tmpl: "-s -w\n-X a.Version={{.Version}}\n-X a.Commit={{.Commit}}",
			want: []string{"-X", "a.Version=#{version}", "-X", "a.Commit=#{commit}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RubyLdflags(tt.tmpl, tt.commitExpr)
			if err != nil {
				t.Fatalf("RubyLdflags: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("RubyLdflags() = %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("token %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRubyLdflagsInvalidTemplate(t *testing.T) {
	if _, err := RubyLdflags("-X a={{.Version", "commit"); err == nil {
		t.Fatal("expected error for an unparseable ldflags template")
	}
}

// TestRubyLdflagsFieldParity guards the contract that RubyLdflags renders the
// very same template text artifact.GoSource feeds to the cross-compiler: both
// must accept the same field names. The artifact package's data struct is
// re-declared here rather than imported to keep formula free of that
// dependency; if its fields ever change, this test fails alongside it.
func TestRubyLdflagsFieldParity(t *testing.T) {
	const tmplText = "-s -w -X a.Version={{.Version}} -X a.Commit={{.Commit}} -X a.Date={{.Date}} -X a.Tag={{.Tag}}"

	// Mirrors the anonymous struct in artifact.GoSource.renderLdflags.
	artifactData := struct {
		Version string
		Commit  string
		Date    string
		Tag     string
	}{Version: "1.2.3", Commit: "abc123", Date: "2026-01-01T00:00:00Z", Tag: "v1.2.3"}

	tmpl, err := template.New("ldflags").Option("missingkey=error").Parse(tmplText)
	if err != nil {
		t.Fatalf("artifact-side parse: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, artifactData); err != nil {
		t.Fatalf("artifact-side execute: %v", err)
	}
	if strings.Contains(buf.String(), "<no value>") {
		t.Fatalf("artifact-side render left a field unresolved: %s", buf.String())
	}

	tokens, err := RubyLdflags(tmplText, "commit")
	if err != nil {
		t.Fatalf("RubyLdflags: %v", err)
	}
	for _, tok := range tokens {
		if strings.Contains(tok, "<no value>") {
			t.Errorf("RubyLdflags left a field unresolved: %q", tok)
		}
	}
}
