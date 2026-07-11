---
name: homebrew:bottles
description: This skill should be used when the user asks about "homebrew bottles", "bottle format", "bottle construction", "INSTALL_RECEIPT.json", "bottle metadata", "bottle tarball structure", "bottle relocation", "ghcr bottle publishing", "bottle platform tags", "create homebrew bottle", "publish bottle to GHCR", "build bottles in Go", or when implementing gobottle (a ko-like tool for Go binaries). Also use when discussing bottle lifecycle, cellar paths, bottle checksums, or OCI publishing for Homebrew packages.
version: 0.1.0
---

# Homebrew Bottle Expertise

Comprehensive knowledge for implementing gobottle - a ko-like tool for compiling and publishing Homebrew bottles for Go binaries in pure Go.

## What is a Bottle?

A bottle is Homebrew's pre-compiled binary package format. Instead of compiling from source, bottles provide ready-to-install binaries as gzipped tarballs (`.tar.gz`).

**Key characteristics:**
- Simple gzipped tarballs containing compiled binaries
- Built for specific OS versions and CPU architectures
- Include metadata about build environment and dependencies
- Support relocation to different installation prefixes

**When bottles are NOT used:**
- `--build-from-source` flag specified
- `HOMEBREW_BUILD_FROM_SOURCE=1` set
- No bottle exists for current OS/architecture
- Formula options were passed
- Bottle checksum missing or mismatched

## Bottle Lifecycle

```
Build → Package → Publish → Pour
```

1. **Build**: Compile with `--build-bottle` flag (sets `built_as_bottle: true`)
2. **Package**: Create tarball with binary, metadata, and `.brew/` directory
3. **Publish**: Upload to GHCR as an OCI index with bottle annotations
4. **Pour**: Fetch index by version tag, read tab annotation, fetch blob by
   the formula's sha256, extract, relocate placeholders if needed.
   **`def install` does NOT run when pouring** — anything the install block
   would produce (shell completions, man pages) must be generated at bottle
   build time and shipped inside the tarball.

## Bottle Filename Format

