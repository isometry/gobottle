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
3. **Publish**: Upload to GHCR as OCI image with bottle annotation
4. **Pour**: Download, extract, relocate placeholders to actual paths

## Bottle Filename Format

```
<formula>-<version>.<platform_tag>.bottle.<rebuild>.tar.gz
```

| Component | Description | Example |
|-----------|-------------|---------|
| `formula` | Formula name | `wget`, `gobottle` |
| `version` | Version string | `1.0.0`, `2.1.0_1` |
| `platform_tag` | OS/arch identifier | `arm64_sonoma`, `x86_64_linux` |
| `rebuild` | Rebuild counter (0 omitted) | `1`, `2` |

**Examples:**
```
gobottle-1.0.0.arm64_sonoma.bottle.tar.gz
gobottle-1.0.0.x86_64_linux.bottle.tar.gz
gobottle-1.0.0.arm64_sequoia.bottle.1.tar.gz  # rebuild 1
```

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

Bottles are stored in GitHub Container Registry as OCI images:

- **URL pattern**: `ghcr.io/v2/<owner>/<repo>/<formula>`
- **Key annotation**: `sh.brew.bottle.digest` contains bottle SHA256
- **Media type**: `application/vnd.oci.image.layer.v1.tar+gzip`
- **Auth token**: `QQ==` (base64 empty string) for public access

See `references/ghcr-publishing.md` for complete OCI manifest structure.

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
3. **Checksums critical** - SHA256 of bottle must match formula's bottle block
4. **Platform detection** - Use `runtime.GOOS` and `runtime.GOARCH` for current platform
5. **Reproducibility** - Honor `SOURCE_DATE_EPOCH`, use deterministic tar/gzip settings
