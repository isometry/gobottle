package config

import (
	"fmt"
	"os"

	"github.com/isometry/gobottle/internal/git"
)

// Config represents the complete gobottle configuration
type Config struct {
	// Project identification
	Formula     string `mapstructure:"formula"`
	Version     string `mapstructure:"version"`
	Description string `mapstructure:"description"`
	Homepage    string `mapstructure:"homepage"`
	License     string `mapstructure:"license"`

	// Source configuration
	Source SourceConfig `mapstructure:"source"`

	// Bottle configuration
	Bottle BottleConfig `mapstructure:"bottle"`

	// Registry configuration
	Registry RegistryConfig `mapstructure:"registry"`

	// Tap configuration
	Tap TapConfig `mapstructure:"tap"`

	// Binary configuration (multi-binary support)
	Binaries []BinaryConfig `mapstructure:"binaries"`
}

// SourceConfig defines where to fetch artifacts from
type SourceConfig struct {
	// Type: "github" or "local"
	Type string `mapstructure:"type"`

	// GitHub source options
	Owner string `mapstructure:"owner"`
	Repo  string `mapstructure:"repo"`
	Tag   string `mapstructure:"tag"`

	// Local source options
	DistPath string `mapstructure:"dist_path"`
}

// BottleConfig defines bottle generation settings
type BottleConfig struct {
	// Cellar setting for bottles
	Cellar string `mapstructure:"cellar"`

	// Platforms to generate bottles for (empty = auto-discover all)
	Platforms []string `mapstructure:"platforms"`

	// Platforms to exclude from generation
	ExcludePlatforms []string `mapstructure:"exclude_platforms"`

	// Rebuild number (0 for initial release)
	Rebuild int `mapstructure:"rebuild"`

	// Force refresh platform cache
	RefreshPlatforms bool `mapstructure:"refresh_platforms"`
}

// RegistryConfig defines OCI registry settings
type RegistryConfig struct {
	// GHCR configuration
	Host    string `mapstructure:"host"`
	Owner   string `mapstructure:"owner"`
	Package string `mapstructure:"package"`

	// Authentication token
	Token string `mapstructure:"token"`
}

// TapConfig defines Homebrew tap repository settings
type TapConfig struct {
	// Tap repository
	Owner  string `mapstructure:"owner"`
	Repo   string `mapstructure:"repo"`
	Branch string `mapstructure:"branch"`

	// Formula path within tap
	FormulaPath string `mapstructure:"formula_path"`

	// Commit message template
	CommitMessage string `mapstructure:"commit_message"`

	// Token for tap repository (defaults to registry token)
	Token string `mapstructure:"token"`
}

// BinaryConfig defines a binary to include in bottles
type BinaryConfig struct {
	Name        string `mapstructure:"name"`
	InstallPath string `mapstructure:"install_path"`
}

// SetDefaults sets default values for the configuration
func (c *Config) SetDefaults() {
	if c.Source.Type == "" {
		c.Source.Type = "local"
	}
	if c.Source.DistPath == "" {
		c.Source.DistPath = "dist"
	}
	if c.Bottle.Cellar == "" {
		c.Bottle.Cellar = ":any_skip_relocation"
	}
	if c.Registry.Host == "" {
		c.Registry.Host = "ghcr.io"
	}
	if c.Tap.Repo == "" {
		c.Tap.Repo = "homebrew-tap"
	}
	if c.Tap.Branch == "" {
		c.Tap.Branch = "main"
	}
	if c.Tap.FormulaPath == "" {
		c.Tap.FormulaPath = "Formula"
	}
	if c.Tap.CommitMessage == "" {
		c.Tap.CommitMessage = "Update {{ .Formula }} to {{ .Version }}"
	}

	// Default package name to formula name
	if c.Registry.Package == "" && c.Formula != "" {
		c.Registry.Package = c.Formula
	}

	// Default binary to formula name
	if len(c.Binaries) == 0 && c.Formula != "" {
		c.Binaries = []BinaryConfig{{Name: c.Formula, InstallPath: "bin"}}
	}

	// Default install path for binaries
	for i := range c.Binaries {
		if c.Binaries[i].InstallPath == "" {
			c.Binaries[i].InstallPath = "bin"
		}
	}

	// Use registry token for tap if not specified
	if c.Tap.Token == "" && c.Registry.Token != "" {
		c.Tap.Token = c.Registry.Token
	}
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e *ValidationError) Error() string {
	if e.Value != nil {
		return fmt.Sprintf("validation error: %s: %s (got: %v)", e.Field, e.Message, e.Value)
	}
	return fmt.Sprintf("validation error: %s: %s", e.Field, e.Message)
}

// MultiError aggregates multiple errors
type MultiError struct {
	Errors []error
}

func (e *MultiError) Error() string {
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	msg := fmt.Sprintf("%d validation errors:\n", len(e.Errors))
	for _, err := range e.Errors {
		msg += fmt.Sprintf("  - %s\n", err.Error())
	}
	return msg
}

