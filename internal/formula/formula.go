package formula

import "strings"

// Formula is the complete model from which a Homebrew formula is generated.
// gobottle owns the formula outright: every publish renders it from this
// model; there is no in-place editing of hand-written formulae.
type Formula struct {
	Name        string // Formula name (e.g., "myapp")
	ClassName   string // PascalCase name for Ruby class (derived if empty)
	Description string // Short description (omitted if empty; brew audit wants it)
	Homepage    string // Project homepage URL
	URL         string // Source tarball URL
	SHA256      string // Source tarball SHA256 (omitted if empty)
	License     string // SPDX identifier (omitted if empty)
	Head        *Head  // Optional head stanza

	// Bottle block
	RootURL string       // e.g. "https://ghcr.io/v2/myorg/tap" (formula name appended by brew)
	Rebuild int          // rebuild counter; emitted as `rebuild N` when > 0
	Bottles []BottleSpec // one line per platform

	// Body
	Build        *SourceBuild    // source-build install block (nil: legacy bin.install)
	Dependencies []string        // depends_on lines
	Conflicts    []string        // conflicts_with lines
	Binaries     []BinaryInstall // install lines (first bin-installed binary drives the default test)
	Completions  []string        // completion-generating command rendered per bin-installed binary (empty: no line)
	ExtraInstall []string        // verbatim extra install lines
	Caveats      string          // literal caveats text
	Service      string          // verbatim service block body
	Test         Test            // test block (defaults to `system bin/"<bin>", "--version"`)
}

// BinaryInstall names a binary and the keg-relative directory it installs
// into (empty means "bin").
type BinaryInstall struct {
	Name        string
	InstallPath string
}

// BinBinaries returns the names of the binaries installed into bin; only
// these are runnable from PATH, so they drive the default test and
// completion generation.
func (f *Formula) BinBinaries() []string {
	var names []string
	for _, b := range f.Binaries {
		if b.InstallPath == "bin" {
			names = append(names, b.Name)
		}
	}
	return names
}

// SourceBuild describes the install block that compiles the formula from
// source, honouring `brew install --build-from-source` and `--HEAD`. Homebrew
// never runs def install when pouring a bottle, so this is the recipe for
// everyone who cannot (or chooses not to) use one.
type SourceBuild struct {
	GoDependency string   // "go" -> depends_on "go" => :build (empty: none)
	Env          []EnvVar // ENV["K"] = "V" assignments
	ModDir       string   // wrapped in `cd "<dir>" do … end` when set and != "."
	Ldflags      []string // %W[] tokens, already Ruby-interpolated
	Commit       string   // literal SHA of the release tag ("" -> tap.user)
	HeadCommit   bool     // emit the build.head? ternary for --HEAD builds
	Tags         []string // std_go_args(tags: [...])
	Flags        []string // extra go build flags, placed before the package
	Targets      []GoTarget
}

// EnvVar is one ENV["Key"] = "Value" assignment in the install block.
type EnvVar struct {
	Key   string
	Value string
}

// GoTarget pairs a Go package with the binary it produces and where that
// binary is installed, mirroring the package↔binary pairing the cross-compiler
// uses (see artifact.PackageBinaries).
type GoTarget struct {
	Package     string // "./cmd/foo"
	Binary      string // "foo"
	InstallPath string // "bin" | "libexec" | arbitrary keg-relative path
}

// WrapsModDir reports whether the build must run inside `cd "<dir>" do`.
func (b *SourceBuild) WrapsModDir() bool {
	return b.ModDir != "" && b.ModDir != "."
}

// Indent is the extra indentation applied to lines inside the cd block.
func (b *SourceBuild) Indent() string {
	if b.WrapsModDir() {
		return "  "
	}
	return ""
}

// CommitLocal renders the right-hand side of the `commit = …` local, or ""
// when the ldflags never reference it. --HEAD builds read the commit from the
// checkout; the stable spec bakes in the release tag's commit, falling back to
// the homebrew-core `tap.user` idiom when no commit is knowable.
func (b *SourceBuild) CommitLocal() string {
	if !b.usesCommit() {
		return ""
	}
	stable := "tap.user"
	if b.Commit != "" {
		stable = quoteRuby(b.Commit)
	}
	if b.HeadCommit {
		return "build.head? ? Utils.git_head(buildpath, safe: false) : " + stable
	}
	return stable
}

// LdflagLines groups the %W[] tokens onto rendered lines: a bare "-X" stays
// with the symbol assignment it introduces. %W[] splits on whitespace either
// way, so this is purely cosmetic — it just keeps the array readable.
func (b *SourceBuild) LdflagLines() []string {
	var lines []string
	for i := 0; i < len(b.Ldflags); i++ {
		if b.Ldflags[i] == "-X" && i+1 < len(b.Ldflags) {
			lines = append(lines, "-X "+b.Ldflags[i+1])
			i++
			continue
		}
		lines = append(lines, b.Ldflags[i])
	}
	return lines
}

// usesCommit reports whether the rendered ldflags interpolate the commit
// local. RubyLdflags names it "commit" (its default commitExpr), so this pair
// must be kept in step.
func (b *SourceBuild) usesCommit() bool {
	for _, tok := range b.Ldflags {
		if strings.Contains(tok, "#{commit}") {
			return true
		}
	}
	return false
}

// DependsOnArgs returns the rendered arguments of every depends_on line: the
// source build's Go toolchain first, then the configured runtime dependencies.
// They form a single group in the formula, unlike the historic one-blank-line-
// per-dependency layout.
func (f *Formula) DependsOnArgs() []string {
	var args []string
	if f.Build != nil && f.Build.GoDependency != "" {
		args = append(args, quoteRuby(f.Build.GoDependency)+" => :build")
	}
	for _, d := range f.Dependencies {
		args = append(args, quoteRuby(d))
	}
	return args
}

// StdGoArgs renders the keyword arguments passed to std_go_args for one
// target. It never passes -s/-w/-trimpath: std_go_args supplies those itself
// (and honours --debug-symbols while doing so).
func (f *Formula) StdGoArgs(t GoTarget) string {
	if f.Build == nil {
		return ""
	}
	var args []string
	if out := f.goOutput(t); out != "" {
		args = append(args, "output: "+out)
	}
	if len(f.Build.Ldflags) > 0 {
		args = append(args, "ldflags: ldflags")
	}
	if len(f.Build.Tags) > 0 {
		quoted := make([]string, len(f.Build.Tags))
		for i, tag := range f.Build.Tags {
			quoted[i] = quoteRuby(tag)
		}
		args = append(args, "tags: ["+strings.Join(quoted, ", ")+"]")
	}
	return strings.Join(args, ", ")
}

// goOutput renders the Ruby `output:` expression for a target, or "" when it
// already matches std_go_args' own default of bin/"<formula name>".
func (f *Formula) goOutput(t GoTarget) string {
	path := t.InstallPath
	if path == "" {
		path = "bin"
	}
	if f.Build != nil && len(f.Build.Targets) == 1 && path == "bin" && t.Binary == f.Name {
		return ""
	}
	switch path {
	case "bin":
		return "bin/" + quoteRuby(t.Binary)
	case "libexec":
		return "libexec/" + quoteRuby(t.Binary)
	default:
		return "prefix/" + quoteRuby(path+"/"+t.Binary)
	}
}

// BottleSpec represents one sha256 line in the bottle block.
type BottleSpec struct {
	Platform string // e.g. "arm64_sonoma", "x86_64_linux"
	SHA256   string // bottle tarball SHA256
	Cellar   string // e.g. ":any_skip_relocation" or an absolute path
}
