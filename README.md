# gobottle

> **Status: experimental.** Interfaces and behavior change without notice.

`gobottle` is [ko](https://ko.build) for Homebrew: it cross-compiles Go
binaries, packages them as real Homebrew **bottles**, publishes them to GHCR
using the same OCI conventions as homebrew-core, and generates the tap
formula — all in pure Go, from any platform, with no Ruby, no `skopeo`, and
no per-OS build runners.

Nothing else in the ecosystem does this: `goreleaser` generates formulae (now
casks) that build from source or download release tarballs; official bottles
require `brew bottle` + `brew pr-upload` running natively on every target
platform. Static Go binaries (`CGO_ENABLED=0`) don't need any of that — one
machine can bottle for every platform Homebrew supports.

## Pipeline

```sh
gobottle build   -o json > bottles.json   # cross-compile + package bottles
gobottle push    -o json < bottles.json   # publish OCI index + blobs to GHCR
gobottle release          < bottles.json  # generate formula, commit to tap
```

Each verb emits machine-readable JSON on stdout (progress on stderr), so the
stages compose in CI; `gobottle release --all` runs the whole pipeline in one
shot.

Configuration lives in `.gobottle.yaml` (see `gobottle init`); the formula is
fully generated — description, license, dependencies, caveats, completions,
test block and more are driven from the `formula:` section of the config,
with a Go-template escape hatch for anything not yet modeled.

## Requirements

- Go toolchain (for `--source=go` builds)
- A GitHub token with `write:packages` (GHCR) and `contents: write` (tap)
- After the first push, flip the GHCR package visibility to **public** so
  `brew`'s anonymous pulls work

## License

MIT — see [LICENSE](LICENSE).