// Validate performs comprehensive config validation
func (c *Config) Validate() error {
	var errs []error

	// Required: formula name
	if c.Formula == "" {
		errs = append(errs, &ValidationError{
			Field:   "formula",
			Message: "formula name is required",
		})
	}

	// Source validation
	switch c.Source.Type {
	case "github":
		if c.Source.Owner == "" {
			errs = append(errs, &ValidationError{
				Field:   "source.owner",
				Message: "owner is required for github source",
			})
		}
		if c.Source.Repo == "" {
			errs = append(errs, &ValidationError{
				Field:   "source.repo",
				Message: "repo is required for github source",
			})
		}
		if c.Source.Tag == "" && c.Version == "" {
			errs = append(errs, &ValidationError{
				Field:   "source.tag",
				Message: "tag is required for github source (or specify version)",
			})
		}
	case "local":
		if c.Source.DistPath == "" {
			c.Source.DistPath = "dist"
		}
		// Verify dist path exists
		if _, err := os.Stat(c.Source.DistPath); os.IsNotExist(err) {
			errs = append(errs, &ValidationError{
				Field:   "source.dist_path",
				Value:   c.Source.DistPath,
				Message: "dist directory does not exist",
			})
		}
		// Version required for local source
		if c.Version == "" {
			errs = append(errs, &ValidationError{
				Field:   "version",
				Message: "version is required when using local source",
			})
		}
	case "":
		errs = append(errs, &ValidationError{
			Field:   "source.type",
			Message: "source type is required ('github' or 'local')",
		})
	default:
		errs = append(errs, &ValidationError{
			Field:   "source.type",
			Value:   c.Source.Type,
			Message: "must be 'github' or 'local'",
		})
	}

	// Registry validation
	if c.Registry.Owner == "" {
		errs = append(errs, &ValidationError{
			Field:   "registry.owner",
			Message: "registry owner is required (defaults to --owner if set)",
		})
	}

	// Token validation (allow from env)
	if c.Registry.Token == "" {
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			c.Registry.Token = token
		} else if token := os.Getenv("GH_TOKEN"); token != "" {
			c.Registry.Token = token
		} else {
			errs = append(errs, &ValidationError{
				Field:   "registry.token",
				Message: "token is required\n    Hint: set GITHUB_TOKEN or GH_TOKEN environment variable, or add registry.token to .gobottle.yaml\n    The token needs 'write:packages' scope for GHCR access",
			})
		}
	}

	// Tap validation
	if c.Tap.Owner == "" {
		errs = append(errs, &ValidationError{
			Field:   "tap.owner",
			Message: "tap owner is required (defaults to --owner if set)",
		})
	}

	// Binary validation
	if len(c.Binaries) == 0 {
		errs = append(errs, &ValidationError{
			Field:   "binaries",
			Message: "at least one binary is required",
		})
	}
	for i, bin := range c.Binaries {
		if bin.Name == "" {
			errs = append(errs, &ValidationError{
				Field:   fmt.Sprintf("binaries[%d].name", i),
				Message: "binary name is required",
			})
		}
	}

	if len(errs) > 0 {
		return &MultiError{Errors: errs}
	}

	return nil
}

// BinaryNames returns just the names of all configured binaries
func (c *Config) BinaryNames() []string {
	names := make([]string, len(c.Binaries))
	for i, b := range c.Binaries {
		names[i] = b.Name
	}
	return names
}

// ApplyGitDefaults attempts to infer configuration values from git context.
// Uses go-git library instead of exec'ing git binary.
func (c *Config) ApplyGitDefaults() {
	repo, err := git.Open(".")
	if err != nil {
		return // Not a git repo, skip git defaults
	}

	// Try to get owner/repo from git remote
	if c.Source.Owner == "" || c.Source.Repo == "" {
		if owner, repoName, err := repo.GetOwnerRepo(); err == nil {
			if c.Source.Owner == "" {
				c.Source.Owner = owner
			}
			if c.Source.Repo == "" {
				c.Source.Repo = repoName
			}
		}
	}

	// Cascade owner to other owner fields
	if c.Source.Owner != "" {
		if c.Registry.Owner == "" {
			c.Registry.Owner = c.Source.Owner
		}
		if c.Tap.Owner == "" {
			c.Tap.Owner = c.Source.Owner
		}
	}

	// Detect version from tag for ANY source type (not just github)
	if c.Version == "" {
		// First try to find a tag pointing to HEAD
		if tag, err := repo.GetLatestTag(); err == nil && tag != "" {
			if git.IsValidSemver(tag) {
				c.Version = git.ParseVersion(tag)
				if c.Source.Type == "github" && c.Source.Tag == "" {
					c.Source.Tag = tag
				}
			}
		} else {
			// Fall back to most recent reachable tag (like git describe --tags --abbrev=0)
			if tag, err := repo.GetLatestReachableTag(); err == nil && tag != "" {
				if git.IsValidSemver(tag) {
					c.Version = git.ParseVersion(tag)
					if c.Source.Type == "github" && c.Source.Tag == "" {
						c.Source.Tag = tag
					}
				}
			}
		}
	}

	// For github source, also set tag from version if not set
	if c.Source.Type == "github" && c.Source.Tag == "" && c.Version != "" {
		c.Source.Tag = git.NormalizeVersion(c.Version)
	}
}
