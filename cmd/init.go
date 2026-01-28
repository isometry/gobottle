package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/isometry/gobottle/internal/git"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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
- Detect existing GoReleaser configuration and extract project details
- Detect git remote origin for owner/repo defaults
- Generate a sample configuration file with sensible defaults

The generated configuration can be customized before running 'gobottle bottle'.`,
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

// gobottleConfig represents the .gobottle.yaml structure
type gobottleConfig struct {
	Formula     string `yaml:"formula"`
	Description string `yaml:"description,omitempty"`
	Homepage    string `yaml:"homepage,omitempty"`
	License     string `yaml:"license,omitempty"`

	Source struct {
		Type     string `yaml:"type"`
		Owner    string `yaml:"owner"`
		Repo     string `yaml:"repo"`
		DistPath string `yaml:"dist_path,omitempty"`
	} `yaml:"source"`

	Registry struct {
		Host    string `yaml:"host"`
		Owner   string `yaml:"owner,omitempty"`
		Package string `yaml:"package,omitempty"`
	} `yaml:"registry"`

	Tap struct {
		Owner  string `yaml:"owner,omitempty"`
		Repo   string `yaml:"repo"`
		Branch string `yaml:"branch"`
	} `yaml:"tap"`

	Bottle struct {
		Cellar string `yaml:"cellar"`
	} `yaml:"bottle"`
}

func runInit(opts *InitOptions) error {
	configPath := ".gobottle.yaml"

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil && !opts.Force {
		return fmt.Errorf("configuration file %s already exists (use --force to overwrite)", configPath)
	}

	cfg := &gobottleConfig{}

	// Set defaults
	cfg.Source.Type = "local"
	cfg.Source.DistPath = "dist"
	cfg.Registry.Host = "ghcr.io"
	cfg.Tap.Repo = "homebrew-tap"
	cfg.Tap.Branch = "main"
	cfg.Bottle.Cellar = ":any_skip_relocation"

	// Try to detect values from environment
	detectGitRemote(cfg)
	detectGoReleaser(cfg)

	// Generate YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration: %w", err)
	}

	// Add header comment
	header := `# gobottle configuration
# See: https://github.com/isometry/gobottle

`

	if err := os.WriteFile(configPath, []byte(header+string(data)), 0644); err != nil {
		return fmt.Errorf("failed to write configuration: %w", err)
	}

	fmt.Printf("Created %s\n", configPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review and edit the configuration as needed")
	fmt.Println("  2. Set GITHUB_TOKEN environment variable")
	fmt.Println("  3. Run: gobottle bottle --version <version>")

	return nil
}

// detectGitRemote extracts owner/repo from git remote origin using go-git
func detectGitRemote(cfg *gobottleConfig) {
	repo, err := git.Open(".")
	if err != nil {
		return
	}

	owner, repoName, err := repo.GetOwnerRepo()
	if err != nil {
		return
	}

	if owner != "" {
		cfg.Source.Owner = owner
	}
	if repoName != "" {
		cfg.Source.Repo = repoName
		// Use repo name as formula name if not set
		if cfg.Formula == "" {
			cfg.Formula = repoName
		}
	}
}

// detectGoReleaser extracts project information from GoReleaser config
func detectGoReleaser(cfg *gobottleConfig) {
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

	// Extract binary name from builds
	if len(grConfig.Builds) > 0 {
		binary := grConfig.Builds[0].Binary
		if binary != "" && cfg.Formula == "" {
			cfg.Formula = binary
		}
	}

	// Use project name if set
	if grConfig.ProjectName != "" && cfg.Formula == "" {
		cfg.Formula = grConfig.ProjectName
	}

	// Try to get module path from go.mod for homepage
	if modPath := getModulePath(); modPath != "" {
		// Convert module path to homepage URL
		if strings.HasPrefix(modPath, "github.com/") {
			cfg.Homepage = "https://" + modPath
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
