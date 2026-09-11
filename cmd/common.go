package cmd

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

// resolveSourceDate returns the reproducible build timestamp:
// SOURCE_DATE_EPOCH > HEAD commit time > Unix epoch. Deriving the default
// from the commit keeps consecutive builds of the same commit bit-identical
// while still carrying a meaningful date.
func resolveSourceDate() time.Time {
	if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
		if secs, err := strconv.ParseInt(epoch, 10, 64); err == nil {
			return time.Unix(secs, 0).UTC()
		}
	}
	if repo, err := git.Open("."); err == nil {
		if t, err := repo.GetHeadCommitTime(); err == nil {
			return t.UTC()
		}
	}
	return time.Unix(0, 0).UTC()
}

// formulaBinaries maps the configured binaries onto the formula model.
func formulaBinaries(cfg *config.Config) []formula.BinaryInstall {
	out := make([]formula.BinaryInstall, len(cfg.Binaries))
	for i, b := range cfg.Binaries {
		out[i] = formula.BinaryInstall{Name: b.Name, InstallPath: b.InstallPath}
	}
	return out
}

// resolveReleaseCommit resolves the commit the released version was cut from,
// so the generated formula can bake it into the stable spec's ldflags:
// the release tag's commit > HEAD > "" (unknown, rendered as tap.user).
func resolveReleaseCommit(cfg *config.Config) string {
	repo, err := git.Open(".")
	if err != nil {
		return ""
	}
	tag := cfg.Source.Tag
	if tag == "" && cfg.Version != "" {
		tag = git.NormalizeVersion(cfg.Version)
	}
	if tag != "" {
		if commit, err := repo.GetCommitForTag(tag); err == nil && commit != "" {
			return commit
		}
	}
	if commit, err := repo.GetHeadCommit(); err == nil {
		return commit
	}
	return ""
}

// formulaSourceBuild derives the source-build install block from
// formula.build (which itself inherits from source.build), so that
// `brew install --build-from-source` and `--HEAD` compile the same recipe
// gobottle cross-compiles the bottles with.
//
// Returns nil — leaving the legacy bin.install block, which only works when
// pouring a bottle — when the block is disabled, or when the package↔binary
// mapping cannot be guessed for a multi-binary formula.
func formulaSourceBuild(cfg *config.Config, commit string, head bool) (*formula.SourceBuild, error) {
	fb := cfg.Formula.Build
	if !fb.IsEnabled() {
		return nil, nil
	}

	packages := fb.Packages
	if len(packages) == 0 {
		if len(cfg.Binaries) > 1 {
			warn("formula.build.packages is unset and %d binaries are configured - the generated formula falls back to bin.install and cannot build from source (no head stanza)", len(cfg.Binaries))
			return nil, nil
		}
		packages = []string{"."}
	}

	// Pair packages with binaries exactly as the cross-compiler does.
	names := artifact.PackageBinaries(packages, cfg.BinaryNames(), fb.ModDir)
	targets := make([]formula.GoTarget, len(packages))
	for i, pkg := range packages {
		installPath := "bin"
		if i < len(cfg.Binaries) && cfg.Binaries[i].InstallPath != "" {
			installPath = cfg.Binaries[i].InstallPath
		}
		targets[i] = formula.GoTarget{Package: pkg, Binary: names[i], InstallPath: installPath}
		if !hasMainPackage(fb.ModDir, pkg) {
			warn("formula.build.packages: %q contains no main package - set source.build.packages (e.g. ./cmd/%s) or the source build will fail", pkg, names[i])
		}
	}

	// CGO_ENABLED is derived from source.build so the source build matches
	// the bottle rather than picking up the user's environment.
	env := []formula.EnvVar{{Key: "CGO_ENABLED", Value: "0"}}
	if cfg.Source.Build.CGOEnabled {
		env[0].Value = "1"
	}
	for _, kv := range fb.Env {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			warn("ignoring malformed build env entry %q (want KEY=value)", kv)
			continue
		}
		env = append(env, formula.EnvVar{Key: key, Value: value})
	}

	ldflags, err := formula.RubyLdflags(fb.Ldflags, "commit")
	if err != nil {
		return nil, err
	}
	if len(ldflags) == 0 {
		warn("formula source build has no ldflags - version/commit will be empty for --build-from-source and --HEAD; set source.build.ldflags")
	}

	// std_go_args supplies -trimpath itself (and honours --debug-symbols), so
	// forwarding it would only duplicate it — the same reason RubyLdflags
	// drops bare -s/-w.
	var flags []string
	for _, f := range fb.Flags {
		if f != "-trimpath" {
			flags = append(flags, f)
		}
	}

	return &formula.SourceBuild{
		GoDependency: fb.Go,
		Env:          env,
		ModDir:       fb.ModDir,
		Ldflags:      ldflags,
		Commit:       commit,
		HeadCommit:   head,
		Tags:         fb.Tags,
		Flags:        flags,
		Targets:      targets,
	}, nil
}

