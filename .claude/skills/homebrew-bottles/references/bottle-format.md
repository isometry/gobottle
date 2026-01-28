# Bottle Format Reference

Complete technical specification for Homebrew bottle tarball format.

## Filename Convention

### Pattern

```
<formula>-<version>.<platform_tag>.bottle.<rebuild>.tar.gz
```

### Components

| Component | Rules | Examples |
|-----------|-------|----------|
| `formula` | Lowercase, hyphens allowed, `@` for versioned | `wget`, `openssl@3`, `go` |
| `version` | Semver or custom, `_N` suffix for revisions | `1.0.0`, `3.1.0_1`, `2024.01` |
| `platform_tag` | See platform-tags.md | `arm64_sonoma`, `x86_64_linux` |
| `rebuild` | Integer, omitted if 0 | `.1`, `.2` |

### Local vs Upload Filename

Homebrew uses slightly different naming locally vs for upload:

| Context | Separator | Example |
|---------|-----------|---------|
| Local filename | Double dash `--` | `wget--2.12.arm64_sonoma.bottle.tar.gz` |
| Upload/URL | Single dash `-` | `wget-2.12.arm64_sonoma.bottle.tar.gz` |

**For gobottle**: Use single-dash format for both (simpler, works everywhere).

## Tarball Internal Structure

### Directory Layout

```
<formula>/
└── <version>/
    ├── .brew/
    │   ├── <formula>.rb           # Formula file (optional for custom taps)
    │   └── INSTALL_RECEIPT.json   # Required metadata
    ├── bin/
    │   └── <binary>               # Main executable
    ├── lib/                       # Libraries (if any)
    ├── include/                   # Headers (if any)
    ├── share/
    │   ├── bash-completion/
    │   │   └── completions/
    │   │       └── <formula>
    │   ├── zsh/
    │   │   └── site-functions/
    │   │       └── _<formula>
    │   ├── fish/
    │   │   └── vendor_completions.d/
    │   │       └── <formula>.fish
    │   └── man/
    │       └── man1/
    │           └── <formula>.1
    └── etc/                       # Config files (if any)
```

### Example for Go Binary

Minimal bottle for a Go CLI tool `gobottle`:

```
gobottle/
└── 1.0.0/
    ├── .brew/
    │   └── INSTALL_RECEIPT.json
    └── bin/
        └── gobottle
```

With shell completions:

```
gobottle/
└── 1.0.0/
    ├── .brew/
    │   └── INSTALL_RECEIPT.json
    ├── bin/
    │   └── gobottle
    └── share/
        ├── bash-completion/
        │   └── completions/
        │       └── gobottle
        ├── zsh/
        │   └── site-functions/
        │       └── _gobottle
        └── fish/
            └── vendor_completions.d/
                └── gobottle.fish
```

## The .brew Directory

### Contents

| File | Required | Purpose |
|------|----------|---------|
| `INSTALL_RECEIPT.json` | Yes | Installation metadata (Tab) |
| `<formula>.rb` | No* | Formula definition at build time |

*For homebrew-core, formula.rb is included. For custom taps/gobottle, it's optional.

### INSTALL_RECEIPT.json

See `install-receipt.md` for complete schema.

### Formula File (Optional)

If included, the formula file should NOT contain bottle checksums (those are added after bottle creation):

```ruby
class Gobottle < Formula
  desc "Build and publish Homebrew bottles for Go binaries"
  homepage "https://github.com/user/gobottle"
  url "https://github.com/user/gobottle/archive/v1.0.0.tar.gz"
  sha256 "abc123..."
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", "-o", bin/"gobottle", "./cmd/gobottle"
  end

  test do
    system bin/"gobottle", "--version"
  end
end
```

## Reproducible Tarball Creation

### GNU Tar Settings

For reproducible bottles, use these tar options:

```bash
tar \
  --sort=name \
  --mtime="@${SOURCE_DATE_EPOCH}" \
  --owner=0 \
  --group=0 \
  --numeric-owner \
  --pax-option=exthdr.name=%d/PaxHeaders/%f,delete=atime,delete=ctime \
  -cf archive.tar \
  <formula>/
```

### Gzip Settings

Use `-n` to omit embedded timestamp:

```bash
gzip -n archive.tar
```

### Go Implementation

