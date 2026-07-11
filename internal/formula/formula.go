package formula

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

// BottleSpec represents one sha256 line in the bottle block.
type BottleSpec struct {
	Platform string // e.g. "arm64_sonoma", "x86_64_linux"
	SHA256   string // bottle tarball SHA256
	Cellar   string // e.g. ":any_skip_relocation" or an absolute path
}
