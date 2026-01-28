# gobottle - Codebase Structure

```
gobottle/
├── main.go                 # Entry point, calls cmd.Execute()
├── go.mod / go.sum         # Go module definition
├── .goreleaser.yml         # GoReleaser configuration
│
├── cmd/                    # CLI commands (cobra)
│   ├── root.go             # Root command, global flags (--dry-run, --config)
│   ├── bottle.go           # Main `bottle` subcommand
│   ├── init.go             # `init` subcommand (generate .gobottle.yaml)
│   ├── platforms.go        # `platforms` subcommand
│   ├── version.go          # `version` subcommand
│   ├── completion.go       # Shell completion
│   └── *_test.go           # Command tests
│
├── internal/               # Internal packages
│   ├── artifact/           # Artifact sources (GitHub releases, local dist/)
│   │   ├── artifact.go     # Artifact interface and types
│   │   ├── github.go       # GitHub release source
│   │   └── local.go        # Local dist/ source
│   │
│   ├── bottle/             # Bottle building
│   │   ├── bottle.go       # Bottle struct
│   │   └── builder.go      # Bottle builder
│   │
│   ├── config/             # Configuration
│   │   ├── config.go       # Config struct, validation, git defaults
│   │   └── config_test.go  # Config tests
│   │
│   ├── formula/            # Homebrew formula handling
│   │   ├── formula.go      # Formula struct
│   │   ├── generator.go    # Formula generation
│   │   └── parser.go       # Formula parsing
│   │
│   ├── git/                # Git operations (go-git based)
│   │   ├── git.go          # Repository wrapper (Open, GetLatestTag, IsClean, etc.)
│   │   ├── semver.go       # Semver validation (IsValidSemver, ParseVersion)
│   │   ├── url.go          # URL parsing (ParseRemoteURL)
│   │   └── git_test.go     # Tests
│   │
│   ├── oci/                # OCI registry operations
│   │   ├── auth.go         # Authentication
│   │   └── pusher.go       # Push bottles to registry
│   │
│   ├── platform/           # Homebrew platform discovery
│   │   ├── platform.go     # Platform types
│   │   ├── discover.go     # Platform discovery from Homebrew source
│   │   └── cache.go        # Platform cache
│   │
│   ├── tap/                # Tap repository updates
│   │   └── updater.go      # Update formula in tap repo
│   │
│   └── util/               # Utilities
│       ├── archive.go      # Archive handling
│       └── hash.go         # Hashing utilities
│
└── testdata/               # Test fixtures
```

## Package Responsibilities

| Package | Responsibility |
|---------|---------------|
| `cmd` | CLI interface, flag parsing, command orchestration |
| `internal/artifact` | Fetch artifacts from GitHub or local filesystem |
| `internal/bottle` | Build bottle tarballs with correct structure |
| `internal/config` | Configuration loading, validation, git defaults |
| `internal/formula` | Parse and generate Homebrew formulas |
| `internal/git` | Git operations via go-git (no shell exec) |
| `internal/oci` | Push bottles to OCI registries (GHCR) |
| `internal/platform` | Discover Homebrew platforms |
| `internal/tap` | Update tap repository with new bottle info |
| `internal/util` | Shared utilities (archive, hash) |
