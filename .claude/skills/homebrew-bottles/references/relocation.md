# Relocation Reference

How Homebrew handles path relocation in bottles.

## Overview

Relocation allows bottles built on one system to work on another with a different Homebrew prefix. This is achieved by:

1. Replacing hardcoded paths with placeholders at bottle creation time
2. Replacing placeholders with actual paths at installation (pour) time

**For Go binaries**: Static Go binaries (CGO_ENABLED=0) don't need relocation because they have no embedded paths. Use cellar `:any_skip_relocation`.

## Placeholder Constants

| Placeholder | Resolves To | Typical Value (ARM64 Mac) |
|-------------|-------------|---------------------------|
| `@@HOMEBREW_PREFIX@@` | `HOMEBREW_PREFIX` | `/opt/homebrew` |
| `@@HOMEBREW_CELLAR@@` | `HOMEBREW_CELLAR` | `/opt/homebrew/Cellar` |
| `@@HOMEBREW_REPOSITORY@@` | `HOMEBREW_REPOSITORY` | `/opt/homebrew` |
| `@@HOMEBREW_LIBRARY@@` | `HOMEBREW_LIBRARY` | `/opt/homebrew/Library` |

## When Relocation is Needed

### Needs Relocation

- Binaries with hardcoded library paths (rpath)
- Scripts with shebang lines pointing to Cellar
- Config files with absolute paths
- pkg-config (`.pc`) files
- CMake files with prefix paths

### Does NOT Need Relocation

- **Static Go binaries** (CGO_ENABLED=0)
- **Static Rust binaries**
- Pure data files
- Relative symlinks
- Documentation

## Cellar Values

| Value | Meaning | When to Use |
|-------|---------|-------------|
| `:any` | Full relocation at pour | Binaries with paths that CAN be relocated |
| `:any_skip_relocation` | Skip relocation entirely | Static binaries, no embedded paths |
| `"/opt/homebrew/Cellar"` | Exact path required | Cannot be relocated, path is hardcoded |
| (omitted) | Default Cellar for platform | Legacy behavior |

## Go Binary Recommendation

For gobottle (pure Go binaries):

```ruby
bottle do
  sha256 cellar: :any_skip_relocation, arm64_sonoma:  "..."
  sha256 cellar: :any_skip_relocation, sonoma:        "..."
  sha256 cellar: :any_skip_relocation, x86_64_linux:  "..."
end
```

**Why `:any_skip_relocation`**:
- Go binaries compiled with `CGO_ENABLED=0` are fully static
- No shared library dependencies
- No rpath entries
- No embedded Cellar paths
- Skipping relocation saves installation time

## Relocation Process (For Reference)

Even though gobottle doesn't need this, understanding the process helps:

### At Bottle Creation Time

1. Scan text files for Homebrew paths
2. Replace paths with placeholders
3. For binaries, use null-byte splitting to find path strings
4. Convert absolute symlinks to relative where possible

### At Pour (Installation) Time

1. Extract bottle tarball
2. If cellar is `:any`, call `replace_placeholders_with_locations()`
3. Replace `@@HOMEBREW_PREFIX@@` → actual prefix
4. Replace `@@HOMEBREW_CELLAR@@` → actual Cellar
5. Update symlinks if needed

## Text File Relocation

### Detection Regex

Homebrew uses this regex prefix to find paths in compiler flags:

```ruby
RELOCATABLE_PATH_REGEX_PREFIX = "(?:(?<=-F|-I|-L|-isystem)|(?![a-zA-Z0-9]))"
```

This matches paths preceded by:
- `-F` (framework path)
- `-I` (include path)
- `-L` (library path)
- `-isystem` (system include)
- Non-alphanumeric character

### Example Transformation

**Before (at build time)**:
```
prefix=/opt/homebrew/Cellar/mylib/1.0.0
exec_prefix=${prefix}
libdir=${exec_prefix}/lib
```

**After placeholder insertion**:
```
prefix=@@HOMEBREW_CELLAR@@/mylib/1.0.0
exec_prefix=${prefix}
libdir=${exec_prefix}/lib
```

