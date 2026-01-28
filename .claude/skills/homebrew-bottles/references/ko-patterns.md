# Ko Architectural Patterns

Design patterns from ko (github.com/ko-build/ko) applied to Homebrew bottle building.

## Core Philosophy

Ko's key insight: **separate compilation from packaging**. This creates a clean pipeline:

```
Source Code → [Compiler] → Binary → [Packager] → Container Image → [Publisher] → Registry
```

For gobottle:

```
Go Source → [go build] → Binary → [Bottle Creator] → Tarball → [Publisher] → GHCR
```

## Pattern 1: Builder Interface

### Ko's Approach

```go
// Ko's build interface
type Interface interface {
    Build(context.Context, string) (Result, error)
}

type Result interface {
    // Abstract over image vs index
}
```

### Applied to gobottle

```go
// Builder compiles Go binaries for target platforms
type Builder interface {
    Build(ctx context.Context, opts BuildOptions) (*BuildResult, error)
}

type BuildOptions struct {
    ImportPath string   // e.g., "./cmd/mytool"
    Platform   Platform // e.g., darwin/arm64
    LDFlags    []string
    Tags       []string
    Trimpath   bool
}

type BuildResult struct {
    Binary   string   // Path to compiled binary
    Platform Platform // Target platform
}
```

### Implementation

```go
type GoBuilder struct {
    goPath string
}

func (b *GoBuilder) Build(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
    args := []string{"build"}

    if opts.Trimpath {
        args = append(args, "-trimpath")
    }

    if len(opts.LDFlags) > 0 {
        args = append(args, "-ldflags", strings.Join(opts.LDFlags, " "))
    }

    if len(opts.Tags) > 0 {
        args = append(args, "-tags", strings.Join(opts.Tags, ","))
    }

    outputPath := filepath.Join(os.TempDir(), "gobottle-build", opts.Platform.String())
    args = append(args, "-o", outputPath, opts.ImportPath)

    cmd := exec.CommandContext(ctx, "go", args...)
    cmd.Env = append(os.Environ(),
        "CGO_ENABLED=0",
        "GOOS="+opts.Platform.OS,
        "GOARCH="+opts.Platform.Arch,
    )

    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("go build failed: %w", err)
    }

    return &BuildResult{
        Binary:   outputPath,
        Platform: opts.Platform,
    }, nil
}
```

## Pattern 2: Packager Interface

### Ko's Approach

Ko creates OCI layers from binaries using functional composition:

```go
// Pseudo-code for ko's approach
baseImage := empty.Image
binaryLayer := createLayer(binary)
finalImage := mutate.AppendLayers(baseImage, binaryLayer)
```

### Applied to gobottle

```go
// Packager creates bottle tarballs from build results
type Packager interface {
    Package(ctx context.Context, result *BuildResult, opts PackageOptions) (*Bottle, error)
}

type PackageOptions struct {
    Formula     string            // Formula name
    Version     string            // Version string
    Tap         string            // Tap name
    Completions *CompletionFiles  // Optional shell completions
    ManPages    []string          // Optional man page paths
}

type Bottle struct {
    Path     string   // Path to .tar.gz file
    SHA256   string   // Checksum
    Platform Platform // Target platform
    Size     int64    // File size
}
```

### Implementation

```go
type TarballPackager struct{}

func (p *TarballPackager) Package(ctx context.Context, result *BuildResult, opts PackageOptions) (*Bottle, error) {
    // Create tarball structure
    bottlePath := fmt.Sprintf("%s-%s.%s.bottle.tar.gz",
        opts.Formula, opts.Version, result.Platform.Tag())

    f, err := os.Create(bottlePath)
    if err != nil {
        return nil, err
    }
    defer f.Close()

    gw := gzip.NewWriter(f)
    defer gw.Close()

    tw := tar.NewWriter(gw)
    defer tw.Close()

    // Add binary
    if err := addFile(tw, result.Binary,
        filepath.Join(opts.Formula, opts.Version, "bin", filepath.Base(result.Binary)),
        0755); err != nil {
        return nil, err
    }

    // Add INSTALL_RECEIPT.json
    tab := NewTab(opts.Formula, opts.Version, opts.Tap)
    tabJSON, _ := tab.JSON()
    if err := addContent(tw, tabJSON,
        filepath.Join(opts.Formula, opts.Version, ".brew", "INSTALL_RECEIPT.json"),
        0644); err != nil {
        return nil, err
    }

    // Add completions if provided
    if opts.Completions != nil {
        // ... add completion files
    }

    // Finalize
    tw.Close()
    gw.Close()
    f.Close()

    // Compute checksum
    sha256, size, err := checksumFile(bottlePath)
    if err != nil {
        return nil, err
    }

    return &Bottle{
        Path:     bottlePath,
        SHA256:   sha256,
        Platform: result.Platform,
        Size:     size,
    }, nil
}
```