// hasMainPackage reports whether the relative package pkg (resolved under
// modDir) contains a non-test Go file declaring `package main`. Packages
// given as import paths, and directories that cannot be read, are assumed
// fine: the check exists to catch the silent "." default pointing at a
// library root, not to replace the compiler.
func hasMainPackage(modDir, pkg string) bool {
	if pkg != "." && !strings.HasPrefix(pkg, "./") {
		return true
	}
	if modDir == "" {
		modDir = "."
	}
	dir := filepath.Join(modDir, pkg)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err == nil && f.Name.Name == "main" {
			return true
		}
	}
	return false
}

// formulaModel maps the formula config onto the generator model.
// The URL/SHA256 come from source config and may be empty at build time;
// commit is the release tag's commit baked into the source-build ldflags
// (empty when unknown).
func formulaModel(cfg *config.Config, commit string) (*formula.Formula, string, error) {
	f := cfg.Formula
	model := &formula.Formula{
		Name:         f.Name,
		Description:  f.Description,
		Homepage:     f.Homepage,
		URL:          cfg.SourceURL(),
		SHA256:       cfg.Source.SHA256,
		License:      f.License,
		Binaries:     formulaBinaries(cfg),
		Dependencies: f.Dependencies,
		Conflicts:    f.Conflicts,
		Caveats:      f.Caveats,
		Service:      f.Service,
		ExtraInstall: f.Install.Extra,
		Test:         formula.Test{Command: f.Test.Command, Raw: f.Test.Raw},
	}
	if f.Install.Completions {
		model.Completions = f.Install.CompletionsCommand
	}
	// Head defaults on when a git URL is derivable (resolved in SetDefaults);
	// an explicit `head: false` still suppresses the stanza.
	if f.Head != nil && *f.Head && cfg.GitURL() != "" {
		model.Head = &formula.Head{URL: cfg.GitURL(), Branch: f.HeadBranch}
	}

	// The head spec is only buildable if the install block knows how to
	// compile a git checkout, hence the build.head? branch on the commit —
	// and no head stanza at all when the block falls back to bin.install.
	build, err := formulaSourceBuild(cfg, commit, model.Head != nil)
	if err != nil {
		return nil, "", err
	}
	model.Build = build
	if build == nil && model.Head != nil {
		if !cfg.Formula.Build.IsEnabled() {
			warn("head stanza omitted: formula.build.enabled is false, so the install block cannot build a git checkout")
		}
		model.Head = nil
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
		Tags:       cfg.Source.Build.Tags,
		Env:        cfg.Source.Build.Env,
		CGOEnabled: cfg.Source.Build.CGOEnabled,
		Trimpath:   cfg.Source.Build.TrimpathEnabled(),
		Flags:      cfg.Source.Build.Flags,
		ModDir:     cfg.Source.Build.ModDir,
		Parallel:   cfg.Source.Build.Parallel,
		Version:    cfg.Version,
		Commit:     commit,
		Tag:        tag,
		Date:       resolveSourceDate(),
		Binaries:   cfg.BinaryNames(),
		Targets:    targets,
	})
}
