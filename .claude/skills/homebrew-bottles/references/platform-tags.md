# Platform Tags Reference

Complete reference for Homebrew bottle platform tags and cellar paths.

## macOS Tags

### ARM64 (Apple Silicon)

| Tag | macOS Version | Codename | Status |
|-----|---------------|----------|--------|
| `arm64_tahoe` | macOS 26 | Tahoe | Future |
| `arm64_sequoia` | macOS 15 | Sequoia | Current |
| `arm64_sonoma` | macOS 14 | Sonoma | Supported |
| `arm64_ventura` | macOS 13 | Ventura | Supported |
| `arm64_monterey` | macOS 12 | Monterey | Legacy |
| `arm64_big_sur` | macOS 11 | Big Sur | Deprecated |

### x86_64 (Intel)

| Tag | macOS Version | Codename | Status |
|-----|---------------|----------|--------|
| `tahoe` | macOS 26 | Tahoe | Future |
| `sequoia` | macOS 15 | Sequoia | Current |
| `sonoma` | macOS 14 | Sonoma | Supported |
| `ventura` | macOS 13 | Ventura | Supported |
| `monterey` | macOS 12 | Monterey | Legacy |

**Note**: Intel tags omit the `x86_64_` prefix for historical reasons.

## Linux Tags

| Tag | Architecture | Description |
|-----|--------------|-------------|
| `x86_64_linux` | x86_64 | Intel/AMD 64-bit |
| `aarch64_linux` | ARM64 | ARM 64-bit (Raspberry Pi 4+, etc.) |

**Note**: Linux uses `aarch64` instead of `arm64` to match Linux conventions.

## Special Tags

| Tag | Description |
|-----|-------------|
| `all` | Universal bottle (architecture-independent, very rare) |

## Tag Selection for gobottle

### Mapping Go Platforms to Homebrew Tags

```go
func PlatformTag(goos, goarch string) (string, error) {
    switch goos {
    case "darwin":
        macosVersion := detectMacOSVersion()  // or use target version
        switch goarch {
        case "arm64":
            return "arm64_" + macosVersion, nil
        case "amd64":
            return macosVersion, nil
        }
    case "linux":
        switch goarch {
        case "amd64":
            return "x86_64_linux", nil
        case "arm64":
            return "aarch64_linux", nil
        }
    }
    return "", fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
}
```

### Detecting macOS Version

```go
import (
    "os/exec"
    "strings"
)

var macosCodenames = map[string]string{
    "15": "sequoia",
    "14": "sonoma",
    "13": "ventura",
    "12": "monterey",
    "11": "big_sur",
}

func detectMacOSVersion() string {
    out, err := exec.Command("sw_vers", "-productVersion").Output()
    if err != nil {
        return "sonoma" // safe default
    }

    version := strings.TrimSpace(string(out))
    parts := strings.Split(version, ".")
    if len(parts) >= 1 {
        if codename, ok := macosCodenames[parts[0]]; ok {
            return codename
        }
    }

    return "sonoma" // fallback
}
```

### Minimum Target Version Strategy

For maximum compatibility, target the oldest supported macOS version:

| Strategy | ARM64 Tag | x86_64 Tag | Pros | Cons |
|----------|-----------|------------|------|------|
| Current only | `arm64_sequoia` | `sequoia` | Simplest | Limited reach |
| Recent 2 | `arm64_sonoma` | `sonoma` | Good balance | Some older users excluded |
| All supported | `arm64_ventura` | `ventura` | Maximum reach | More bottles to build |

**Recommendation**: Build for `sonoma` and `sequoia` to cover most users.

## Default Cellar Paths

### macOS

| Platform | HOMEBREW_PREFIX | HOMEBREW_CELLAR |
|----------|-----------------|-----------------|
| ARM64 (Apple Silicon) | `/opt/homebrew` | `/opt/homebrew/Cellar` |
| x86_64 (Intel) | `/usr/local` | `/usr/local/Cellar` |

### Linux

| Platform | HOMEBREW_PREFIX | HOMEBREW_CELLAR |
|----------|-----------------|-----------------|
| x86_64 | `/home/linuxbrew/.linuxbrew` | `/home/linuxbrew/.linuxbrew/Cellar` |
| ARM64 | `/home/linuxbrew/.linuxbrew` | `/home/linuxbrew/.linuxbrew/Cellar` |

## Cellar Values in Formula

### Options

| Value | Meaning | Use Case |
|-------|---------|----------|
| `:any` | Relocatable to any Cellar | Bottles with only relative paths |
| `:any_skip_relocation` | Relocatable, skip checks | Static binaries (Go, Rust) |
| `"/opt/homebrew/Cellar"` | Requires exact path | Hardcoded paths that can't be relocated |
| (omitted) | Built in platform's default Cellar | Default behavior |

### For Go Binaries

**Always use `:any_skip_relocation`** because:
- Go binaries are statically linked (with `CGO_ENABLED=0`)
- No shared library paths embedded
- No Cellar paths in binary
- Relocation checks are unnecessary overhead

