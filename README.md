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
gobottle release -i bottles.json          # generate formula, commit to tap
```

Each verb emits machine-readable JSON on stdout (progress on stderr), so the
stages compose in CI; `gobottle release` without `-i` runs the whole pipeline
in one shot.

Configuration lives in `.gobottle.yaml` (see `gobottle init`); the formula is
fully generated — description, license, dependencies, caveats, completions,
test block and more are driven from the `formula:` section of the config,
with a Go-template escape hatch for anything not yet modeled.

The generated formula also builds from source: its `def install` emits
`depends_on "go" => :build` and `system "go", "build", *std_go_args(...)`
with the ldflags inherited from `source.build`, so
`brew install --build-from-source` and `brew install --HEAD` compile
correctly while everyone else gets a poured bottle. Override it under
`formula.build`, or set `formula.build.enabled: false` for the older
bottle-only install block.

## Installation

```sh
brew trust isometry/tap               # Homebrew ≥6: one-time tap trust
brew install isometry/tap/gobottle    # poured as a bottle, naturally
go install github.com/isometry/gobottle@latest
```

In GitHub Actions, use
[isometry/gobottle-setup](https://github.com/isometry/gobottle-setup) to
install from verified release binaries.

## Requirements

- Go toolchain (for `--source=go` builds)
- A GitHub token with `write:packages` (GHCR) and `contents: write` (tap)
- After the first push, flip the GHCR package visibility to **public** so
  `brew`'s anonymous pulls work

## Claude Code plugin

This repository doubles as a Claude Code plugin providing a `gobottle`
skill (usage, configuration reference, and a goreleaser-migration guide):

```
/plugin marketplace add isometry/gobottle
/plugin install gobottle@gobottle
```

## License

MIT — see [LICENSE](LICENSE).
