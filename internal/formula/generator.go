package formula

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"unicode"
)

// defaultTemplate renders a complete Homebrew formula from the Formula model.
// Stanzas are conditional: empty values are omitted entirely rather than
// rendered as empty strings (brew audit rejects `desc ""` / `license ""`).
const defaultTemplate = `class {{ .ClassName }} < Formula
{{- if .Description }}
  desc {{ quote .Description }}
{{- end }}
{{- if .Homepage }}
  homepage {{ quote .Homepage }}
{{- end }}
{{- if .URL }}
  url {{ quote .URL }}
{{- end }}
{{- if .SHA256 }}
  sha256 {{ quote .SHA256 }}
{{- end }}
{{- if .License }}
  license {{ quote .License }}
{{- end }}
{{- if .Head }}
  head {{ quote .Head.URL }}, branch: {{ quote .Head.Branch }}
{{- end }}
{{- if .Bottles }}

  bottle do
    root_url {{ quote .RootURL }}
{{- if gt .Rebuild 0 }}
    rebuild {{ .Rebuild }}
{{- end }}
{{- range .Bottles }}
    sha256 cellar: {{ cellar .Cellar }}, {{ .Platform }}: {{ quote .SHA256 }}
{{- end }}
  end
{{- end }}
{{- range .Dependencies }}

  depends_on {{ quote . }}
{{- end }}
{{- range .Conflicts }}

  conflicts_with {{ quote . }}
{{- end }}

  def install
{{- range .Binaries }}
{{- if eq .InstallPath "bin" }}
    bin.install {{ quote .Name }}
{{- else if eq .InstallPath "libexec" }}
    libexec.install {{ quote .Name }}
{{- else }}
    (prefix/{{ quote .InstallPath }}).install {{ quote .Name }}
{{- end }}
{{- end }}
{{- if .Completions }}
{{- range .BinBinaries }}
    generate_completions_from_executable(bin/{{ quote . }}, "completion")
{{- end }}
{{- end }}
{{- range .ExtraInstall }}
    {{ . }}
{{- end }}
  end
{{- if .Caveats }}

  def caveats
    <<~EOS
{{ indentHeredoc .Caveats }}
    EOS
  end
{{- end }}
{{- if .Service }}

  service do
{{ indentBlock .Service }}
  end
{{- end }}

  test do
{{- if .Test.Raw }}
{{ indentBlock .Test.Raw }}
{{- else }}
    system bin/{{ quote (index .BinBinaries 0) }}{{ range .Test.Command }}, {{ quote . }}{{ end }}
{{- end }}
  end
end
`

// Head describes an optional `head` stanza.
type Head struct {
	URL    string // git URL, e.g. https://github.com/owner/repo.git
	Branch string // e.g. "main"
}

// Test describes the `test do` block. If Raw is set it is used verbatim
// (indented); otherwise `system bin/"<first binary>", <Command...>` is
// rendered, with Command defaulting to ["--version"].
type Test struct {
	Command []string
	Raw     string
}

// Generate renders a Ruby formula from the Formula model using the default
// template, or tmplText if non-empty (a user-supplied override receiving the
// same model).
func Generate(f *Formula, tmplText string) (string, error) {
	if f.ClassName == "" {
		f.ClassName = ToPascalCase(f.Name)
	}
	if f.Test.Command == nil && f.Test.Raw == "" {
		f.Test.Command = []string{"--version"}
	}
	if len(f.Binaries) == 0 {
		return "", fmt.Errorf("formula %s: at least one binary is required", f.Name)
	}
	for i := range f.Binaries {
		if f.Binaries[i].InstallPath == "" {
			f.Binaries[i].InstallPath = "bin"
		}
	}
	if f.Test.Raw == "" && len(f.BinBinaries()) == 0 {
		return "", fmt.Errorf("formula %s: the default test requires a bin-installed binary; set an explicit test", f.Name)
	}
	if f.URL == "" && f.Head == nil {
		return "", fmt.Errorf("formula %s: a source url (or head) is required", f.Name)
	}

	if tmplText == "" {
		tmplText = defaultTemplate
	}

	tmpl, err := template.New("formula").Funcs(template.FuncMap{
		"quote":         quoteRuby,
		"cellar":        cellarValue,
		"indentHeredoc": indentLines(6),
		"indentBlock":   indentLines(4),
	}).Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("failed to parse formula template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, f); err != nil {
		return "", fmt.Errorf("failed to render formula: %w", err)
	}

	return buf.String(), nil
}

// quoteRuby renders a double-quoted Ruby string literal.
func quoteRuby(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "#{", `\#{`)
	return `"` + s + `"`
}

// cellarValue renders a cellar setting: Ruby symbols (":any",
// ":any_skip_relocation") stay bare, absolute paths are quoted.
func cellarValue(cellar string) string {
	if strings.HasPrefix(cellar, ":") {
		return cellar
	}
	return quoteRuby(cellar)
}

// indentLines returns a template func indenting every non-empty line by n spaces.
func indentLines(n int) func(string) string {
	pad := strings.Repeat(" ", n)
	return func(s string) string {
		lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) != "" {
				lines[i] = pad + line
			} else {
				lines[i] = ""
			}
		}
		return strings.Join(lines, "\n")
	}
}

// ToPascalCase converts a string to PascalCase for Ruby class names
// Examples:
//   - "myapp" -> "Myapp"
//   - "my-app" -> "MyApp"
//   - "my_app" -> "MyApp"
//   - "myApp" -> "MyApp"
func ToPascalCase(s string) string {
	if s == "" {
		return ""
	}

	// Split on delimiters (-, _, space) and camelCase boundaries
	var words []string
	var currentWord strings.Builder

	prevWasUpper := false
	prevWasDelimiter := true

	for i, r := range s {
		if r == '-' || r == '_' || r == ' ' {
			if currentWord.Len() > 0 {
				words = append(words, currentWord.String())
				currentWord.Reset()
			}
			prevWasDelimiter = true
			prevWasUpper = false
			continue
		}

		isUpper := unicode.IsUpper(r)

		// Start new word if:
		// 1. Current char is uppercase and previous was lowercase (camelCase boundary)
		// 2. Previous was delimiter
		if isUpper && !prevWasUpper && !prevWasDelimiter && currentWord.Len() > 0 {
			words = append(words, currentWord.String())
			currentWord.Reset()
		}

		currentWord.WriteRune(r)
		prevWasUpper = isUpper
		prevWasDelimiter = false

		// Check if this is the last character
		if i == len(s)-1 && currentWord.Len() > 0 {
			words = append(words, currentWord.String())
		}
	}

	// Capitalize each word
	var result strings.Builder
	for _, word := range words {
		if len(word) == 0 {
			continue
		}
		// Capitalize first letter, lowercase the rest
		result.WriteRune(unicode.ToUpper(rune(word[0])))
		if len(word) > 1 {
			result.WriteString(strings.ToLower(word[1:]))
		}
	}

	return result.String()
}