Local filenames (and brew's GHCR resolved basename, `Bottle::Filename#github_packages`)
use a **double dash** between formula and version:

```
<formula>--<version>.<platform_tag>.bottle[.<rebuild>].tar.gz
```

| Component | Description | Example |
|-----------|-------------|---------|
| `formula` | Formula name | `wget`, `gobottle` |
| `version` | Version string | `1.0.0`, `2.1.0_1` |
| `platform_tag` | OS/arch identifier | `arm64_sonoma`, `x86_64_linux` |
| `rebuild` | Rebuild counter (omitted when 0) | `.1`, `.2` |

**Examples:**
```
gobottle--1.0.0.arm64_sonoma.bottle.tar.gz
gobottle--1.0.0.x86_64_linux.bottle.tar.gz
gobottle--1.0.0.arm64_sequoia.bottle.1.tar.gz  # rebuild 1
```

(Single-dash `formula-version...` appears only in URL-encoded GitHub release
asset names, `Bottle::Filename#url_encode`.)

## Tarball Internal Structure

```
<formula>/<version>/
  .brew/
    <formula>.rb           # Formula definition (without bottle sha256)
    INSTALL_RECEIPT.json   # Required metadata (Tab)
  bin/                     # Binaries
  lib/                     # Libraries (if any)
  share/                   # Shared files (completions, man pages)
    bash-completion/
    zsh/
    fish/
    man/
```

For Go binaries, typically only `bin/` and optionally `share/` are needed.

## Platform Tags Quick Reference

| Tag | OS | Architecture |
|-----|-----|--------------|
| `arm64_tahoe` | macOS 26 (Future) | ARM64 |
| `arm64_sequoia` | macOS 15 | ARM64 |
| `arm64_sonoma` | macOS 14 | ARM64 |
| `arm64_ventura` | macOS 13 | ARM64 |
| `sequoia` | macOS 15 | x86_64 |
| `sonoma` | macOS 14 | x86_64 |
| `x86_64_linux` | Linux | x86_64 |
| `aarch64_linux` | Linux | ARM64 |

**Older-macOS fallback (verified in `extend/os/mac/utils/bottles.rb`):** when
no bottle matches the running macOS exactly, brew falls back to a bottle for
an **older-or-equal** macOS version of the same arch (`find_older_compatible_tag`).
So publishing ONE bottle per arch, tagged with the oldest supported macOS
version (e.g. `arm64_monterey`), covers every newer macOS. Linux tags match
exactly. There is no newer-to-older fallback, and `:all` is only for
platform-independent bottles (identical bytes on every platform — not
applicable when binaries differ per OS/arch).

## Default Cellar Paths

| Platform | HOMEBREW_CELLAR |
|----------|-----------------|
| macOS ARM64 | `/opt/homebrew/Cellar` |
| macOS x86_64 | `/usr/local/Cellar` |
| Linux | `/home/linuxbrew/.linuxbrew/Cellar` |

## Cellar Values for Bottles

| Value | Meaning |
|-------|---------|
| `:any` | Relocatable to any Cellar |
| `:any_skip_relocation` | Relocatable, skip relocation checks |
| `"/path/to/Cellar"` | Requires exact path match |

For Go binaries compiled with no CGO (`CGO_ENABLED=0`), use `:any_skip_relocation` since statically-linked Go binaries have no hardcoded paths.

## Relocation Placeholders

When bottles contain hardcoded paths, use these placeholders:
- `@@HOMEBREW_PREFIX@@`
- `@@HOMEBREW_CELLAR@@`
- `@@HOMEBREW_REPOSITORY@@`
- `@@HOMEBREW_LIBRARY@@`

**For Go binaries**: Static Go binaries (CGO_ENABLED=0) don't need relocation - they have no embedded paths. Use cellar `:any_skip_relocation`.

## INSTALL_RECEIPT.json Quick Reference

Required fields for Go binary bottles:

```json
{
  "homebrew_version": "4.4.0",
  "used_options": [],
  "unused_options": [],
  "built_as_bottle": true,
  "poured_from_bottle": false,
  "time": 1706000000,
  "source_modified_time": 1706000000,
  "compiler": "go",
  "stdlib": null,
  "runtime_dependencies": [],
  "arch": "arm64",
  "source": {
    "tap": "user/tap",
    "spec": "stable",
    "versions": {
      "stable": "1.0.0",
      "head": null,
      "version_scheme": 0
    }
  }
}
```

See `references/install-receipt.md` for complete schema.

## Reproducible Bottle Creation

For deterministic bottles:

```go
// Use SOURCE_DATE_EPOCH for timestamps
timestamp := os.Getenv("SOURCE_DATE_EPOCH")

// Tar settings
// --sort=name
// --mtime=@${SOURCE_DATE_EPOCH}
// --owner=0 --group=0 --numeric-owner
// --pax-option=exthdr.name=%d/PaxHeaders/%f,delete=atime,delete=ctime

// Gzip with -n (no embedded timestamp)
```

## GHCR Publishing Overview

Bottles are stored in GHCR as OCI images. The load-bearing contract (verified
against Homebrew source: `github_packages.rb`, `bottle.rb`, `resource.rb`,
`utils/bottles.rb`, and a live `brew install` from a scratch tap):

- **root_url NEVER contains the formula name.** Brew constructs
  `<root_url>/<formula>/manifests/...` and `<root_url>/<formula>/blobs/...`
  itself. Image path = `ghcr.io/<root_path>/<formula>`; formula's
  `root_url "https://ghcr.io/v2/<root_path>"`. Getting this wrong 404s every
  install. Formula name sanitized `@`→`/`, `+`→`x`; repo path lowercase.
- **Always push an OCI *index*, tagged `<version>[-<rebuild>]`** — even for a
  single platform. Brew parses the manifest response for a `manifests[]`
  array and errors "Missing 'manifests' section." on a bare image manifest.
- **Child descriptor annotations are load-bearing.** Brew selects the child
  where `sh.brew.bottle.digest` == the formula's bottle sha256 (bare hex, no
  `sha256:` prefix) AND `org.opencontainers.image.ref.name` ==
  `<version>.<platform_tag>[.<rebuild>]`, then **requires `sh.brew.tab`**
  (JSON tab) on it — install aborts with "Couldn't find tab from manifest."
  without it.
- **Layer blob = the bottle tar.gz byte-for-byte** (layer digest == formula
  sha256). Media type `application/vnd.oci.image.layer.v1.tar+gzip`.
- **Anonymous pour auth**: `Bearer QQ==` (base64 empty string). First push
  creates a **private** package — flip it public manually in the GitHub UI
  (the packages REST API cannot change visibility). Private taps still pour
  with `HOMEBREW_DOCKER_REGISTRY_TOKEN=$(gh auth token | base64)`.
- Link the package to a repo via `org.opencontainers.image.source` index
  annotation; also set `com.github.package.type=homebrew_bottle`.

### The pour flow (what `brew install` actually does)

1. GET `<root_url>/<formula>/manifests/<version>[-<rebuild>]` with
   `Accept: application/vnd.oci.image.index.v1+json`
2. Match a child by `sh.brew.bottle.digest` + `ref.name`; read `sh.brew.tab`
   for runtime dependencies
3. GET `<root_url>/<formula>/blobs/sha256:<formula bottle sha256>` and verify
4. Extract into the Cellar — **`def install` never runs on a pour**

See `references/ghcr-publishing.md` for complete manifest structure and a
working go-containerregistry implementation.

## gobottle Design Guidance (Ko-Inspired)

### Core Architecture Principles

Follow ko's patterns for gobottle:

