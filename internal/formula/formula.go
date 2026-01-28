package formula

// Formula represents a Homebrew formula
type Formula struct {
	Name        string       // Formula name (e.g., "myapp")
	ClassName   string       // PascalCase name for Ruby class (e.g., "MyApp")
	Description string       // Short description
	Homepage    string       // Project homepage URL
	URL         string       // Source tarball URL
	SHA256      string       // Source tarball SHA256
	License     string       // License identifier (e.g., "MIT")
	Bottles     []BottleSpec // Bottle specifications
	Binaries    []string     // Binary names to install
}

// BottleSpec represents a bottle entry in the formula
type BottleSpec struct {
	RootURL  string // e.g., "https://ghcr.io/v2/myorg/myapp"
	Platform string // e.g., "arm64_sonoma", "x86_64_linux"
	SHA256   string // Bottle SHA256
	Cellar   string // e.g., ":any_skip_relocation"
}
