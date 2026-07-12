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
			Packages []string `yaml:"packages"`
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
	detectGoReleaser(cfg)

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

`

	if err := os.WriteFile(configPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("failed to write configuration: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Created %s\n\n", configPath)
	fmt.Fprintln(os.Stderr, "Next steps:")
	fmt.Fprintln(os.Stderr, "  1. Edit formula.description (brew audit requires it)")
	fmt.Fprintln(os.Stderr, "  2. Set GITHUB_TOKEN (write:packages + tap contents:write)")
	fmt.Fprintln(os.Stderr, "  3. Run: gobottle release --dry-run")

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

// detectGoReleaser extracts project information from GoReleaser config
func detectGoReleaser(cfg *initFileConfig) {
	// Try common GoReleaser config locations
	paths := []string{
		".goreleaser.yaml",
		".goreleaser.yml",
		"goreleaser.yaml",
		"goreleaser.yml",
	}

	var configPath string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			configPath = p
			break
		}
	}

	if configPath == "" {
		return
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	// Parse GoReleaser config
	var grConfig struct {
		ProjectName string `yaml:"project_name"`
		Builds      []struct {
			Binary string `yaml:"binary"`
			Main   string `yaml:"main"`
		} `yaml:"builds"`
	}

	if err := yaml.Unmarshal(data, &grConfig); err != nil {
		return
	}

	// Prefer the GoReleaser binary/project name for the formula
	if len(grConfig.Builds) > 0 {
		if binary := grConfig.Builds[0].Binary; binary != "" {
			cfg.Formula.Name = binary
		}
		if main := grConfig.Builds[0].Main; main != "" {
			cfg.Source.Build.Packages = []string{main}
		}
	}
	if grConfig.ProjectName != "" && cfg.Formula.Name == "" {
		cfg.Formula.Name = grConfig.ProjectName
	}

	// Try to get module path from go.mod for homepage
	if cfg.Formula.Homepage == "" {
		if modPath := getModulePath(); strings.HasPrefix(modPath, "github.com/") {
			cfg.Formula.Homepage = "https://" + modPath
		}
	}
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
