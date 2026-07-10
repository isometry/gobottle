package artifact

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"text/template"
	"time"

	"golang.org/x/sync/errgroup"
)

// GoSourceConfig holds configuration for building Go binaries
type GoSourceConfig struct {
	// Packages to build (e.g., ["./cmd/myapp"])
	Packages []string

	// Ldflags template (supports {{.Version}}, {{.Commit}}, {{.Date}}, {{.Tag}})
	Ldflags string

	// Extra environment variables for go build, as KEY=value strings
	Env []string

	// Enable CGO (default: false for portable static binaries)
	CGOEnabled bool

	// Use -trimpath for reproducible builds (default: true)
	Trimpath bool

	// Extra go build flags
	Flags []string

	// Path to go.mod directory (default: ".")
	ModDir string

	// Number of parallel builds (default: GOMAXPROCS)
	Parallel int

	// Version string for ldflags template
	Version string

	// Git commit SHA for ldflags template
	Commit string

	// Git tag for ldflags template
	Tag string

	// Build date for ldflags template (default: now or SOURCE_DATE_EPOCH)
	Date time.Time

	// Binary names to use (defaults to package base name)
	Binaries []string

	// Targets restricts the GOOS/GOARCH combinations to build
	// (default: all Homebrew-supported targets)
	Targets []BuildTarget
}

// GoSource builds Go binaries and creates artifacts for bottle building
type GoSource struct {
	config    GoSourceConfig
	workDir   string
	artifacts []Artifact
	built     bool
}

// BuildTarget represents a single build target (OS/Arch combination)
type BuildTarget struct {
	OS   string
	Arch string
}

// AllBuildTargets returns all Homebrew-supported build targets
func AllBuildTargets() []BuildTarget {
	return []BuildTarget{
		{OS: "darwin", Arch: "arm64"},
		{OS: "darwin", Arch: "amd64"},
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm64"},
	}
}

// NewGoSource creates a new Go source for building binaries
func NewGoSource(cfg GoSourceConfig) (*GoSource, error) {
	// Validate go is available
	if _, err := exec.LookPath("go"); err != nil {
		return nil, fmt.Errorf("go command not found: %w\n  Hint: install Go from https://go.dev/dl/", err)
	}

	// Set defaults
	if cfg.ModDir == "" {
		cfg.ModDir = "."
	}
	if cfg.Parallel <= 0 {
		cfg.Parallel = runtime.GOMAXPROCS(0)
	}
	if cfg.Ldflags == "" {
		// Strip symbol tables by default, like GoReleaser
		cfg.Ldflags = "-s -w"
	}

	// Verify mod_dir exists and contains go.mod
	modPath := filepath.Join(cfg.ModDir, "go.mod")
	if _, err := os.Stat(modPath); err != nil {
		return nil, fmt.Errorf("go.mod not found at %s: %w", modPath, err)
	}

	// Set build date
	if cfg.Date.IsZero() {
		// Respect SOURCE_DATE_EPOCH for reproducible builds
		if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
			if secs, err := strconv.ParseInt(epoch, 10, 64); err == nil {
				cfg.Date = time.Unix(secs, 0).UTC()
			}
		}
		if cfg.Date.IsZero() {
			cfg.Date = time.Now().UTC()
		}
	}

	// Create work directory
	workDir, err := os.MkdirTemp("", "gobottle-go-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
	}

	return &GoSource{
		config:  cfg,
		workDir: workDir,
	}, nil
}

// List returns all available artifacts (builds them if not already built)
func (s *GoSource) List(ctx context.Context) ([]Artifact, error) {
	if s.built {
		return s.artifacts, nil
	}

	if err := s.build(ctx); err != nil {
		return nil, err
	}

	s.built = true
	return s.artifacts, nil
}

// build compiles Go binaries for all target platforms
func (s *GoSource) build(ctx context.Context) error {
	targets := s.config.Targets
	if len(targets) == 0 {
		targets = AllBuildTargets()
	}

	// Render ldflags template
	ldflags, err := s.renderLdflags()
	if err != nil {
		return fmt.Errorf("failed to render ldflags: %w", err)
	}

	// Use errgroup for parallel builds
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(s.config.Parallel)

	// Channel to collect artifacts
	artifactsChan := make(chan Artifact, len(targets)*len(s.config.Packages))

	for _, target := range targets {
		// capture for goroutine
		g.Go(func() error {
			artifact, err := s.buildTarget(ctx, target, ldflags)
			if err != nil {
				return fmt.Errorf("failed to build for %s/%s: %w", target.OS, target.Arch, err)
			}
			artifactsChan <- artifact
			return nil
		})
	}

	// Wait for all builds to complete
	if err := g.Wait(); err != nil {
		return err
	}
	close(artifactsChan)

	// Collect artifacts
	for artifact := range artifactsChan {
		s.artifacts = append(s.artifacts, artifact)
	}

	return nil
}

