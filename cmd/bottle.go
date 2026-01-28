package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/isometry/gobottle/internal/artifact"
	"github.com/isometry/gobottle/internal/bottle"
	"github.com/isometry/gobottle/internal/config"
	"github.com/isometry/gobottle/internal/formula"
	"github.com/isometry/gobottle/internal/git"
	"github.com/isometry/gobottle/internal/oci"
	"github.com/isometry/gobottle/internal/platform"
	"github.com/isometry/gobottle/internal/tap"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// BottleOptions holds options for the bottle command
type BottleOptions struct {
	// Source options
	Source   string
	Owner    string
	Repo     string
	Tag      string
	DistPath string

	// Formula options
	Formula  string
	Version  string
	Binaries []string

	// Bottle options
	Platforms        []string
	ExcludePlatforms []string
	Cellar           string
	Rebuild          int
	RefreshPlatforms bool

	// Registry options
	RegistryHost  string
	RegistryOwner string
	Package       string
	Push          bool

	// Tap options
	TapOwner      string
	TapRepo       string
	TapBranch     string
	UpdateFormula bool

	// Build options
	Snapshot bool // Allow building from dirty working tree

	// Go source options (ko-like mode)
	Packages   []string // Go packages to build
	Ldflags    string   // ldflags template
	CGOEnabled bool     // Enable CGO
	Parallel   int      // Number of parallel builds

	// Output options
	Output string
}