1. **Separate compilation from packaging** - Build Go binary first, then create bottle tarball
2. **Publisher interface abstraction** - Pluggable destinations (GHCR, S3, local file)
3. **Multi-platform parallel builds** - Use errgroup for concurrent platform builds
4. **Layered configuration** - CLI flags > env vars > config file > defaults

### Suggested Interface Design

```go
// Builder compiles Go binaries
type Builder interface {
    Build(ctx context.Context, importPath string, platform Platform) (string, error)
}

// Packager creates bottle tarballs
type Packager interface {
    Package(ctx context.Context, binary string, opts PackageOptions) (*Bottle, error)
}

// Publisher uploads bottles
type Publisher interface {
    Publish(ctx context.Context, bottle *Bottle) (string, error)
}
```

### Configuration Model

```yaml
# .gobottle.yaml
formula: my-tool
version: "1.0.0"
tap: user/homebrew-tap

platforms:
  - darwin/amd64
  - darwin/arm64
  - linux/amd64
  - linux/arm64

build:
  main: ./cmd/my-tool
  ldflags:
    - -s -w
    - -X main.version={{.Version}}

publish:
  type: ghcr
  repo: ghcr.io/user/homebrew-bottles
```

See `references/ko-patterns.md` for detailed architectural patterns.

## Additional Resources

### Reference Files

For detailed technical specifications, consult:

- **`references/bottle-format.md`** - Complete tarball structure, naming conventions, reproducible build settings
- **`references/install-receipt.md`** - Full INSTALL_RECEIPT.json schema with all fields documented
- **`references/platform-tags.md`** - All platform tags, cellar paths, architecture mappings
- **`references/relocation.md`** - Placeholder system, binary patching process, when to skip
- **`references/ghcr-publishing.md`** - OCI manifest structure, authentication, upload process
- **`references/ko-patterns.md`** - Ko architectural patterns applied to bottle building

### When to Consult Each Reference

| Task | Reference File |
|------|----------------|
| Creating tarball structure | `bottle-format.md` |
| Generating metadata JSON | `install-receipt.md` |
| Cross-compilation targets | `platform-tags.md` |
| Handling embedded paths | `relocation.md` |
| Uploading to GHCR | `ghcr-publishing.md` |
| Designing gobottle architecture | `ko-patterns.md` |

## Key Implementation Notes

1. **Go binaries are simple** - No shared libraries, no relocation needed with CGO_ENABLED=0
2. **Formula file optional** - gobottle can generate minimal formula or user provides
3. **Checksums critical** - SHA256 of bottle must match formula's bottle block AND the OCI layer digest
4. **Platform detection** - Use `runtime.GOOS` and `runtime.GOARCH` for current platform
5. **Reproducibility** - Honor `SOURCE_DATE_EPOCH`, use deterministic tar/gzip settings.
   In Go: sort tar entries, zero uid/gid/uname/gname, fix ModTime, clear
   atime/ctime; `gzip.Writer` is already deterministic if `Name`/`ModTime`
   are left unset. Beware map iteration order and `-X main.date=<now>`
   ldflags — both silently break digest stability.

## Hard-Won Gotchas (from building and live-testing gobottle)

- **`brew audit` rejects `desc ""` / `license ""`** — omit empty stanzas
  entirely rather than rendering empty strings.
- **A `rebuild N` line is required in the bottle block** when rebuild > 0,
  and the rebuild appears in the OCI tag (`1.2.3-1`), the child `ref.name`
  (`1.2.3.arm64_sonoma.1`), and the filename (`.bottle.1.tar.gz`).
- **The `.brew/<formula>.rb` inside the bottle should be a real, loadable
  formula** (typically rendered without the bottle block): Formulary loads it
  for `brew info/uninstall` on installed kegs when the tap is unavailable.
- **Tab JSON**: `head` and `stdlib` must be `null` (not `""`); brew tabs are
  compact JSON. The same tab goes in `.brew/INSTALL_RECEIPT.json` and the
  `sh.brew.tab` annotation.
- **Homebrew ≥ 6 tap trust**: third-party taps may need `brew trust
  <user>/<repo>` (or `HOMEBREW_NO_REQUIRE_TAP_TRUST=1`) before their formulae
  evaluate.
- **The formula's stable `url`/`sha256` are never fetched during a pour** —
  only `brew audit`, `--build-from-source`, and humans touch them. A private
  source repo therefore doesn't block bottle installs.
- **go-containerregistry**: prefer a custom `v1.Layer` whose `Digest()` is
  the bottle file's sha256 and `DiffID()` the uncompressed-tar sha256 —
  guarantees the blob is byte-identical to the file. `mutate.Append` builds
  the config `rootfs.diff_ids` from `DiffID()`. Force OCI media types
  (`empty.Image` defaults to Docker types). `remote.WriteIndex` pushes
  children automatically; "keep-old" = `remote.Index` (existing) →
  `mutate.RemoveManifests` (replaced ref.names) → `mutate.AppendManifests`.