**After pour (on different system)**:
```
prefix=/usr/local/Cellar/mylib/1.0.0
exec_prefix=${prefix}
libdir=${exec_prefix}/lib
```

## Binary Relocation

### How It Works

For compiled binaries with embedded paths, Homebrew:

1. Reads binary file
2. Splits on null bytes (C strings are null-terminated)
3. Searches for Homebrew path patterns
4. Replaces with placeholders (same length or padded)

### Limitations

- New path must be same length or shorter
- Longer paths require padding with null bytes
- Very long paths may not fit

### Example (Mach-O)

```
# Before
@rpath/opt/homebrew/Cellar/openssl@3/3.1.0/lib/libssl.dylib

# With placeholder (shorter, padded)
@rpath@@HOMEBREW_CELLAR@@/openssl@3/3.1.0/lib/libssl.dylib\0\0\0...
```

## Verifying No Relocation Needed

### Check Go Binary for Paths

```bash
# Look for Homebrew paths in binary
strings gobottle | grep -E "(homebrew|Cellar|/opt/|/usr/local)"

# Check for dynamic libraries
otool -L gobottle  # macOS
ldd gobottle       # Linux

# Expected output for static Go binary:
# gobottle:
#   (nothing - no dynamic dependencies)
```

### Verify Static Build

```bash
# Check if truly static
file gobottle

# Expected output:
# gobottle: Mach-O 64-bit executable arm64
# (no "dynamically linked" mention for static binary)
```

### Go Build Flags for Static Binary

```bash
CGO_ENABLED=0 go build -ldflags="-s -w" -o gobottle ./cmd/gobottle
```

The `-ldflags="-s -w"` strips debug info and DWARF symbols, making the binary smaller.

## When CGO is Enabled

If your Go binary uses CGO (`CGO_ENABLED=1`), it may link against system libraries:

```bash
# CGO-enabled binary might show:
otool -L mybinary
# mybinary:
#   /usr/lib/libSystem.B.dylib
#   /usr/lib/libresolv.9.dylib
```

In this case:
- Still probably safe for `:any_skip_relocation` on macOS (system libs are always present)
- May need `:any` if linking against Homebrew libraries

## Go Implementation

### Check if Relocation Needed

```go
// For gobottle, this should always return false for Go binaries
func NeedsRelocation(binaryPath string) (bool, error) {
    // Read binary
    data, err := os.ReadFile(binaryPath)
    if err != nil {
        return false, err
    }

    // Check for Homebrew paths
    homebrewPaths := []string{
        "/opt/homebrew",
        "/usr/local/Homebrew",
        "/home/linuxbrew",
        "@@HOMEBREW_",
    }

    for _, path := range homebrewPaths {
        if bytes.Contains(data, []byte(path)) {
            return true, nil
        }
    }

    return false, nil
}
```

### Recommended Cellar Value

```go
func RecommendedCellar(binaryPath string) string {
    needs, _ := NeedsRelocation(binaryPath)
    if needs {
        return ":any"
    }
    return ":any_skip_relocation"
}
```

## Formula DSL Reference

### Specifying Cellar

```ruby
bottle do
  # Skip relocation (recommended for Go)
  sha256 cellar: :any_skip_relocation, arm64_sonoma: "abc..."

  # Full relocation
  sha256 cellar: :any, arm64_sonoma: "def..."

  # Exact path required
  sha256 cellar: "/opt/homebrew/Cellar", arm64_sonoma: "ghi..."

  # Platform default (legacy)
  sha256 arm64_sonoma: "jkl..."
end
```

### pour_bottle? Block

Control when bottles are used:

```ruby
pour_bottle? do
  reason "Requires specific installation prefix"
  satisfy { HOMEBREW_PREFIX.to_s == "/opt/homebrew" }
end

# Or with preset
pour_bottle? only_if: :default_prefix
```

## Summary for gobottle

1. **Build Go binaries with CGO_ENABLED=0** for fully static binaries
2. **Use cellar `:any_skip_relocation`** in bottle block
3. **No relocation logic needed** in gobottle implementation
4. **Verify with `otool -L`** (macOS) or `ldd` (Linux) that binary is static