// NewBottleCommand creates the bottle command
func NewBottleCommand(rootOpts *Options) *cobra.Command {
	opts := &BottleOptions{Push: true}

	cmd := &cobra.Command{
		Use:   "bottle [formula]",
		Short: "Build and push bottles from GoReleaser artifacts or Go source",
		Long: `Build Homebrew bottles from GoReleaser artifacts or Go source and push to GHCR.

This command orchestrates the full workflow:
1. Fetches artifacts from GitHub Release, local dist/, or builds from Go source
2. Discovers supported Homebrew platforms from Homebrew source
3. Builds bottles for each platform
4. Pushes bottles to GHCR as OCI artifacts
5. Updates the tap formula with the bottle block

Source types:
  - local:  Use pre-built archives from dist/ directory (default)
  - github: Fetch archives from a GitHub release
  - go:     Build directly from Go source (ko-like mode)`,
		Example: `  # Build bottles from local dist/ directory
  gobottle bottle myformula --version 1.0.0 --owner myorg

  # Build bottles from a GitHub release
  gobottle bottle --formula myformula --source github --owner myorg --repo myrepo --tag v1.0.0

  # Build bottles directly from Go source (ko-like mode)
  gobottle bottle myformula --source go --packages ./cmd/myapp --version 1.0.0 --owner myorg

  # Build from Go source with ldflags
  gobottle bottle myformula --source go --packages ./cmd/myapp \
      --ldflags "-X main.version={{.Version}} -X main.commit={{.Commit}}" \
      --version 1.0.0 --owner myorg

  # Build bottles without pushing to registry (local testing)
  gobottle bottle myformula --version 1.0.0 --owner myorg --push=false

  # Build bottles for specific platforms only
  gobottle bottle myformula --version 1.0.0 --owner myorg --platforms arm64_sonoma,ventura

  # Build and push but skip tap formula update
  gobottle bottle myformula --version 1.0.0 --owner myorg --skip-tap-update

  # Output bottle references as JSON
  gobottle bottle myformula --version 1.0.0 --owner myorg --output json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Support formula as positional argument
			if len(args) > 0 && opts.Formula == "" {
				opts.Formula = args[0]
			}
			return runBottle(cmd, rootOpts, opts)
		},
	}

	// Source flags
	cmd.Flags().StringVar(&opts.Source, "source", "local",
		"artifact source: 'github', 'local', or 'go' (build from source)")
	cmd.Flags().StringVar(&opts.Owner, "owner", "",
		"GitHub owner/org (required)")
	cmd.Flags().StringVar(&opts.Repo, "repo", "",
		"GitHub repository name (required for --source=github)")
	cmd.Flags().StringVar(&opts.Tag, "tag", "",
		"release tag, e.g., v1.2.3 (required for --source=github)")
	cmd.Flags().StringVar(&opts.DistPath, "dist", "dist",
		"path to dist/ directory (for --source=local)")

	// Go source flags (ko-like mode)
	cmd.Flags().StringSliceVar(&opts.Packages, "packages", nil,
		"Go packages to build (for --source=go), e.g., ./cmd/myapp")
	cmd.Flags().StringVar(&opts.Ldflags, "ldflags", "",
		"ldflags template for go build (supports {{.Version}}, {{.Commit}}, {{.Date}}, {{.Tag}})")
	cmd.Flags().BoolVar(&opts.CGOEnabled, "cgo", false,
		"enable CGO for go build (default: false for portable static binaries)")
	cmd.Flags().IntVar(&opts.Parallel, "parallel", 0,
		"number of parallel builds for --source=go (default: CPU count)")

	// Formula flags
	cmd.Flags().StringVar(&opts.Formula, "formula", "",
		"formula name (can also be passed as positional argument)")
	cmd.Flags().StringVar(&opts.Version, "version", "",
		"version string (derived from --tag if not specified)")
	cmd.Flags().StringSliceVar(&opts.Binaries, "binaries", nil,
		"binary names to include (default: formula name)")

	// Bottle flags
	cmd.Flags().StringSliceVar(&opts.Platforms, "platforms", nil,
		"platforms to build bottles for, e.g., arm64_sonoma,ventura (default: auto-discover)")
	cmd.Flags().StringSliceVar(&opts.ExcludePlatforms, "exclude-platforms", nil,
		"platforms to exclude from bottle generation")
	cmd.Flags().StringVar(&opts.Cellar, "cellar", ":any_skip_relocation",
		"cellar setting: ':any', ':any_skip_relocation', or absolute path")
	cmd.Flags().IntVar(&opts.Rebuild, "rebuild", 0,
		"rebuild number (increment when re-bottling same version)")
	cmd.Flags().BoolVar(&opts.RefreshPlatforms, "refresh-platforms", false,
		"force refresh platform cache from Homebrew source")

	// Registry flags
	cmd.Flags().StringVar(&opts.RegistryHost, "registry", "ghcr.io",
		"OCI registry host")
	cmd.Flags().StringVar(&opts.RegistryOwner, "registry-owner", "",
		"registry owner (defaults to --owner)")
	cmd.Flags().StringVar(&opts.Package, "package", "",
		"OCI package name (defaults to formula name)")
	cmd.Flags().BoolVar(&opts.Push, "push", true,
		"push bottles to registry (use --push=false to build locally only)")

	// Tap flags
	cmd.Flags().StringVar(&opts.TapOwner, "tap-owner", "",
		"tap owner (defaults to --owner)")
	cmd.Flags().StringVar(&opts.TapRepo, "tap-repo", "homebrew-tap",
		"tap repository name")
	cmd.Flags().StringVar(&opts.TapBranch, "tap-branch", "main",
		"tap branch to update")
	cmd.Flags().BoolVar(&opts.UpdateFormula, "update-formula", true,
		"update tap formula with bottle block")
	cmd.Flags().Bool("skip-tap-update", false,
		"skip tap formula update (alias for --update-formula=false)")

	// Build options
	cmd.Flags().BoolVar(&opts.Snapshot, "snapshot", false,
		"allow building from dirty working tree (uncommitted changes)")

	// Output flags
	cmd.Flags().StringVarP(&opts.Output, "output", "o", "text",
		"output format: 'text', 'json', or 'bare' (just image refs)")

	return cmd
}

func runBottle(cmd *cobra.Command, rootOpts *Options, opts *BottleOptions) error {
	ctx := cmd.Context()

	// Handle --skip-tap-update flag (alias for --update-formula=false)
	if skipTapUpdate, _ := cmd.Flags().GetBool("skip-tap-update"); skipTapUpdate {
		opts.UpdateFormula = false
	}

	// Check working tree cleanliness (default: require clean)
	if !opts.Snapshot {
		if repo, err := git.Open("."); err == nil {
			if dirty, err := repo.HasUncommittedChanges(); err == nil && dirty {
				return fmt.Errorf("git working tree has uncommitted changes\n  Hint: commit your changes or use --snapshot to build anyway")
			}
		}
	}

	// Build config from flags and viper
	cfg := buildConfig(opts)

	// Apply smart defaults from git context
	cfg.ApplyGitDefaults()

	// Apply remaining defaults
	cfg.SetDefaults()

	// Validate config (skip token validation if not pushing)
	if !opts.Push {
		// Clear token requirement for local-only builds
		cfg.Registry.Token = "not-required"
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Determine output mode
	jsonOutput := opts.Output == "json"
	bareOutput := opts.Output == "bare"
	quietOutput := jsonOutput || bareOutput

	if rootOpts.DryRun && !quietOutput {
		fmt.Println("Dry run mode - no changes will be made")
		fmt.Println()
	}

	if !quietOutput {
		fmt.Printf("Building bottles for formula: %s\n", cfg.Formula)
		fmt.Printf("Version: %s\n", cfg.Version)
		fmt.Printf("Source: %s\n", cfg.Source.Type)
		fmt.Printf("Binaries: %v\n", cfg.BinaryNames())
		fmt.Println()
	}

	// Step 1: Create artifact source
	if !quietOutput {
		fmt.Println("Step 1: Loading artifacts...")
	}
	source, err := createArtifactSource(cfg)
	if err != nil {
		return fmt.Errorf("failed to create artifact source: %w", err)
	}
	defer func() { _ = source.Close() }()

	// List available artifacts
	artifacts, err := source.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list artifacts: %w", err)
	}

	if len(artifacts) == 0 {
		return fmt.Errorf("no artifacts found")
	}

	if !quietOutput {
		fmt.Printf("  Found %d artifacts\n", len(artifacts))
		for _, a := range artifacts {
			fmt.Printf("    - %s (%s/%s)\n", a.Name, a.OS, a.Arch)
		}
		fmt.Println()
	}

	// Step 2: Discover platforms
	if !quietOutput {
		fmt.Println("Step 2: Discovering platforms...")
	}
	cacheDir, err := GetCacheDir()
	if err != nil {
		return err
	}

	discoverer, err := platform.NewDiscoverer(cacheDir)
	if err != nil {
		return fmt.Errorf("failed to create platform discoverer: %w", err)
	}

	platformInfo, err := discoverer.Discover(ctx, cfg.Bottle.RefreshPlatforms)
	if err != nil {
		return fmt.Errorf("failed to discover platforms: %w", err)
	}

	if !quietOutput {
		fmt.Printf("  Discovered %d macOS versions and %d Linux architectures\n",
			len(platformInfo.MacOSVersions), len(platformInfo.LinuxArches))
		fmt.Println()
	}

	// Step 3: Build bottles
	if !quietOutput {
		fmt.Println("Step 3: Building bottles...")
	}
	builder, err := bottle.NewBuilder()
	if err != nil {
		return fmt.Errorf("failed to create bottle builder: %w", err)
	}
	defer func() { _ = builder.Close() }()

	var bottles []*bottle.Bottle
	for _, art := range artifacts {
		// Get all Homebrew platforms for this artifact
		platforms := platformInfo.GetPlatformsForOS(art.OS, art.Arch)

		// Apply platform filters
		platforms = platform.FilterPlatforms(platforms, cfg.Bottle.Platforms, cfg.Bottle.ExcludePlatforms)

		if len(platforms) == 0 {
			if !quietOutput {
				fmt.Printf("  Skipping %s (no matching platforms)\n", art.Name)
			}
			continue
		}

		// Fetch artifact to temp directory
		tempDir, err := os.MkdirTemp("", "gobottle-artifact-*")
		if err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(tempDir) }()

		artPath, err := source.Fetch(ctx, art, tempDir)
		if err != nil {
			return fmt.Errorf("failed to fetch artifact %s: %w", art.Name, err)
		}

		// Build a bottle for each platform
		for _, plat := range platforms {
			if !quietOutput {
				fmt.Printf("  Building %s for %s...\n", cfg.Formula, plat.Tag)
			}

			b, err := builder.Build(ctx, bottle.BuildOptions{
				Formula:      cfg.Formula,
				Version:      cfg.Version,
				Platform:     plat,
				ArtifactPath: artPath,
				Binaries:     cfg.BinaryNames(),
				Cellar:       cfg.Bottle.Cellar,
				Rebuild:      cfg.Bottle.Rebuild,
				Tap:          fmt.Sprintf("%s/%s", cfg.Tap.Owner, cfg.Tap.Repo),
			})
			if err != nil {
				return fmt.Errorf("failed to build bottle for %s: %w", plat.Tag, err)
			}

			bottles = append(bottles, b)
			if !quietOutput {
				fmt.Printf("    Created: %s (SHA256: %s...)\n", b.BottleName(), b.SHA256[:12])
			}
		}
	}

	if len(bottles) == 0 {
		return fmt.Errorf("no bottles were built")
	}

	if !quietOutput {
		fmt.Printf("\n  Built %d bottles\n\n", len(bottles))
	}

	// Step 4: Push bottles to registry
	var imageRefs []string
	if opts.Push {
		if !quietOutput {
			fmt.Println("Step 4: Pushing bottles to registry...")
		}
		if rootOpts.DryRun {
			if !quietOutput {
				fmt.Println("  [DRY RUN] Would push bottles to registry")
				for _, b := range bottles {
					fmt.Printf("    - %s\n", b.BottleName())
				}
			}
			// Generate expected refs for dry run
			for _, b := range bottles {
				ref := fmt.Sprintf("%s/%s/%s:%s", cfg.Registry.Host, cfg.Registry.Owner, cfg.Registry.Package, b.Platform.Tag)
				imageRefs = append(imageRefs, ref)
			}
		} else {
			pusher, err := oci.NewPusher(
				cfg.Registry.Host,
				cfg.Registry.Owner,
				cfg.Registry.Package,
				cfg.Registry.Token,
			)
			if err != nil {
				return fmt.Errorf("failed to create OCI pusher: %w", err)
			}

			if err := pusher.PushAll(ctx, bottles); err != nil {
				return fmt.Errorf("failed to push bottles: %w", err)
			}

			// Collect image refs
			for _, b := range bottles {
				ref := pusher.GetFullReference(b)
				imageRefs = append(imageRefs, ref)
			}

			if !quietOutput {
				fmt.Printf("  Pushed %d bottles to %s/%s/%s\n",
					len(bottles), cfg.Registry.Host, cfg.Registry.Owner, cfg.Registry.Package)
			}
		}
	} else {
		if !quietOutput {
			fmt.Println("Step 4: Skipping push (--push=false)")
		}
		// Generate local bottle paths for reference
		for _, b := range bottles {
			imageRefs = append(imageRefs, b.Path)
		}
	}
	if !quietOutput {
		fmt.Println()
	}

	// Step 5: Update tap formula
	if opts.UpdateFormula && opts.Push {
		if !quietOutput {
			fmt.Println("Step 5: Updating tap formula...")
		}
		if err := updateTapFormula(ctx, cfg, bottles, rootOpts.DryRun); err != nil {
			return fmt.Errorf("failed to update tap formula: %w", err)
		}
	} else if !quietOutput {
		if !opts.Push {
			fmt.Println("Step 5: Skipping tap formula update (push disabled)")
		} else {
			fmt.Println("Step 5: Skipping tap formula update (--update-formula=false)")
		}
	}

	// Output results based on format
	if jsonOutput {
		return outputJSON(bottles, imageRefs, cfg)
	} else if bareOutput {
		for _, ref := range imageRefs {
			fmt.Println(ref)
		}
		return nil
	}

	fmt.Println()
	fmt.Println("Done!")
	return nil
}

// createArtifactSource creates the appropriate artifact source based on config
func createArtifactSource(cfg *config.Config) (artifact.Source, error) {
	switch cfg.Source.Type {
	case "local":
		return artifact.NewLocalSource(cfg.Source.DistPath)
	case "github":
		return artifact.NewGitHubSource(artifact.GitHubSourceConfig{
			Owner: cfg.Source.Owner,
			Repo:  cfg.Source.Repo,
			Tag:   cfg.Source.Tag,
			Token: cfg.Registry.Token,
		})
	case "go":
		return createGoSource(cfg)
	default:
		return nil, fmt.Errorf("unknown source type: %s", cfg.Source.Type)
	}
}

// createGoSource creates a GoSource for building from Go source
func createGoSource(cfg *config.Config) (artifact.Source, error) {
	// Get git info for ldflags template
	var commit, tag string
	if repo, err := git.Open("."); err == nil {
		// Try to get current commit
		if head, err := repo.GetRemoteURL(); err == nil {
			_ = head // We don't need the URL, just checking repo is valid
		}
		// Get the tag if available
		if t, err := repo.GetLatestTag(); err == nil && t != "" {
			tag = t
		} else if t, err := repo.GetLatestReachableTag(); err == nil && t != "" {
			tag = t
		}
	}

	return artifact.NewGoSource(artifact.GoSourceConfig{
		Packages:   cfg.Source.Build.Packages,
		Ldflags:    cfg.Source.Build.Ldflags,
		Env:        cfg.Source.Build.Env,
		CGOEnabled: cfg.Source.Build.CGOEnabled,
		Trimpath:   cfg.Source.Build.Trimpath,
		Flags:      cfg.Source.Build.Flags,
		ModDir:     cfg.Source.Build.ModDir,
		Parallel:   cfg.Source.Build.Parallel,
		Version:    cfg.Version,
		Commit:     commit,
		Tag:        tag,
		Binaries:   cfg.BinaryNames(),
	})
}

// updateTapFormula updates the tap formula with the new bottle block
func updateTapFormula(ctx context.Context, cfg *config.Config, bottles []*bottle.Bottle, dryRun bool) error {
	// Create tap updater
	updater := tap.NewUpdater(cfg.Tap.Token, cfg.Tap.Owner, cfg.Tap.Repo, cfg.Tap.Branch)

	// Build bottle specs for the formula
	var bottleSpecs []formula.BottleSpec
	rootURL := fmt.Sprintf("https://%s/v2/%s/%s", cfg.Registry.Host, cfg.Registry.Owner, cfg.Registry.Package)

	for _, b := range bottles {
		bottleSpecs = append(bottleSpecs, formula.BottleSpec{
			RootURL:  rootURL,
			Platform: b.Platform.Tag,
			SHA256:   b.SHA256,
			Cellar:   b.Cellar,
		})
	}

	// Generate bottle block
	bottleBlock := formula.GenerateBottleBlock(bottleSpecs)

	// Try to get existing formula
	existingContent, err := updater.GetFormula(ctx, cfg.Formula)
	var newContent string

	if err != nil {
		// No existing formula - generate a new one
		fmt.Printf("  Creating new formula: %s.rb\n", cfg.Formula)
		f := &formula.Formula{
			Name:        cfg.Formula,
			Description: cfg.Description,
			Homepage:    cfg.Homepage,
			License:     cfg.License,
			Bottles:     bottleSpecs,
			Binaries:    cfg.BinaryNames(),
		}
		newContent, err = formula.Generate(f)
		if err != nil {
			return fmt.Errorf("failed to generate formula: %w", err)
		}
	} else {
		// Update existing formula with new bottle block
		fmt.Printf("  Updating existing formula: %s.rb\n", cfg.Formula)
		newContent, err = formula.UpdateBottleBlock(string(existingContent), bottleBlock)
		if err != nil {
			// If no bottle block exists, try to insert one
			newContent, err = formula.InsertBottleBlock(string(existingContent), bottleBlock)
			if err != nil {
				return fmt.Errorf("failed to update bottle block: %w", err)
			}
		}
	}

	if dryRun {
		fmt.Println("  [DRY RUN] Would update formula with:")
		fmt.Println("  ---")
		// Show just the bottle block
		lines := strings.Split(newContent, "\n")
		inBottle := false
		for _, line := range lines {
			if strings.Contains(line, "bottle do") {
				inBottle = true
			}
			if inBottle {
				fmt.Printf("  %s\n", line)
			}
			if inBottle && strings.TrimSpace(line) == "end" {
				break
			}
		}
		fmt.Println("  ---")
		return nil
	}

	// Commit the formula update
	commitMsg := fmt.Sprintf("Update %s to %s", cfg.Formula, cfg.Version)
	if err := updater.UpdateFormula(ctx, cfg.Formula, []byte(newContent), commitMsg); err != nil {
		return fmt.Errorf("failed to commit formula: %w", err)
	}

	fmt.Printf("  Committed formula update to %s/%s\n", cfg.Tap.Owner, cfg.Tap.Repo)
	return nil
}

// BottleOutput represents the JSON output for the bottle command
type BottleOutput struct {
	Formula  string       `json:"formula"`
	Version  string       `json:"version"`
	Registry string       `json:"registry"`
	Bottles  []BottleInfo `json:"bottles"`
}

// BottleInfo represents a single bottle in JSON output
type BottleInfo struct {
	Platform string `json:"platform"`
	SHA256   string `json:"sha256"`
	Ref      string `json:"ref"`
	Cellar   string `json:"cellar"`
}

// outputJSON outputs the bottle information in JSON format
func outputJSON(bottles []*bottle.Bottle, imageRefs []string, cfg *config.Config) error {
	output := BottleOutput{
		Formula:  cfg.Formula,
		Version:  cfg.Version,
		Registry: fmt.Sprintf("%s/%s/%s", cfg.Registry.Host, cfg.Registry.Owner, cfg.Registry.Package),
		Bottles:  make([]BottleInfo, len(bottles)),
	}

	for i, b := range bottles {
		ref := ""
		if i < len(imageRefs) {
			ref = imageRefs[i]
		}
		output.Bottles[i] = BottleInfo{
			Platform: b.Platform.Tag,
			SHA256:   b.SHA256,
			Ref:      ref,
			Cellar:   b.Cellar,
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// buildConfig creates a Config from command options and viper
func buildConfig(opts *BottleOptions) *config.Config {
	cfg := &config.Config{
		Formula: opts.Formula,
		Version: opts.Version,
	}

	// Source config
	cfg.Source.Type = opts.Source
	cfg.Source.Owner = opts.Owner
	cfg.Source.Repo = opts.Repo
	cfg.Source.Tag = opts.Tag
	cfg.Source.DistPath = opts.DistPath

	// Go source build config
	cfg.Source.Build.Packages = opts.Packages
	cfg.Source.Build.Ldflags = opts.Ldflags
	cfg.Source.Build.CGOEnabled = opts.CGOEnabled
	cfg.Source.Build.Parallel = opts.Parallel
	cfg.Source.Build.Trimpath = true // Default to true for reproducible builds

	// Bottle config
	cfg.Bottle.Cellar = opts.Cellar
	cfg.Bottle.Platforms = opts.Platforms
	cfg.Bottle.ExcludePlatforms = opts.ExcludePlatforms
	cfg.Bottle.Rebuild = opts.Rebuild
	cfg.Bottle.RefreshPlatforms = opts.RefreshPlatforms

	// Registry config
	cfg.Registry.Host = opts.RegistryHost
	cfg.Registry.Owner = opts.RegistryOwner
	if cfg.Registry.Owner == "" {
		cfg.Registry.Owner = opts.Owner
	}
	cfg.Registry.Package = opts.Package
	cfg.Registry.Token = viper.GetString("registry.token")

	// Tap config
	cfg.Tap.Owner = opts.TapOwner
	if cfg.Tap.Owner == "" {
		cfg.Tap.Owner = opts.Owner
	}
	cfg.Tap.Repo = opts.TapRepo
	cfg.Tap.Branch = opts.TapBranch
	cfg.Tap.Token = viper.GetString("tap.token")

	// Binaries
	for _, name := range opts.Binaries {
		cfg.Binaries = append(cfg.Binaries, config.BinaryConfig{Name: name})
	}

	// Override with viper values if set via config file
	if v := viper.GetString("formula"); v != "" && cfg.Formula == "" {
		cfg.Formula = v
	}
	if v := viper.GetString("version"); v != "" && cfg.Version == "" {
		cfg.Version = v
	}
	if v := viper.GetString("description"); v != "" {
		cfg.Description = v
	}
	if v := viper.GetString("homepage"); v != "" {
		cfg.Homepage = v
	}
	if v := viper.GetString("license"); v != "" {
		cfg.License = v
	}

	return cfg
}
