package cmd

import (
	"fmt"
	"os"

	"github.com/isometry/gobottle/internal/artifact"
	"github.com/isometry/gobottle/internal/config"
	"github.com/isometry/gobottle/internal/formula"
	"github.com/isometry/gobottle/internal/git"
	"github.com/isometry/gobottle/internal/platform"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// bindFlags binds a verb's flags into viper (flag name -> config key).
// Called at RunE time — binding at construction would let the last-registered
// command's (unused) flag instances shadow the running command's values.
func bindFlags(cmd *cobra.Command, bindings map[string]string) error {
	for flagName, key := range bindings {
		flag := cmd.Flags().Lookup(flagName)
		if flag == nil {
			return fmt.Errorf("internal error: flag --%s not defined", flagName)
		}
		if err := viper.BindPFlag(key, flag); err != nil {
			return fmt.Errorf("failed to bind --%s: %w", flagName, err)
		}
	}
	return nil
}

// loadConfig realizes the full configuration: viper (flags > env > file) then
// git-derived defaults, then built-in defaults. A positional formula-name
// argument wins over everything.
func loadConfig(args []string) (*config.Config, error) {
	cfg, err := config.Load(viper.GetViper())
	if err != nil {
		return nil, err
	}
	if len(args) > 0 && args[0] != "" {
		cfg.Formula.Name = args[0]
	}
	cfg.ApplyGitDefaults()
	cfg.SetDefaults()
	return cfg, nil
}

// progress writes a human progress line to stderr (stdout is reserved for
// machine-readable output).
func progress(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// warn writes a warning to stderr.
func warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "WARNING: "+format+"\n", args...)
}

// formulaModel maps the formula config onto the generator model.
// The URL/SHA256 come from source config and may be empty at build time.
func formulaModel(cfg *config.Config) (*formula.Formula, string, error) {
	f := cfg.Formula
	model := &formula.Formula{
		Name:         f.Name,
		Description:  f.Description,
		Homepage:     f.Homepage,
		URL:          cfg.SourceURL(),
		SHA256:       cfg.Source.SHA256,
		License:      f.License,
		Binaries:     cfg.BinaryNames(),
		Dependencies: f.Dependencies,
		Conflicts:    f.Conflicts,
		Caveats:      f.Caveats,
		Service:      f.Service,
		Completions:  f.Install.Completions,
		ExtraInstall: f.Install.Extra,
		Test:         formula.Test{Command: f.Test.Command, Raw: f.Test.Raw},
	}
	if f.Head && cfg.GitURL() != "" {
		model.Head = &formula.Head{URL: cfg.GitURL(), Branch: f.HeadBranch}
	}

	tmpl := ""
	if f.Template != "" {
		data, err := os.ReadFile(f.Template)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read formula template %s: %w", f.Template, err)
		}
		tmpl = string(data)
	}

	return model, tmpl, nil
}

// buildTargets derives the GOOS/GOARCH combinations that can yield at least
// one requested bottle platform, so go-source builds skip filtered-out
// targets entirely.
func buildTargets(info *platform.PlatformInfo, cfg *config.Config) []artifact.BuildTarget {
	var targets []artifact.BuildTarget
	for _, t := range artifact.AllBuildTargets() {
		plats := info.GetPlatformsForOS(t.OS, t.Arch)
		plats = platform.FilterPlatforms(plats, cfg.Bottle.Platforms, cfg.Bottle.ExcludePlatforms)
		if len(plats) > 0 {
			targets = append(targets, t)
		}
	}
	return targets
}

// createArtifactSource creates the appropriate artifact source based on config
func createArtifactSource(cfg *config.Config, targets []artifact.BuildTarget) (artifact.Source, error) {
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
		return createGoSource(cfg, targets)
	default:
		return nil, fmt.Errorf("unknown source type: %s", cfg.Source.Type)
	}
}

// createGoSource creates a GoSource for building from Go source
func createGoSource(cfg *config.Config, targets []artifact.BuildTarget) (artifact.Source, error) {
	// Get git info for the ldflags template
	var commit, tag string
	if repo, err := git.Open("."); err == nil {
		if c, err := repo.GetHeadCommit(); err == nil {
			commit = c
		}
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
		Targets:    targets,
	})
}
