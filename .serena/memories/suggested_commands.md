# gobottle - Development Commands

## Build Commands

```bash
# Build the binary
go build -o gobottle .

# Build all packages (verify compilation)
go build ./...

# Build with version info (like GoReleaser does)
go build -ldflags "-s -w -X main.version=dev" -o gobottle .
```

## Test Commands

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test ./... -v

# Run tests for a specific package
go test ./internal/git/... -v
go test ./internal/config/... -v
go test ./cmd/... -v

# Run tests with coverage
go test ./... -cover

# Run a specific test
go test ./internal/git/... -run TestParseRemoteURL -v
```

## Lint and Format Commands

```bash
# Format code
go fmt ./...
gofmt -s -w .

# Run go vet
go vet ./...

# Tidy dependencies
go mod tidy
```

## GoReleaser Commands

```bash
# Build snapshot (local testing, no publish)
goreleaser build --snapshot --clean

# Full release (requires git tag)
goreleaser release --clean

# Check goreleaser config
goreleaser check
```

## Running gobottle

```bash
# Show help
./gobottle --help
./gobottle bottle --help

# Initialize config
./gobottle init

# Dry run bottle build
./gobottle bottle myformula --version 1.0.0 --owner myorg --dry-run

# Build bottles with snapshot (dirty tree allowed)
./gobottle bottle myformula --version 1.0.0 --owner myorg --snapshot

# Build and push bottles
export GITHUB_TOKEN=ghp_...
./gobottle bottle myformula --version 1.0.0 --owner myorg
```

## Git Commands (Darwin/macOS)

```bash
# Standard git commands work as expected
git status
git add .
git commit -m "message"
git tag v1.0.0
git push --tags
```

## Useful Checks

```bash
# Verify no exec.Command("git"...) calls remain
grep -rn 'exec\.Command.*"git"' internal/ cmd/

# Verify no os/exec imports in main code
grep -rn '"os/exec"' internal/ cmd/
```
