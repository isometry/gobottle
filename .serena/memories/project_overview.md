# gobottle - Project Overview

## Purpose
gobottle is a CLI tool that builds and publishes Homebrew bottles from GoReleaser artifacts. It's essentially a "ko-like" tool for Go binaries targeting Homebrew distribution.

## What It Does
1. Fetches artifacts from GitHub Releases or local `dist/` directory
2. Discovers supported Homebrew platforms from Homebrew source
3. Builds bottles (tar.gz archives) for each platform
4. Pushes bottles to GHCR (GitHub Container Registry) as OCI artifacts
5. Updates tap formula with the bottle block

## Tech Stack
- **Language**: Go 1.25.5
- **CLI Framework**: cobra + viper
- **Git Operations**: go-git/go-git/v5 (pure Go, no exec)
- **OCI/Registry**: google/go-containerregistry
- **GitHub API**: google/go-github/v68
- **Semver Parsing**: Masterminds/semver/v3
- **Build Tool**: GoReleaser

## Key Dependencies
- `github.com/spf13/cobra` - CLI commands
- `github.com/spf13/viper` - Configuration
- `github.com/go-git/go-git/v5` - Git operations (no shell exec)
- `github.com/google/go-containerregistry` - OCI image/registry operations
- `github.com/google/go-github/v68` - GitHub API client
- `github.com/Masterminds/semver/v3` - Semantic version parsing

## Entry Point
- `main.go` → `cmd.Execute()` → `cmd.NewRootCommand()`
