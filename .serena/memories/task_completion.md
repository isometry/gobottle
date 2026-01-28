# gobottle - Task Completion Checklist

When completing a task in this project, follow these steps:

## 1. Build Verification
```bash
go build ./...
```
Ensure all packages compile without errors.

## 2. Run Tests
```bash
go test ./...
```
All tests must pass. If you modified code, ensure related tests still pass.

## 3. Format Code
```bash
go fmt ./...
```
Ensure code is properly formatted.

## 4. Run Vet
```bash
go vet ./...
```
Check for common mistakes.

## 5. Tidy Dependencies
If you added/removed imports:
```bash
go mod tidy
```

## 6. Design Constraints Check
- **No shell exec for git**: Verify no `exec.Command("git", ...)` calls were added
- **Semver validation**: Version auto-detection should only use valid semver tags
- **Error handling**: Errors should be wrapped with context

## 7. Test New Functionality
If you added new features:
- Add appropriate tests in `*_test.go` files
- Use table-driven tests where applicable
- Run the specific test: `go test ./path/to/package/... -v`

## Quick Verification Script
```bash
go build ./... && go test ./... && go vet ./... && echo "All checks passed"
```

## Before Committing
1. Ensure all the above checks pass
2. Review changes with `git diff`
3. Stage specific files (avoid `git add -A` for sensitive files)
4. Write a clear commit message describing the "why"
