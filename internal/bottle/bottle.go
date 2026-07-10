package bottle

import (
	"fmt"
	"os"

	"github.com/isometry/gobottle/internal/platform"
)

// Bottle represents a built Homebrew bottle
type Bottle struct {
	Formula            string            // e.g., "myapp"
	Version            string            // e.g., "1.2.3"
	Platform           platform.Platform // The Homebrew platform
	SHA256             string            // SHA256 of the bottle tarball (formula sha256 == OCI layer digest)
	UncompressedSHA256 string            // SHA256 of the raw tar stream (OCI diff_id)
	UncompressedSize   int64             // raw tar size in bytes (installed_size basis)
	Path               string            // Local path to bottle file
	Binaries           []string          // Binaries included
	Cellar             string            // e.g., ":any_skip_relocation"
	Rebuild            int               // Rebuild number (usually 0)
	Tab                *Tab              // INSTALL_RECEIPT content (also pushed as sh.brew.tab)
}

// FileSize returns the size of the bottle tarball in bytes.
func (b *Bottle) FileSize() (int64, error) {
	fi, err := os.Stat(b.Path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// BottleName returns the local bottle filename, matching brew's convention:
// formula--version.platform.bottle[.rebuild].tar.gz
func (b *Bottle) BottleName() string {
	return bottleFilename(b.Formula, b.Version, b.Platform.Tag, b.Rebuild)
}

// VersionRebuild returns the OCI tag for this bottle's version:
// "version" or "version-rebuild" (GitHubPackages.version_rebuild).
func (b *Bottle) VersionRebuild() string {
	if b.Rebuild > 0 {
		return fmt.Sprintf("%s-%d", b.Version, b.Rebuild)
	}
	return b.Version
}

// RefName returns the org.opencontainers.image.ref.name annotation value:
// "version.platform_tag" or "version.platform_tag.rebuild".
func (b *Bottle) RefName() string {
	if b.Rebuild > 0 {
		return fmt.Sprintf("%s.%s.%d", b.Version, b.Platform.Tag, b.Rebuild)
	}
	return fmt.Sprintf("%s.%s", b.Version, b.Platform.Tag)
}

func bottleFilename(formula, version, tag string, rebuild int) string {
	suffix := ""
	if rebuild > 0 {
		suffix = fmt.Sprintf(".%d", rebuild)
	}
	return fmt.Sprintf("%s--%s.%s.bottle%s.tar.gz", formula, version, tag, suffix)
}