## Pattern 3: Publisher Interface

### Ko's Approach

Ko defines publishers as a pluggable abstraction:

```go
type Interface interface {
    Publish(context.Context, build.Result, string) (name.Reference, error)
    Close() error
}
```

Ko ships multiple implementations: remote registry, local daemon, tarball, kind.

### Applied to gobottle

```go
// Publisher uploads bottles to a destination
type Publisher interface {
    Publish(ctx context.Context, bottle *Bottle) (string, error) // Returns URL
    Close() error
}

// Multiple implementations
type GHCRPublisher struct { ... }      // GitHub Container Registry
type S3Publisher struct { ... }         // S3-compatible storage
type GitHubReleasesPublisher struct { } // GitHub Releases
type LocalPublisher struct { ... }      // Local directory
```

### GHCR Implementation

```go
type GHCRPublisher struct {
    repo   string
    auth   authn.Authenticator
    client *remote.Pusher
}

func NewGHCRPublisher(repo, token string) *GHCRPublisher {
    return &GHCRPublisher{
        repo: repo,
        auth: authn.FromConfig(authn.AuthConfig{
            Username: "token",
            Password: token,
        }),
    }
}

func (p *GHCRPublisher) Publish(ctx context.Context, bottle *Bottle) (string, error) {
    ref, err := name.ParseReference(fmt.Sprintf("%s:%s.%s",
        p.repo, bottle.Version, bottle.Platform.Tag()))
    if err != nil {
        return "", err
    }

    // Create OCI image with bottle as layer
    layer, err := tarball.LayerFromFile(bottle.Path)
    if err != nil {
        return "", err
    }

    img, err := mutate.AppendLayers(empty.Image, layer)
    if err != nil {
        return "", err
    }

    img = mutate.Annotations(img, map[string]string{
        "sh.brew.bottle.digest": "sha256:" + bottle.SHA256,
    }).(v1.Image)

    if err := remote.Write(ref, img, remote.WithAuth(p.auth)); err != nil {
        return "", err
    }

    return ref.String(), nil
}
```

## Pattern 4: Multi-Platform Parallel Builds

### Ko's Approach

Ko uses `errgroup` for concurrent platform builds:

```go
func (g *gobuild) buildAll(ctx context.Context, platforms []Platform) (Result, error) {
    eg, ctx := errgroup.WithContext(ctx)

    images := make([]v1.Image, len(platforms))
    for i, platform := range platforms {
        i, platform := i, platform // capture
        eg.Go(func() error {
            img, err := g.buildOne(ctx, platform)
            if err != nil {
                return err
            }
            images[i] = img
            return nil
        })
    }

    if err := eg.Wait(); err != nil {
        return nil, err
    }

    return combineIntoIndex(images)
}
```

### Applied to gobottle

```go
func (g *Gobottle) BuildAll(ctx context.Context, platforms []Platform) ([]*Bottle, error) {
    eg, ctx := errgroup.WithContext(ctx)

    bottles := make([]*Bottle, len(platforms))
    for i, platform := range platforms {
        i, platform := i, platform
        eg.Go(func() error {
            result, err := g.builder.Build(ctx, BuildOptions{
                ImportPath: g.config.Main,
                Platform:   platform,
                LDFlags:    g.config.LDFlags,
            })
            if err != nil {
                return err
            }

            bottle, err := g.packager.Package(ctx, result, g.packageOpts)
            if err != nil {
                return err
            }

            bottles[i] = bottle
            return nil
        })
    }

    if err := eg.Wait(); err != nil {
        return nil, err
    }

    return bottles, nil
}
```

## Pattern 5: Layered Configuration

### Ko's Configuration Hierarchy

Ko uses precedence: CLI flags > environment > config file > defaults

```go
// From ko
type options struct {
    // CLI flags take precedence
    BaseImage string // --base-image flag

    // Then environment
    // KO_DOCKER_REPO, KO_DEFAULTBASEIMAGE

    // Then config file
    // .ko.yaml

    // Then defaults
}
```

### Applied to gobottle

```go
type Config struct {
    Formula   string     `yaml:"formula"`
    Version   string     `yaml:"version"`
    Tap       string     `yaml:"tap"`
    Main      string     `yaml:"main"`
    Platforms []string   `yaml:"platforms"`
    LDFlags   []string   `yaml:"ldflags"`
    Publish   PublishConfig `yaml:"publish"`
}

func LoadConfig() (*Config, error) {
    cfg := &Config{
        // Defaults
        Main:      "./cmd/...",
        Platforms: []string{"darwin/arm64", "darwin/amd64", "linux/amd64"},
    }

    // Load from .gobottle.yaml
    if data, err := os.ReadFile(".gobottle.yaml"); err == nil {
        if err := yaml.Unmarshal(data, cfg); err != nil {
            return nil, err
        }
    }

    // Override from environment
    if repo := os.Getenv("GOBOTTLE_REPO"); repo != "" {
        cfg.Publish.Repo = repo
    }
    if platforms := os.Getenv("GOBOTTLE_PLATFORMS"); platforms != "" {
        cfg.Platforms = strings.Split(platforms, ",")
    }

    return cfg, nil
}
```

