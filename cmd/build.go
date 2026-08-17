package cmd

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"

	"github.com/isometry/gobottle/internal/artifact"
	"github.com/isometry/gobottle/internal/bottle"
	"github.com/isometry/gobottle/internal/config"
	"github.com/isometry/gobottle/internal/formula"
	"github.com/isometry/gobottle/internal/platform"
	"github.com/isometry/gobottle/internal/util"
	"github.com/spf13/cobra"
)

// buildFlagBindings maps build-verb flags to config keys.
var buildFlagBindings = map[string]string{
	"formula":           "formula.name",
	"version":           "version",
	"source":            "source.type",
	"owner":             "source.owner",
	"repo":              "source.repo",
	"tag":               "source.tag",
	"dist":              "source.dist_path",
	"url":               "source.url",
	"sha256":            "source.sha256",
	"packages":          "source.build.packages",
	"ldflags":           "source.build.ldflags",
	"cgo":               "source.build.cgo_enabled",
	"parallel":          "source.build.parallel",
	"binaries":          "binaries",
	"platforms":         "bottle.platforms",
	"exclude-platforms": "bottle.exclude_platforms",
	"cellar":            "bottle.cellar",
	"rebuild":           "bottle.rebuild",
	"refresh-platforms": "bottle.refresh_platforms",
	"registry":          "registry.host",
	"registry-path":     "registry.root_path",
	"tap-owner":         "tap.owner",
	"tap-repo":          "tap.repo",
	"tap-branch":        "tap.branch",
}

// addBuildFlags defines the flags shared by `build` and one-shot `release`.
func addBuildFlags(cmd *cobra.Command) {
	cmd.Flags().String("formula", "", "formula name (also accepted as positional argument; default: repo name)")
	cmd.Flags().String("version", "", "version to bottle (default: latest reachable semver tag)")
	cmd.Flags().String("source", "local", "artifact source: 'local', 'go' (build from source), or 'github'")
	cmd.Flags().String("owner", "", "GitHub owner/org (default: derived from git remote)")
	cmd.Flags().String("repo", "", "GitHub repository name (default: derived from git remote)")
	cmd.Flags().String("tag", "", "release tag, e.g. v1.2.3 (for --source=github)")
	cmd.Flags().String("dist", "dist", "path to dist/ directory (for --source=local)")
	cmd.Flags().String("url", "", "source tarball URL (auto-derived from GitHub if not set)")
	cmd.Flags().String("sha256", "", "source tarball SHA256 (fetched and computed if not set)")
	cmd.Flags().StringSlice("packages", nil, "Go packages to build (for --source=go), e.g. ./cmd/myapp")
	cmd.Flags().String("ldflags", "", "ldflags template for go build ({{.Version}}, {{.Commit}}, {{.Date}}, {{.Tag}})")
	cmd.Flags().Bool("cgo", false, "enable CGO for go build")
	cmd.Flags().Int("parallel", 0, "number of parallel go builds (default: CPU count)")
	cmd.Flags().StringSlice("binaries", nil, "binary names to include (default: formula name)")
	cmd.Flags().StringSlice("platforms", nil, "platforms to bottle, e.g. arm64_sonoma (default: all supported)")
	cmd.Flags().StringSlice("exclude-platforms", nil, "platforms to exclude")
	cmd.Flags().String("cellar", "", "cellar setting: ':any', ':any_skip_relocation' (default), or absolute path")
	cmd.Flags().Int("rebuild", 0, "rebuild number (increment when re-bottling the same version)")
	cmd.Flags().Bool("refresh-platforms", false, "force refresh of the platform cache")
	cmd.Flags().String("registry", "", "OCI registry host (default: ghcr.io)")
	cmd.Flags().String("registry-path", "", "image path between host and formula name (default: <owner>/<tap minus homebrew->)")
	cmd.Flags().String("tap-owner", "", "tap owner (default: --owner)")
	cmd.Flags().String("tap-repo", "", "tap repository name (default: homebrew-tap)")
	cmd.Flags().String("tap-branch", "", "tap branch (default: the tap's default branch)")
}

