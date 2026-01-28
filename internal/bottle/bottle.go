package bottle

import (
	"fmt"

	"github.com/isometry/gobottle/internal/platform"
)

// Bottle represents a built Homebrew bottle
type Bottle struct {
	Formula  string            // e.g., "myapp"
	Version  string            // e.g., "1.2.3"
	Platform platform.Platform // The Homebrew platform
	SHA256   string            // SHA256 of bottle tarball
	Path     string            // Local path to bottle file
	Binaries []string          // Binaries included
	Cellar   string            // e.g., ":any_skip_relocation"
	Rebuild  int               // Rebuild number (usually 0)
}

// BottleName returns the filename: formula--version.platform.bottle.tar.gz
func (b *Bottle) BottleName() string {
	return fmt.Sprintf("%s--%s.%s.bottle.tar.gz", b.Formula, b.Version, b.Platform.Tag)
}
