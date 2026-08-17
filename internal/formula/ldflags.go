package formula

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// RubyLdflags renders a gobottle ldflags template — the very text
// source.build.ldflags feeds to the cross-compiler, with the same template
// fields — substituting Ruby interpolations for the build-time values, then
// splits the result into %W[] tokens:
//
//	{{.Version}} -> #{version}      {{.Tag}}    -> v#{version}
//	{{.Date}}    -> #{time.iso8601} {{.Commit}} -> #{<commitExpr>}
//
// commitExpr is the Ruby expression yielding the commit (typically the
// "commit" local computed by SourceBuild.CommitLocal).
//
// Bare -s and -w tokens are dropped: std_go_args already supplies them unless
// the user asked for --debug-symbols, and forwarding them would defeat that.
// -trimpath is likewise std_go_args' business, not the ldflags'.
//
// Every %W[] element is one whitespace-separated token, so ldflags values
// containing spaces are not supported — the same constraint the host build
// already imposes.
func RubyLdflags(tmplText, commitExpr string) ([]string, error) {
	if strings.TrimSpace(tmplText) == "" {
		return nil, nil
	}
	if commitExpr == "" {
		commitExpr = "commit"
	}

	tmpl, err := template.New("ldflags").Parse(tmplText)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ldflags template: %w", err)
	}

	data := struct {
		Version string
		Commit  string
		Date    string
		Tag     string
	}{
		Version: "#{version}",
		Commit:  "#{" + commitExpr + "}",
		Date:    "#{time.iso8601}",
		Tag:     "v#{version}",
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to render ldflags template: %w", err)
	}

	var tokens []string
	for _, tok := range strings.Fields(buf.String()) {
		if tok == "-s" || tok == "-w" {
			continue
		}
		tokens = append(tokens, tok)
	}
	return tokens, nil
}