### Config File Example

```yaml
# .gobottle.yaml
formula: mytool
version: "1.0.0"
tap: myuser/homebrew-tap

main: ./cmd/mytool

platforms:
  - darwin/arm64
  - darwin/amd64
  - linux/amd64

ldflags:
  - -s -w
  - -X main.version={{.Version}}
  - -X main.commit={{.Commit}}

publish:
  type: ghcr
  repo: ghcr.io/myuser/homebrew-bottles
```

## Pattern 6: Template Substitution

### Ko's Approach

Ko supports Go templates in ldflags:

```yaml
# .ko.yaml
builds:
- ldflags:
  - -X main.version={{.Env.VERSION}}
```

### Applied to gobottle

```go
type TemplateData struct {
    Version string
    Commit  string
    Date    string
    Formula string
}

func (c *Config) ExpandTemplates() error {
    data := TemplateData{
        Version: c.Version,
        Commit:  getGitCommit(),
        Date:    time.Now().Format(time.RFC3339),
        Formula: c.Formula,
    }

    for i, flag := range c.LDFlags {
        tmpl, err := template.New("flag").Parse(flag)
        if err != nil {
            return err
        }

        var buf bytes.Buffer
        if err := tmpl.Execute(&buf, data); err != nil {
            return err
        }

        c.LDFlags[i] = buf.String()
    }

    return nil
}
```

## Pattern 7: Reproducible Builds

### Ko's Approach

Ko respects `SOURCE_DATE_EPOCH` for reproducibility:

```go
// Ko checks SOURCE_DATE_EPOCH for layer timestamps
if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
    // Use epoch for file modification times
}
```

### Applied to gobottle

```go
func getReproducibleTime() time.Time {
    if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
        if ts, err := strconv.ParseInt(epoch, 10, 64); err == nil {
            return time.Unix(ts, 0)
        }
    }
    return time.Now()
}

func (p *TarballPackager) addFile(tw *tar.Writer, path, name string, mode int64) error {
    info, err := os.Stat(path)
    if err != nil {
        return err
    }

    header := &tar.Header{
        Name:    name,
        Size:    info.Size(),
        Mode:    mode,
        ModTime: getReproducibleTime(), // Reproducible timestamp
        Uid:     0,
        Gid:     0,
        Format:  tar.FormatPAX,
    }

    // ... write header and content
}
```

## Complete Pipeline Example

```go
func main() {
    ctx := context.Background()

    // Load configuration (Pattern 5)
    cfg, err := LoadConfig()
    if err != nil {
        log.Fatal(err)
    }

    // Expand templates (Pattern 6)
    if err := cfg.ExpandTemplates(); err != nil {
        log.Fatal(err)
    }

    // Create components (Patterns 1, 2, 3)
    builder := &GoBuilder{}
    packager := &TarballPackager{}
    publisher := NewGHCRPublisher(cfg.Publish.Repo, os.Getenv("GITHUB_TOKEN"))

    // Parse platforms
    platforms := make([]Platform, len(cfg.Platforms))
    for i, p := range cfg.Platforms {
        platforms[i], _ = ParsePlatform(p)
    }

    // Build all platforms in parallel (Pattern 4)
    gobottle := &Gobottle{
        config:   cfg,
        builder:  builder,
        packager: packager,
    }

    bottles, err := gobottle.BuildAll(ctx, platforms)
    if err != nil {
        log.Fatal(err)
    }

    // Publish all bottles
    for _, bottle := range bottles {
        url, err := publisher.Publish(ctx, bottle)
        if err != nil {
            log.Fatal(err)
        }
        fmt.Printf("Published: %s (sha256:%s)\n", url, bottle.SHA256)
    }

    // Output formula bottle block
    fmt.Println("\nbottle do")
    fmt.Printf("  root_url \"%s\"\n", cfg.Publish.Repo)
    for _, b := range bottles {
        fmt.Printf("  sha256 cellar: :any_skip_relocation, %s: \"%s\"\n",
            b.Platform.Tag(), b.SHA256)
    }
    fmt.Println("end")
}
```

## Summary

| Ko Pattern | gobottle Application |
|------------|---------------------|
| Builder interface | Compile Go binaries for platforms |
| Packager interface | Create bottle tarballs |
| Publisher interface | Upload to GHCR/S3/local |
| Parallel builds | Use errgroup for platforms |
| Layered config | CLI > env > .gobottle.yaml > defaults |
| Template substitution | Version/commit in ldflags |
| Reproducible builds | SOURCE_DATE_EPOCH timestamps |
