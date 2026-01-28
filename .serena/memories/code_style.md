# gobottle - Code Style and Conventions

## Go Version
- Go 1.25.5

## General Style
- Standard Go conventions (gofmt)
- No CGO (`CGO_ENABLED=0` in builds)
- Trimpath builds for reproducibility

## Package Organization
- `cmd/` for CLI commands (cobra pattern)
- `internal/` for private packages
- Flat package structure within internal (no deep nesting)

## Naming Conventions
- **Files**: lowercase, underscore for multi-word (`config_test.go`)
- **Packages**: short, lowercase, no underscores
- **Types**: PascalCase (`BottleOptions`, `Config`)
- **Functions**: PascalCase for exported, camelCase for private
- **Constants**: PascalCase or ALL_CAPS depending on context

## Error Handling
- Return errors with context: `fmt.Errorf("failed to X: %w", err)`
- Use wrapped errors for stack traces
- Validate early, fail fast

## Testing
- Test files alongside source: `foo.go` → `foo_test.go`
- Table-driven tests preferred
- Use `t.Run()` for subtests

## CLI Pattern (Cobra)
- Options structs for command flags (e.g., `BottleOptions`)
- `New*Command()` functions return `*cobra.Command`
- `run*()` functions implement command logic
- Viper for config file support

## Important Design Decisions
1. **No shell exec for git**: Use `go-git/go-git/v5` library instead of `exec.Command("git", ...)`
2. **Semver validation**: Only accept valid semver tags for version auto-detection
3. **Clean tree enforcement**: Default behavior requires clean git working tree (use `--snapshot` to override)

## Code Documentation
- Package-level doc comments on package declarations
- Function doc comments for exported functions
- Inline comments for complex logic only
