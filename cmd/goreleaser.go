package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// goreleaserConfigPaths are the file names goreleaser itself looks for.
var goreleaserConfigPaths = []string{
	".goreleaser.yaml",
	".goreleaser.yml",
	"goreleaser.yaml",
	"goreleaser.yml",
}

// goreleaserBuild is the subset of a goreleaser `builds:` entry that maps
// onto gobottle's source.build recipe, so `gobottle init` can scaffold a
// formula whose source build mirrors the binaries goreleaser ships.
type goreleaserBuild struct {
	Binary  string       `yaml:"binary"`
	Main    string       `yaml:"main"`
	Dir     string       `yaml:"dir"`
	Ldflags stringOrList `yaml:"ldflags"`
	Flags   stringOrList `yaml:"flags"`
	Env     stringOrList `yaml:"env"`
	Tags    stringOrList `yaml:"tags"`
}

// goreleaserConfig is the subset of .goreleaser.yaml gobottle reads.
type goreleaserConfig struct {
	ProjectName string            `yaml:"project_name"`
	Builds      []goreleaserBuild `yaml:"builds"`
}

// stringOrList accepts goreleaser's string-or-sequence YAML shape.
type stringOrList []string

func (s *stringOrList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*s = []string{node.Value}
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		*s = list
		return nil
	default:
		return fmt.Errorf("line %d: expected a string or a list", node.Line)
	}
}

// readGoReleaserConfig loads the first goreleaser config found in dir.
// Returns nil (no error) when there is none.
func readGoReleaserConfig(dir string) (*goreleaserConfig, error) {
	for _, name := range goreleaserConfigPaths {
		data, err := os.ReadFile(dir + "/" + name)
		if err != nil {
			continue
		}
		var cfg goreleaserConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", name, err)
		}
		return &cfg, nil
	}
	return nil, nil
}

// goreleaserTemplateVars maps goreleaser template fields onto gobottle's
// ldflags template fields. Anything else (.Env.X, .CommitTimestamp, .Branch,
// …) has no build-time equivalent in a formula and is rejected.
var goreleaserTemplateVars = map[string]string{
	"Version":     "Version",
	"Tag":         "Tag",
	"Commit":      "Commit",
	"FullCommit":  "Commit",
	"ShortCommit": "ShortCommit",
	"Date":        "Date",
	"CommitDate":  "Date",
}

var goreleaserTemplateRe = regexp.MustCompile(`\{\{-?\s*([^{}]*?)\s*-?\}\}`)

// translateGoreleaserTemplate rewrites goreleaser template actions into
// gobottle's ldflags template dialect, e.g. `{{ .ShortCommit }}` ->
// `{{.ShortCommit}}`. It fails on any action that is not a bare supported
// field, so the caller can leave the ldflags out rather than scaffold a
// template gobottle cannot render.
func translateGoreleaserTemplate(s string) (string, error) {
	var bad string
	out := goreleaserTemplateRe.ReplaceAllStringFunc(s, func(action string) string {
		m := goreleaserTemplateRe.FindStringSubmatch(action)
		field, ok := strings.CutPrefix(m[1], ".")
		if !ok {
			bad = action
			return action
		}
		if mapped, ok := goreleaserTemplateVars[field]; ok {
			return "{{." + mapped + "}}"
		}
		bad = action
		return action
	})
	if bad != "" {
		return "", fmt.Errorf("unsupported goreleaser template action %s", bad)
	}
	return out, nil
}

// importedBuild is the gobottle source.build recipe derived from a
// goreleaser build entry.
type importedBuild struct {
	Binary     string
	Packages   []string
	Ldflags    string
	Flags      []string
	Env        []string
	Tags       []string
	ModDir     string
	CGOEnabled bool
	// Warnings explains anything that could not be imported.
	Warnings []string
}

// importGoReleaserBuild translates a goreleaser build entry into gobottle's
// source.build vocabulary:
//
//   - main -> packages; dir -> mod_dir
//   - ldflags -> one whitespace-joined template (template vars translated)
//   - flags minus -trimpath (source.build.trimpath is on by default)
//   - env minus CGO_ENABLED, which becomes cgo_enabled
//   - tags as-is
func importGoReleaserBuild(b goreleaserBuild) importedBuild {
	out := importedBuild{Binary: b.Binary, Tags: []string(b.Tags)}

	if b.Main != "" {
		out.Packages = []string{b.Main}
	}
	if b.Dir != "" && b.Dir != "." {
		out.ModDir = b.Dir
	}

	if joined := strings.TrimSpace(strings.Join(b.Ldflags, " ")); joined != "" {
		ldflags, err := translateGoreleaserTemplate(joined)
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("could not import goreleaser ldflags (%v); set source.build.ldflags manually", err))
		} else {
			out.Ldflags = strings.Join(strings.Fields(ldflags), " ")
		}
	}

	for _, f := range b.Flags {
		for _, tok := range strings.Fields(f) {
			if tok == "-trimpath" {
				continue
			}
			out.Flags = append(out.Flags, tok)
		}
	}

	for _, kv := range b.Env {
		if key, value, ok := strings.Cut(kv, "="); ok && key == "CGO_ENABLED" {
			out.CGOEnabled = strings.TrimSpace(value) == "1"
			continue
		}
		out.Env = append(out.Env, kv)
	}

	return out
}