```go
import (
    "archive/tar"
    "compress/gzip"
    "io"
    "os"
    "sort"
    "strconv"
    "time"
)

func CreateBottle(w io.Writer, formula, version string, files []FileEntry) error {
    // Get reproducible timestamp
    modTime := time.Unix(0, 0)
    if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
        if ts, err := strconv.ParseInt(epoch, 10, 64); err == nil {
            modTime = time.Unix(ts, 0)
        }
    }

    // Sort files for reproducibility
    sort.Slice(files, func(i, j int) bool {
        return files[i].Path < files[j].Path
    })

    // Create gzip writer (no timestamp in header)
    gw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
    if err != nil {
        return err
    }
    defer gw.Close()

    // Note: gzip.Writer doesn't expose Header.ModTime in Go stdlib
    // For true reproducibility, use a custom gzip implementation or
    // shell out to gzip -n

    tw := tar.NewWriter(gw)
    defer tw.Close()

    for _, f := range files {
        header := &tar.Header{
            Name:    fmt.Sprintf("%s/%s/%s", formula, version, f.Path),
            Size:    f.Size,
            Mode:    f.Mode,
            ModTime: modTime,
            Uid:     0,
            Gid:     0,
            Uname:   "",
            Gname:   "",
            Format:  tar.FormatPAX,
        }

        if err := tw.WriteHeader(header); err != nil {
            return err
        }

        if _, err := tw.Write(f.Content); err != nil {
            return err
        }
    }

    return nil
}
```

## File Permissions

### Standard Permissions

| File Type | Mode | Octal |
|-----------|------|-------|
| Executables (bin/) | rwxr-xr-x | 0755 |
| Regular files | rw-r--r-- | 0644 |
| Directories | rwxr-xr-x | 0755 |

### Go Implementation

```go
const (
    ModeExecutable = 0755
    ModeRegular    = 0644
    ModeDirectory  = 0755
)
```

## Tarball Size Considerations

### Typical Sizes

| Content | Approximate Size |
|---------|------------------|
| Single Go binary | 5-30 MB |
| + Shell completions | +10-50 KB |
| + Man pages | +5-20 KB |
| Gzip compression | ~30-40% of uncompressed |

### Compression Ratio

Go binaries compress well due to repeated patterns:
- Uncompressed: 15 MB
- Gzip default: ~6 MB (40%)
- Gzip best: ~5.5 MB (37%)

## Verification

### Validate Tarball Structure

```bash
# List contents
tar -tzf gobottle-1.0.0.arm64_sonoma.bottle.tar.gz

# Expected output pattern:
# gobottle/1.0.0/
# gobottle/1.0.0/.brew/
# gobottle/1.0.0/.brew/INSTALL_RECEIPT.json
# gobottle/1.0.0/bin/
# gobottle/1.0.0/bin/gobottle
```

### Compute SHA256

```bash
shasum -a 256 gobottle-1.0.0.arm64_sonoma.bottle.tar.gz
```

### Verify Reproducibility

Build twice and compare:

```bash
SOURCE_DATE_EPOCH=0 gobottle build --platform darwin/arm64
mv gobottle-1.0.0.arm64_sonoma.bottle.tar.gz bottle1.tar.gz

SOURCE_DATE_EPOCH=0 gobottle build --platform darwin/arm64
mv gobottle-1.0.0.arm64_sonoma.bottle.tar.gz bottle2.tar.gz

diff <(xxd bottle1.tar.gz) <(xxd bottle2.tar.gz)
# Should be identical
```

## Common Issues

### Issue: Different SHA256 on Each Build

**Cause**: Non-deterministic timestamps or file ordering

**Fix**:
- Set `SOURCE_DATE_EPOCH`
- Sort files before adding to tar
- Use `gzip -n`
- Set owner/group to 0

### Issue: Bottle Won't Pour

**Cause**: Missing or malformed INSTALL_RECEIPT.json

**Fix**: Ensure `.brew/INSTALL_RECEIPT.json` exists with valid content

### Issue: Wrong Platform Tag

**Cause**: Mismatched GOOS/GOARCH and platform tag

**Fix**: Map Go platform to Homebrew tag correctly:
- `darwin/arm64` → `arm64_sonoma` (or current macOS)
- `darwin/amd64` → `sonoma`
- `linux/amd64` → `x86_64_linux`
- `linux/arm64` → `aarch64_linux`