// buildTarget builds binaries for a single OS/Arch target
func (s *GoSource) buildTarget(ctx context.Context, target BuildTarget, ldflags string) (Artifact, error) {
	// Create target directory
	targetDir := filepath.Join(s.workDir, fmt.Sprintf("%s_%s", target.OS, target.Arch))
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return Artifact{}, fmt.Errorf("failed to create target directory: %w", err)
	}

	// Build each package
	var binaries []string
	for i, pkg := range s.config.Packages {
		// Determine binary name
		var binaryName string
		if i < len(s.config.Binaries) {
			binaryName = s.config.Binaries[i]
		} else {
			// Default to package base name
			binaryName = filepath.Base(pkg)
			if binaryName == "." || binaryName == "" {
				// If building current directory, use directory name
				absPath, err := filepath.Abs(filepath.Join(s.config.ModDir, pkg))
				if err == nil {
					binaryName = filepath.Base(absPath)
				} else {
					binaryName = "app"
				}
			}
		}

		outputPath := filepath.Join(targetDir, binaryName)
		binaries = append(binaries, binaryName)

		// Build the binary
		if err := s.goBuild(ctx, target, pkg, outputPath, ldflags); err != nil {
			return Artifact{}, err
		}
	}

	// Create tarball with binaries at root level
	tarballName := fmt.Sprintf("build_%s_%s.tar.gz", target.OS, target.Arch)
	tarballPath := filepath.Join(s.workDir, tarballName)

	if err := s.createTarball(tarballPath, targetDir, binaries); err != nil {
		return Artifact{}, fmt.Errorf("failed to create tarball: %w", err)
	}

	// Compute checksum
	checksum, err := computeFileSHA256(tarballPath)
	if err != nil {
		return Artifact{}, fmt.Errorf("failed to compute checksum: %w", err)
	}

	return Artifact{
		Name:     tarballName,
		Path:     tarballPath,
		OS:       target.OS,
		Arch:     target.Arch,
		Checksum: checksum,
	}, nil
}

// goBuild executes go build for a single target
func (s *GoSource) goBuild(ctx context.Context, target BuildTarget, pkg, outputPath, ldflags string) error {
	args := []string{"build"}

	// Add -trimpath for reproducible builds
	if s.config.Trimpath {
		args = append(args, "-trimpath")
	}

	// Add ldflags
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}

	// Add extra flags
	args = append(args, s.config.Flags...)

	// Add output path and package
	args = append(args, "-o", outputPath, pkg)

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = s.config.ModDir

	// Set environment
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("GOOS=%s", target.OS))
	cmd.Env = append(cmd.Env, fmt.Sprintf("GOARCH=%s", target.Arch))

	// CGO setting
	if s.config.CGOEnabled {
		cmd.Env = append(cmd.Env, "CGO_ENABLED=1")
	} else {
		cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	}

	// Add extra environment variables
	cmd.Env = append(cmd.Env, s.config.Env...)

	// Capture output for error messages
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return fmt.Errorf("go build failed for %s/%s: %s", target.OS, target.Arch, errMsg)
	}

	return nil
}

// renderLdflags renders the ldflags template with build variables
func (s *GoSource) renderLdflags() (string, error) {
	if s.config.Ldflags == "" {
		return "", nil
	}

	tmpl, err := template.New("ldflags").Parse(s.config.Ldflags)
	if err != nil {
		return "", fmt.Errorf("failed to parse ldflags template: %w", err)
	}

	data := struct {
		Version string
		Commit  string
		Date    string
		Tag     string
	}{
		Version: s.config.Version,
		Commit:  s.config.Commit,
		Date:    s.config.Date.Format(time.RFC3339),
		Tag:     s.config.Tag,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render ldflags template: %w", err)
	}

	return buf.String(), nil
}

// createTarball creates a tar.gz archive with binaries at root level
func (s *GoSource) createTarball(outputPath, sourceDir string, binaries []string) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	gzw := gzip.NewWriter(outFile)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	for _, binary := range binaries {
		srcPath := filepath.Join(sourceDir, binary)

		info, err := os.Stat(srcPath)
		if err != nil {
			return fmt.Errorf("failed to stat binary %s: %w", binary, err)
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header: %w", err)
		}

		// Binary at root level in tarball
		header.Name = binary

		// Set consistent timestamps for reproducibility
		if !s.config.Date.IsZero() {
			header.ModTime = s.config.Date
		}

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		file, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("failed to open binary: %w", err)
		}

		if _, err := io.Copy(tw, file); err != nil {
			file.Close()
			return fmt.Errorf("failed to write binary to tar: %w", err)
		}
		file.Close()
	}

	return nil
}

// Fetch copies an artifact to the target directory
func (s *GoSource) Fetch(ctx context.Context, artifact Artifact, targetDir string) (string, error) {
	// Ensure target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create target directory: %w", err)
	}

	// Target path
	targetPath := filepath.Join(targetDir, artifact.Name)

	// If source and target are the same, no need to copy
	if artifact.Path == targetPath {
		return targetPath, nil
	}

	// Open source file
	src, err := os.Open(artifact.Path)
	if err != nil {
		return "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer src.Close()

	// Create target file
	dst, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to create target file: %w", err)
	}
	defer dst.Close()

	// Copy and compute checksum simultaneously
	hash := sha256.New()
	writer := io.MultiWriter(dst, hash)

	if _, err := io.Copy(writer, src); err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	// Verify checksum if available
	if artifact.Checksum != "" {
		actualChecksum := fmt.Sprintf("%x", hash.Sum(nil))
		if err := ValidateChecksum(actualChecksum, artifact.Checksum); err != nil {
			os.Remove(targetPath)
			return "", err
		}
	}

	return targetPath, nil
}

// Close releases any resources
func (s *GoSource) Close() error {
	if s.workDir != "" {
		return os.RemoveAll(s.workDir)
	}
	return nil
}

// BinaryNames returns the names of binaries that will be produced
func (s *GoSource) BinaryNames() []string {
	if len(s.config.Binaries) > 0 {
		return s.config.Binaries
	}

	// Derive from package names
	var names []string
	for _, pkg := range s.config.Packages {
		name := filepath.Base(pkg)
		if name == "." || name == "" {
			absPath, err := filepath.Abs(filepath.Join(s.config.ModDir, pkg))
			if err == nil {
				name = filepath.Base(absPath)
			} else {
				name = "app"
			}
		}
		names = append(names, name)
	}
	return names
}
