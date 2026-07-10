package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/isometry/gobottle/internal/git"
)

// Config represents the complete gobottle configuration.
// Precedence (highest first): CLI flag > GOBOTTLE_* env > config file >
// git-derived defaults > built-in defaults.
type Config struct {
	// Formula content (gobottle owns the generated formula outright)
	Formula FormulaConfig `mapstructure:"formula"`

	// Version being bottled (no "v" prefix; normalized in SetDefaults)
	Version string `mapstructure:"version"`

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

// FormulaConfig drives formula generation: every overrideable aspect of the
// rendered formula lives here. A bare string in YAML ("formula: mytool") is
// accepted as shorthand for {name: mytool}.
type FormulaConfig struct {
	// Name of the formula (default: repo name)
	Name string `mapstructure:"name"`

	// Description for the desc stanza (brew audit requires one; warned if empty)
	Description string `mapstructure:"description"`

	// Homepage URL (default: https://github.com/<owner>/<repo>)
	Homepage string `mapstructure:"homepage"`

	// License as an SPDX identifier; omitted from the formula if empty
	License string `mapstructure:"license"`

	// Head emits a `head "<repo>.git", branch: "<branch>"` stanza
	Head bool `mapstructure:"head"`

	// HeadBranch overrides the head branch (default: "main")
	HeadBranch string `mapstructure:"head_branch"`

	// Dependencies become depends_on lines
	Dependencies []string `mapstructure:"dependencies"`

	// Conflicts become conflicts_with lines
	Conflicts []string `mapstructure:"conflicts"`

	// Caveats is emitted verbatim in a caveats heredoc
	Caveats string `mapstructure:"caveats"`

	// Install customizes the install block beyond bin.install
	Install InstallConfig `mapstructure:"install"`

	// Test customizes the test block
	Test TestConfig `mapstructure:"test"`

	// Service is a verbatim `service do` block body
	Service string `mapstructure:"service"`

	// Template is a path to a Go template overriding the built-in formula
	// template; it receives the full formula model.
	Template string `mapstructure:"template"`
}

// InstallConfig customizes the generated install block.
type InstallConfig struct {
	// Completions emits generate_completions_from_executable for each binary
	Completions bool `mapstructure:"completions"`

	// Extra lines are appended verbatim to the install block
	Extra []string `mapstructure:"extra"`
}

// TestConfig customizes the generated test block.
type TestConfig struct {
	// Command arguments for `system bin/"<binary>", ...` (default: ["--version"])
	Command []string `mapstructure:"command"`

	// Raw replaces the whole test body verbatim
	Raw string `mapstructure:"raw"`
}

// SourceConfig defines where to fetch artifacts from
type SourceConfig struct {
	// Type: "github", "local", or "go"
	Type string `mapstructure:"type"`

	// GitHub source options
	Owner string `mapstructure:"owner"`
	Repo  string `mapstructure:"repo"`
	Tag   string `mapstructure:"tag"`

	// Local source options
	DistPath string `mapstructure:"dist_path"`

	// Go source options (ko-like mode)
	Build BuildConfig `mapstructure:"build"`

	// Source tarball options (for formula generation)
	URL    string `mapstructure:"url"`    // Source tarball URL (auto-derived if empty)
	SHA256 string `mapstructure:"sha256"` // Source tarball SHA256 (computed if empty)
}

// BuildConfig defines Go build configuration for ko-like mode
type BuildConfig struct {
	// Packages to build (e.g., ["./cmd/myapp"])
	Packages []string `mapstructure:"packages"`

	// Ldflags template (supports {{.Version}}, {{.Commit}}, {{.Date}}, {{.Tag}})
	Ldflags string `mapstructure:"ldflags"`

	// Extra environment variables for go build, as KEY=value strings.
	// (A YAML map would lose case: viper lowercases map keys.)
	Env []string `mapstructure:"env"`

	// Enable CGO (default: false for portable static binaries)
	CGOEnabled bool `mapstructure:"cgo_enabled"`

	// Use -trimpath for reproducible builds (default: true)
	Trimpath bool `mapstructure:"trimpath"`

	// Extra go build flags
	Flags []string `mapstructure:"flags"`

	// Path to go.mod directory (default: ".")
	ModDir string `mapstructure:"mod_dir"`

	// Number of parallel builds (default: GOMAXPROCS)
	Parallel int `mapstructure:"parallel"`
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
	Host  string `mapstructure:"host"`
	Owner string `mapstructure:"owner"`

	// RootPath is the image path between host and formula name
	// (default: "<owner>/<tap repo minus homebrew- prefix>", mirroring
	// homebrew-core's ghcr.io/homebrew/core/<formula> layout). The formula's
	// root_url becomes "https://<host>/v2/<root_path>"; brew appends the
	// formula name itself. Env: GOBOTTLE_REPO or GOBOTTLE_REGISTRY_ROOT_PATH.
	RootPath string `mapstructure:"root_path"`

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

// BinaryConfig defines a binary to include in bottles. A bare string in YAML
// is accepted as shorthand for {name: ...}.
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

	// Go source defaults
	if c.Source.Type == "go" {
		if c.Source.Build.ModDir == "" {
			c.Source.Build.ModDir = "."
		}
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

	// Normalize version: Homebrew versions have no "v" prefix.
	c.Version = strings.TrimPrefix(c.Version, "v")

	// Cascade the source owner to registry and tap owners.
	if c.Registry.Owner == "" {
		c.Registry.Owner = c.Source.Owner
	}
	if c.Tap.Owner == "" {
		c.Tap.Owner = c.Source.Owner
	}

	// Default registry root path: <owner>/<tap repo minus homebrew- prefix>
	if c.Registry.RootPath == "" && c.Registry.Owner != "" {
		c.Registry.RootPath = fmt.Sprintf("%s/%s",
			c.Registry.Owner, strings.TrimPrefix(c.Tap.Repo, "homebrew-"))
	}

	// Default homepage from GitHub coordinates
	if c.Formula.Homepage == "" && c.Source.Owner != "" && c.Source.Repo != "" {
		c.Formula.Homepage = fmt.Sprintf("https://github.com/%s/%s", c.Source.Owner, c.Source.Repo)
	}
	if c.Formula.HeadBranch == "" {
		c.Formula.HeadBranch = "main"
	}

	// Default binary to formula name
	if len(c.Binaries) == 0 && c.Formula.Name != "" {
		c.Binaries = []BinaryConfig{{Name: c.Formula.Name, InstallPath: "bin"}}
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
	Value   any
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
	return c.ValidateFor(true)
}

// ValidateFor validates the config for a pipeline stage. needToken=false
// skips the registry-token requirement (local build stage).
func (c *Config) ValidateFor(needToken bool) error {
	var errs []error

	// Required: formula name
	if c.Formula.Name == "" {
		errs = append(errs, &ValidationError{
			Field:   "formula.name",
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
	case "go":
		// Version required for go source
		if c.Version == "" {
			errs = append(errs, &ValidationError{
				Field:   "version",
				Message: "version is required when using go source",
			})
		}
		// Packages required for go source
		if len(c.Source.Build.Packages) == 0 {
			errs = append(errs, &ValidationError{
				Field:   "source.build.packages",
				Message: "at least one package is required for go source",
			})
		}
		// Verify mod_dir exists if specified
		modDir := c.Source.Build.ModDir
		if modDir == "" {
			modDir = "."
		}
		if _, err := os.Stat(modDir); os.IsNotExist(err) {
			errs = append(errs, &ValidationError{
				Field:   "source.build.mod_dir",
				Value:   modDir,
				Message: "module directory does not exist",
			})
		}
	case "":
		errs = append(errs, &ValidationError{
			Field:   "source.type",
			Message: "source type is required ('github', 'local', or 'go')",
		})
	default:
		errs = append(errs, &ValidationError{
			Field:   "source.type",
			Value:   c.Source.Type,
			Message: "must be 'github', 'local', or 'go'",
		})
	}

	// Registry validation
	if c.Registry.RootPath == "" {
		errs = append(errs, &ValidationError{
			Field:   "registry.root_path",
			Message: "registry root path is required (derived from --owner and tap repo if set)",
		})
	}

	// Token validation (allow from env)
	if c.Registry.Token == "" {
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			c.Registry.Token = token
		} else if token := os.Getenv("GH_TOKEN"); token != "" {
			c.Registry.Token = token
		} else if needToken {
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

	// Template file must exist when configured
	if c.Formula.Template != "" {
		if _, err := os.Stat(c.Formula.Template); err != nil {
			errs = append(errs, &ValidationError{
				Field:   "formula.template",
				Value:   c.Formula.Template,
				Message: "template file does not exist",
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

	// Default the formula name to the repo name
	if c.Formula.Name == "" && c.Source.Repo != "" {
		c.Formula.Name = c.Source.Repo
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

// SourceURL returns the source tarball URL for the formula.
// Returns explicit URL if set, otherwise derives from GitHub owner/repo/version.
// Returns empty string when derivation is not possible.
func (c *Config) SourceURL() string {
	if c.Source.URL != "" {
		return c.Source.URL
	}

	// Need owner and repo to derive GitHub URL
	if c.Source.Owner == "" || c.Source.Repo == "" {
		return ""
	}

	// Determine tag: explicit tag, or "v" + version
	tag := c.Source.Tag
	if tag == "" && c.Version != "" {
		tag = "v" + c.Version
	}
	if tag == "" {
		return ""
	}

	return fmt.Sprintf("https://github.com/%s/%s/archive/refs/tags/%s.tar.gz",
		c.Source.Owner, c.Source.Repo, tag)
}

// GitURL returns the git clone URL for the source repository (for head stanzas).
func (c *Config) GitURL() string {
	if c.Source.Owner == "" || c.Source.Repo == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s.git", c.Source.Owner, c.Source.Repo)
}

// TapGitHubURL returns the GitHub URL for the tap repository
func (c *Config) TapGitHubURL() string {
	if c.Tap.Owner == "" || c.Tap.Repo == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s", c.Tap.Owner, c.Tap.Repo)
}

// RootURL returns the bottle block root_url. Brew appends "/<formula>" to
// this when constructing manifest and blob URLs, so the formula name must
// NOT be part of it.
func (c *Config) RootURL() string {
	if c.Registry.Host == "" || c.Registry.RootPath == "" {
		return ""
	}
	return fmt.Sprintf("https://%s/v2/%s", c.Registry.Host, strings.ToLower(c.Registry.RootPath))
}