```ruby
bottle do
  sha256 cellar: :any_skip_relocation, arm64_sonoma: "abc123..."
  sha256 cellar: :any_skip_relocation, sonoma:       "def456..."
  sha256 cellar: :any_skip_relocation, x86_64_linux: "ghi789..."
end
```

## Go Implementation

### Platform Type

```go
type Platform struct {
    OS   string // darwin, linux
    Arch string // amd64, arm64
    Tag  string // Homebrew platform tag
}

// Common platforms for bottle building
var (
    DarwinARM64 = Platform{
        OS:   "darwin",
        Arch: "arm64",
        Tag:  "arm64_sonoma",
    }
    DarwinAMD64 = Platform{
        OS:   "darwin",
        Arch: "amd64",
        Tag:  "sonoma",
    }
    LinuxAMD64 = Platform{
        OS:   "linux",
        Arch: "amd64",
        Tag:  "x86_64_linux",
    }
    LinuxARM64 = Platform{
        OS:   "linux",
        Arch: "arm64",
        Tag:  "aarch64_linux",
    }
)
```

### All Supported Platforms

```go
var SupportedPlatforms = []Platform{
    // macOS ARM64
    {OS: "darwin", Arch: "arm64", Tag: "arm64_sequoia"},
    {OS: "darwin", Arch: "arm64", Tag: "arm64_sonoma"},
    {OS: "darwin", Arch: "arm64", Tag: "arm64_ventura"},

    // macOS x86_64
    {OS: "darwin", Arch: "amd64", Tag: "sequoia"},
    {OS: "darwin", Arch: "amd64", Tag: "sonoma"},
    {OS: "darwin", Arch: "amd64", Tag: "ventura"},

    // Linux
    {OS: "linux", Arch: "amd64", Tag: "x86_64_linux"},
    {OS: "linux", Arch: "arm64", Tag: "aarch64_linux"},
}
```

### Platform Parsing

```go
func ParsePlatform(s string) (Platform, error) {
    // Accept formats: "darwin/arm64", "arm64_sonoma", "linux-amd64"

    // Handle Go-style platform strings
    if strings.Contains(s, "/") {
        parts := strings.Split(s, "/")
        if len(parts) == 2 {
            return Platform{OS: parts[0], Arch: parts[1]}, nil
        }
    }

    // Handle Homebrew tags directly
    switch s {
    case "arm64_sonoma", "arm64_sequoia", "arm64_ventura":
        return Platform{OS: "darwin", Arch: "arm64", Tag: s}, nil
    case "sonoma", "sequoia", "ventura":
        return Platform{OS: "darwin", Arch: "amd64", Tag: s}, nil
    case "x86_64_linux":
        return Platform{OS: "linux", Arch: "amd64", Tag: s}, nil
    case "aarch64_linux":
        return Platform{OS: "linux", Arch: "arm64", Tag: s}, nil
    }

    return Platform{}, fmt.Errorf("unknown platform: %s", s)
}
```

## OCI Architecture Mapping

When publishing to GHCR as OCI images:

| Homebrew | OCI `os` | OCI `architecture` |
|----------|----------|-------------------|
| `arm64_*` | `darwin` | `arm64` |
| `sonoma`, `sequoia`, etc. | `darwin` | `amd64` |
| `x86_64_linux` | `linux` | `amd64` |
| `aarch64_linux` | `linux` | `arm64` |

```go
func (p Platform) OCIPlatform() v1.Platform {
    return v1.Platform{
        OS:           p.OS,
        Architecture: ociArch(p.Arch),
    }
}

func ociArch(goarch string) string {
    if goarch == "amd64" {
        return "amd64"
    }
    return goarch // arm64 stays arm64
}
```

## Cross-Compilation Matrix

### Recommended Build Matrix

For a typical Go CLI tool:

```yaml
# .gobottle.yaml
platforms:
  # Primary targets (most users)
  - darwin/arm64   # Apple Silicon Macs
  - darwin/amd64   # Intel Macs
  - linux/amd64    # Linux servers, CI

  # Secondary targets (optional)
  - linux/arm64    # ARM Linux, Raspberry Pi
```

### CI Matrix Example

```yaml
# GitHub Actions
jobs:
  build:
    strategy:
      matrix:
        include:
          - goos: darwin
            goarch: arm64
            tag: arm64_sonoma
          - goos: darwin
            goarch: amd64
            tag: sonoma
          - goos: linux
            goarch: amd64
            tag: x86_64_linux
          - goos: linux
            goarch: arm64
            tag: aarch64_linux
```

## Version Lifecycle

### Current Recommended Targets

| Priority | macOS | Tags |
|----------|-------|------|
| High | Sonoma (14), Sequoia (15) | `arm64_sonoma`, `arm64_sequoia`, `sonoma`, `sequoia` |
| Medium | Ventura (13) | `arm64_ventura`, `ventura` |
| Low | Monterey (12) | `arm64_monterey`, `monterey` |

### Deprecation Schedule

Apple typically supports 3 most recent macOS versions. When a new version releases:
1. Add new version tags
2. Consider dropping oldest supported version
3. Update default target version

**Example**: When macOS 16 releases, consider dropping Ventura (13) support.