// NewBuildCommand creates the `build` verb: compile/collect artifacts and
// package them as bottles, emitting a bottles.json manifest.
func NewBuildCommand(rootOpts *Options) *cobra.Command {
	var outputDir, output string

	cmd := &cobra.Command{
		Use:   "build [formula]",
		Short: "Build Homebrew bottles into a local directory",
		Long: `Build Homebrew bottles from Go source, a GoReleaser dist/ directory, or a
GitHub release, without pushing anything.

Bottles and a bottles.json manifest are written to --output-dir; the manifest
is the input to 'gobottle push' and 'gobottle release'.`,
		Example: `  # ko-like: cross-compile and bottle a Go package
  gobottle build --source go --packages .

  # bottle GoReleaser artifacts, emitting the manifest on stdout
  gobottle build mytool -o json > bottles.json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bindFlags(cmd, buildFlagBindings); err != nil {
				return err
			}
			cfg, err := loadConfig(args)
			if err != nil {
				return err
			}
			if err := cfg.ValidateFor(false); err != nil {
				return err
			}
			manifest, err := runBuild(cmd.Context(), cfg, outputDir)
			if err != nil {
				return err
			}
			if output == "json" {
				return emitManifest(manifest)
			}
			for _, e := range manifest.Bottles {
				fmt.Printf("%s\t%s\n", e.Platform, e.Path)
			}
			fmt.Printf("manifest\t%s\n", filepath.Join(outputDir, "bottles.json"))
			return nil
		},
	}

	addBuildFlags(cmd)
	cmd.Flags().StringVar(&outputDir, "output-dir", "bottles", "directory for bottles and manifest")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: 'text' or 'json'")

	return cmd
}

// bottleBinaries maps the configured binaries onto the bottle staging model.
func bottleBinaries(cfg *config.Config) []bottle.BinaryInstall {
	out := make([]bottle.BinaryInstall, len(cfg.Binaries))
	for i, b := range cfg.Binaries {
		out[i] = bottle.BinaryInstall{Name: b.Name, InstallPath: b.InstallPath}
	}
	return out
}

// findHostArtifact returns the artifact runnable on this machine, or nil.
// On Apple Silicon a darwin/amd64 artifact is an acceptable Rosetta fallback
// when no exact match exists.
func findHostArtifact(artifacts []artifact.Artifact) *artifact.Artifact {
	for i := range artifacts {
		if artifacts[i].OS == runtime.GOOS && artifacts[i].Arch == runtime.GOARCH {
			return &artifacts[i]
		}
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		for i := range artifacts {
			if artifacts[i].OS == "darwin" && artifacts[i].Arch == "amd64" {
				return &artifacts[i]
			}
		}
	}
	return nil
}

// generateCompletions runs the completion command of a host-runnable build of
// each bin-installed binary, returning keg-relative archive path -> local
// path entries (under workDir) for inclusion in every platform's bottle.
// Returns nil (with a warning) when no artifact can run on this machine.
func generateCompletions(ctx context.Context, cfg *config.Config, source artifact.Source, artifacts []artifact.Artifact, workDir string) (map[string]string, error) {
	host := findHostArtifact(artifacts)
	if host == nil {
		warn("no artifact runs on %s/%s - bottles will not include shell completions", runtime.GOOS, runtime.GOARCH)
		return nil, nil
	}

	archivePath, err := source.Fetch(ctx, *host, workDir)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch artifact %s for completions: %w", host.Name, err)
	}
	extractDir := filepath.Join(workDir, "extract")
	if info, err := os.Stat(archivePath); err == nil && info.IsDir() {
		extractDir = archivePath
	} else if err := util.ExtractArchive(archivePath, extractDir); err != nil {
		return nil, fmt.Errorf("failed to extract artifact for completions: %w", err)
	}

	files := map[string]string{}
	for _, b := range cfg.Binaries {
		if b.InstallPath != "bin" {
			continue
		}
		entries, err := bottle.GenerateCompletions(ctx, filepath.Join(extractDir, b.Name), cfg.Formula.Install.CompletionsCommand, workDir)
		if err != nil {
			return nil, err
		}
		maps.Copy(files, entries)
	}
	return files, nil
}

// runBuild executes the build stage and returns the manifest.
// Shared by `build` and one-shot `release`.
func runBuild(ctx context.Context, cfg *config.Config, outputDir string) (*Manifest, error) {
	progress("Building bottles for %s %s (source: %s)", cfg.Formula.Name, cfg.Version, cfg.Source.Type)

	// Discover platforms first: for go source this restricts which
	// GOOS/GOARCH targets get compiled at all.
	cacheDir, err := GetCacheDir()
	if err != nil {
		return nil, err
	}
	discoverer, err := platform.NewDiscoverer(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create platform discoverer: %w", err)
	}
	platformInfo, err := discoverer.Discover(ctx, cfg.Bottle.RefreshPlatforms)
	if err != nil {
		return nil, fmt.Errorf("failed to discover platforms: %w", err)
	}

	source, err := createArtifactSource(cfg, buildTargets(platformInfo, cfg))
	if err != nil {
		return nil, fmt.Errorf("failed to create artifact source: %w", err)
	}
	defer func() { _ = source.Close() }()

	artifacts, err := source.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list artifacts: %w", err)
	}
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("no artifacts found")
	}

	// The commit the released version was cut from: baked into the formula's
	// source-build ldflags and carried in the manifest so a later `release`
	// stage on another machine renders the same formula.
	releaseCommit := resolveReleaseCommit(cfg)

	// Render the formula (without bottle block) for embedding as
	// .brew/<formula>.rb. Best-effort: a source URL may not be derivable for
	// purely local builds.
	embeddedRb := ""
	if model, tmpl, err := formulaModel(cfg, releaseCommit); err == nil {
		if rb, err := formula.Generate(model, tmpl); err == nil {
			embeddedRb = rb
		}
	} else {
		return nil, err
	}

	builder, err := bottle.NewBuilder()
	if err != nil {
		return nil, fmt.Errorf("failed to create bottle builder: %w", err)
	}
	defer func() { _ = builder.Close() }()

	manifest := &Manifest{
		SchemaVersion: 1,
		Formula:       cfg.Formula.Name,
		Version:       cfg.Version,
		Rebuild:       cfg.Bottle.Rebuild,
		RootURL:       cfg.RootURL(),
		Description:   cfg.Formula.Description,
		Homepage:      cfg.Formula.Homepage,
		License:       cfg.Formula.License,
		Source: ManifestSource{
			Type:   cfg.Source.Type,
			URL:    cfg.SourceURL(),
			SHA256: cfg.Source.SHA256,
			Commit: releaseCommit,
			Tag:    cfg.Source.Tag,
		},
		Registry: ManifestReg{Host: cfg.Registry.Host, RootPath: cfg.Registry.RootPath},
		Tap: ManifestTap{
			Owner:       cfg.Tap.Owner,
			Repo:        cfg.Tap.Repo,
			Branch:      cfg.Tap.Branch,
			FormulaPath: cfg.Tap.FormulaPath,
		},
	}

	sourceDate := resolveSourceDate()

	// Completions are generated once from a host-runnable binary (cobra
	// output is platform-independent) and shipped in every bottle: brew
	// never runs def install when pouring.
	var extraFiles map[string]string
	if cfg.Formula.Install.Completions {
		completionsDir, err := os.MkdirTemp("", "gobottle-completions-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create completions directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(completionsDir) }()

		extraFiles, err = generateCompletions(ctx, cfg, source, artifacts, completionsDir)
		if err != nil {
			return nil, err
		}
	}

	for _, art := range artifacts {
		platforms := platformInfo.GetPlatformsForOS(art.OS, art.Arch)
		platforms = platform.FilterPlatforms(platforms, cfg.Bottle.Platforms, cfg.Bottle.ExcludePlatforms)
		if len(platforms) == 0 {
			progress("  skipping %s (no matching platforms)", art.Name)
			continue
		}

		tempDir, err := os.MkdirTemp("", "gobottle-artifact-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(tempDir) }()

		artPath, err := source.Fetch(ctx, art, tempDir)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch artifact %s: %w", art.Name, err)
		}

		for _, plat := range platforms {
			b, err := builder.Build(ctx, bottle.BuildOptions{
				Formula:      cfg.Formula.Name,
				Version:      cfg.Version,
				Platform:     plat,
				ArtifactPath: artPath,
				Binaries:     bottleBinaries(cfg),
				Cellar:       cfg.Bottle.Cellar,
				Rebuild:      cfg.Bottle.Rebuild,
				Tap:          fmt.Sprintf("%s/%s", cfg.Tap.Owner, cfg.Tap.Repo),
				FormulaRb:    embeddedRb,
				ExtraFiles:   extraFiles,
				SourceDate:   sourceDate,
				OutputDir:    outputDir,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to build bottle for %s: %w", plat.Tag, err)
			}
			progress("  built %s (%s)", b.BottleName(), b.SHA256[:12])

			entry, err := manifestEntry(b)
			if err != nil {
				return nil, err
			}
			manifest.Bottles = append(manifest.Bottles, entry)
		}
	}

	if len(manifest.Bottles) == 0 {
		return nil, fmt.Errorf("no bottles were built")
	}

	if err := writeManifestFile(manifest, filepath.Join(outputDir, "bottles.json")); err != nil {
		return nil, err
	}

	return manifest, nil
}
