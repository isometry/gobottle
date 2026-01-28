package formula

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"unicode"
)

// formulaTemplate is the Ruby formula template
const formulaTemplate = `class {{ .ClassName }} < Formula
  desc "{{ .Description }}"
  homepage "{{ .Homepage }}"
  url "{{ .URL }}"
  sha256 "{{ .SHA256 }}"
  license "{{ .License }}"
{{- if .Bottles }}

  bottle do
    root_url "{{ (index .Bottles 0).RootURL }}"
{{- range .Bottles }}
    sha256 cellar: {{ .Cellar }}, {{ .Platform }}: "{{ .SHA256 }}"
{{- end }}
  end
{{- end }}

  def install
{{- range .Binaries }}
    bin.install "{{ . }}"
{{- end }}
  end

  test do
    system bin/"{{ index .Binaries 0 }}", "--version"
  end
end
`

// Generate generates a Ruby formula from the Formula struct
func Generate(f *Formula) (string, error) {
	if f.ClassName == "" {
		f.ClassName = ToPascalCase(f.Name)
	}

	tmpl, err := template.New("formula").Parse(formulaTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse formula template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, f); err != nil {
		return "", fmt.Errorf("failed to execute formula template: %w", err)
	}

	return buf.String(), nil
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
