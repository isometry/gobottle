package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/isometry/gobottle/internal/git"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// InitOptions holds options for the init command
type InitOptions struct {
	Force bool
}

// NewInitCommand creates the init command
func NewInitCommand() *cobra.Command {
	opts := &InitOptions{}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new gobottle configuration",
		Long: `Initialize a new .gobottle.yaml configuration file in the current directory.

This command will:
- Detect the git remote origin for owner/repo/formula defaults
- Detect an existing GoReleaser configuration for binary names
- Generate a configuration with the formula content section ready to edit

The generated configuration can be customized before running 'gobottle release'.`,
		Example: `  # Initialize with auto-detection
  gobottle init

  # Overwrite existing configuration
  gobottle init --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "overwrite existing configuration")

	return cmd
}

// initFileConfig mirrors the .gobottle.yaml structure for generation.
type initFileConfig struct {
	Formula struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Homepage    string `yaml:"homepage,omitempty"`
		License     string `yaml:"license"`
		Test        struct {
			Command []string `yaml:"command"`
		} `yaml:"test"`
	} `yaml:"formula"`

	Source struct {
		Type  string `yaml:"type"`
		Build struct {
			Packages   []string `yaml:"packages"`
			Ldflags    string   `yaml:"ldflags,omitempty"`
			Flags      []string `yaml:"flags,omitempty"`
			Env        []string `yaml:"env,omitempty"`
			Tags       []string `yaml:"tags,omitempty"`
			ModDir     string   `yaml:"mod_dir,omitempty"`
			CGOEnabled bool     `yaml:"cgo_enabled,omitempty"`
		} `yaml:"build"`
	} `yaml:"source"`

	Tap struct {
		Repo string `yaml:"repo"`
	} `yaml:"tap"`
}

func runInit(opts *InitOptions) error {
	configPath := ".gobottle.yaml"

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil && !opts.Force {
		return fmt.Errorf("configuration file %s already exists (use --force to overwrite)", configPath)
	}

	cfg := &initFileConfig{}

	// Defaults for the flagship go-source mode
	cfg.Source.Type = "go"
	cfg.Source.Build.Packages = []string{"."}
	cfg.Tap.Repo = "homebrew-tap"
	cfg.Formula.License = "MIT"
	cfg.Formula.Test.Command = []string{"--version"}

	// Detect values from the environment
	detectGitRemote(cfg)
	imported, err := detectGoReleaser(cfg, ".")
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration: %w", err)
	}

	header := `# gobottle configuration
# See: https://github.com/isometry/gobottle
#
# owner/repo/version are derived from git; registry root path defaults to
# <owner>/<tap repo minus homebrew- prefix>. The formula section drives the
# fully generated formula (dependencies, caveats, completions, service,
# template override, ... are also available).
#
# The formula's install block compiles from source (so --build-from-source
# and --HEAD work), inheriting packages/ldflags/flags/env/tags from
# source.build; override any of it under formula.build.
`
	if imported {
		header += `#
# source.build was imported from the goreleaser build so that a source
# build reproduces the released binaries (template vars translated:
# .ShortCommit -> {{.ShortCommit}}, .CommitDate -> {{.Date}}, ...).
`
	}
	header += "\n"

	if err := os.WriteFile(configPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("failed to write configuration: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Created %s\n\n", configPath)
	fmt.Fprintln(os.Stderr, "Next steps:")
	fmt.Fprintln(os.Stderr, "  1. Edit formula.description (brew audit requires it)")
	if imported {
		fmt.Fprintln(os.Stderr, "  2. Check source.build (imported from goreleaser) matches how you build")
		fmt.Fprintln(os.Stderr, "  3. Set GITHUB_TOKEN (write:packages + tap contents:write)")
		fmt.Fprintln(os.Stderr, "  4. Run: gobottle release --dry-run")
	} else {
		fmt.Fprintln(os.Stderr, "  2. Set GITHUB_TOKEN (write:packages + tap contents:write)")
		fmt.Fprintln(os.Stderr, "  3. Run: gobottle release --dry-run")
	}

	return nil
}

// detectGitRemote extracts owner/repo from git remote origin using go-git
func detectGitRemote(cfg *initFileConfig) {
	repo, err := git.Open(".")
	if err != nil {
		return
	}

	owner, repoName, err := repo.GetOwnerRepo()
	if err != nil {
		return
	}

	if repoName != "" && cfg.Formula.Name == "" {
		cfg.Formula.Name = repoName
	}
	if owner != "" && repoName != "" {
		cfg.Formula.Homepage = fmt.Sprintf("https://github.com/%s/%s", owner, repoName)
	}
}

// detectGoReleaser imports the goreleaser build recipe (binary, main,
// ldflags, flags, env, tags, dir) from the config found in dir so the
// scaffolded source.build — and therefore the formula's source build —
// mirrors the binaries goreleaser releases. It reports whether a goreleaser
// build was imported; anything untranslatable is warned about and left out.
func detectGoReleaser(cfg *initFileConfig, dir string) (bool, error) {
	gr, err := readGoReleaserConfig(dir)
	if err != nil {
		return false, err
	}
	if gr == nil {
		return false, nil
	}

	if gr.ProjectName != "" && cfg.Formula.Name == "" {
		cfg.Formula.Name = gr.ProjectName
	}
	if len(gr.Builds) == 0 {
		return false, nil
	}

	// Prefer the GoReleaser binary name for the formula
	b := importGoReleaserBuild(gr.Builds[0])
	if b.Binary != "" {
		cfg.Formula.Name = b.Binary
	}
	if len(b.Packages) > 0 {
		cfg.Source.Build.Packages = b.Packages
	}
	cfg.Source.Build.Ldflags = b.Ldflags
	cfg.Source.Build.Flags = b.Flags
	cfg.Source.Build.Env = b.Env
	cfg.Source.Build.Tags = b.Tags
	cfg.Source.Build.ModDir = b.ModDir
	cfg.Source.Build.CGOEnabled = b.CGOEnabled
	for _, w := range b.Warnings {
		warn("%s", w)
	}

	// Try to get module path from go.mod for homepage
	if cfg.Formula.Homepage == "" {
		if modPath := getModulePath(); strings.HasPrefix(modPath, "github.com/") {
			cfg.Formula.Homepage = "https://" + modPath
		}
	}
	return true, nil
}

// getModulePath extracts the module path from go.mod
func getModulePath() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return ""
	}

	lines := strings.SplitSeq(string(data), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return after
		}
	}

	return ""
}
