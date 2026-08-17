---
name: gobottle
description: >-
  Publish real Homebrew bottles for Go projects with gobottle: cross-compile,
  package brew-pourable bottles for every supported macOS/Linux platform,
  push them to GHCR as OCI indexes, and generate the tap formula outright.
  Use this skill whenever the user wants to distribute a Go CLI via Homebrew,
  set up or automate a Homebrew tap, publish bottles (not just formulas),
  configure .gobottle.yaml, use the gobottle CLI or the isometry/gobottle-setup
  GitHub Action, or migrate a repository away from goreleaser's `brews:`
  homebrew support — even if they only say "make my tool brew-installable"
  or "my formula builds from source and it's slow".
---

# gobottle

gobottle is to Homebrew bottles what [ko](https://ko.build) is to container
images: point it at a Go module and it cross-compiles, packages **real,
brew-pourable bottles** for every platform Homebrew supports, publishes
them to GHCR using the exact OCI conventions brew expects (one image index
per version, blobs addressable by bottle SHA256), and generates and commits
the complete tap formula. The formula is owned outright — every release
renders it from config; there is no hand-editing.

Why bottles instead of goreleaser's `brews:`: goreleaser writes a formula
that downloads per-OS release archives — brew treats it as a from-source
formula wearing a trench coat. Real bottles pour like homebrew-core
packages: `brew install` fetches a keg with an INSTALL_RECEIPT, shell
completions land in the keg (and get linked automatically), `brew test`
works, and the bottle block pins per-platform SHA256s for integrity. See
[Migrating from goreleaser](#migrating-from-goreleaser-brews) below.

## Installing gobottle

- Locally: `brew trust isometry/tap && brew install isometry/tap/gobottle`
  (Homebrew ≥6 requires third-party taps to be trusted before install;
  poured as a bottle, naturally) or
  `go install github.com/isometry/gobottle@latest`.
- In GitHub Actions: the `isometry/gobottle-setup` action — see
  [CI with gobottle-setup](#ci-with-gobottle-setup).

## Quickstart

`.gobottle.yaml` in the repo root (`gobottle init` scaffolds one):

```yaml
formula:
  name: mytool
  description: Does something delightful
  license: MIT
  install:
    completions: true   # generated at build time, shipped in every bottle
  test:
    command: [--version]

source:
  type: go
  build:
    packages: ["."]
    ldflags: >-
      -s -w
      -X main.version={{.Version}}

tap:
  owner: myorg          # commits Formula/mytool.rb to myorg/homebrew-tap
```

One-shot release (build + push + tap commit), version from the semver tag
at HEAD:

```sh
GITHUB_TOKEN=... gobottle release
```

## Pipeline and verbs

Each verb emits machine-readable JSON on stdout (progress on stderr) so
stages compose in CI:

| Verb        | Purpose                                                        |
| ----------- | -------------------------------------------------------------- |
| `build`     | Compile/collect artifacts, package bottles + `bottles.json` manifest |
| `push`      | Publish manifest bottles to the registry (idempotent re-push)  |
| `release`   | Render formula (with bottle block) and commit it to the tap; without `-i` runs the whole pipeline |
| `platforms` | List/refresh the discovered Homebrew platform set              |
| `init`      | Scaffold `.gobottle.yaml`                                       |

Staged: `gobottle build -o json | gobottle push`, then
`gobottle release -i bottles.json` (or `-i -` for stdin — stdin is only
read when asked explicitly). `--dry-run` renders everything and pushes
nothing.

Artifact sources: `--source go` (ko-like, cross-compiles the module),
`--source local` (a GoReleaser `dist/` directory), `--source github` (an
existing GitHub release's archives). Local and GitHub sources accept
tar.gz, zip, and tar.bz2 archives (gobottle ≥0.5), verified against the
release's checksum manifest.

## Key behaviors worth knowing

- **Deterministic builds**: timestamps default to the HEAD commit's time
  (override with `SOURCE_DATE_EPOCH`), so rebuilding the same commit
  yields bit-identical bottles and idempotent re-pushes.
- **Completions in bottles**: with `formula.install.completions: true`,
  gobottle runs the binary's completion subcommand
  (`formula.install.completions_command`, default `[completion]`) at build
  time and ships bash/zsh/fish scripts in every platform's bottle at
  brew's keg-relative paths. The
  `generate_completions_from_executable` line is still emitted for
  `--build-from-source` users.
- **Source-buildable formulae**: the generated `def install` compiles from
  source — `depends_on "go" => :build`, the ldflags from `source.build`
  rendered as Ruby interpolations, and `system "go", "build",
  *std_go_args(...)` — so `brew install --build-from-source` and
  `brew install --HEAD` both work, and the `head` stanza is emitted by
  default. The recipe is inherited from `source.build`, so existing
  configs get it with no edits; override under `formula.build` (or set
  `formula.build.enabled: false` for the old bottle-only install block).
  `{{.Commit}}` becomes the release tag's actual commit, with a
  `build.head?` branch reading the checkout for `--HEAD`.
- **Tokens**: `GITHUB_TOKEN` authenticates GHCR; `GOBOTTLE_TAP_TOKEN`
  optionally dedicates a token to the tap commit and falls back to the
  standard token when unset. In GitHub Actions the default job token
  handles GHCR (`permissions: packages: write`); the tap commit needs a
  PAT with contents write on the tap repo.
- **Tap branch**: unconfigured `tap.branch` means the tap's actual default
  branch, resolved via the API — taps still on `master` just work.
- **Multi-binary**: `binaries: [{name: mytool}, {name: helper,
  install_path: libexec}]` stages and renders install lines per path.
- **GHCR visibility**: after the first release, make the package public
  (Package settings → Change visibility) or brew cannot pour anonymously.
- **Tap trust (Homebrew ≥6)**: installs from third-party taps fail until
  the user runs `brew trust <owner>/<tap>` once (or trusts a single
  formula with `brew trust --formula <owner>/<tap>/<formula>`) — tell
  formula consumers to include that step.

Full configuration reference: [references/configuration.md](references/configuration.md).

## CI with gobottle-setup

The [isometry/gobottle-setup](https://github.com/isometry/gobottle-setup)
action installs gobottle from verified release binaries (SHA256SUMS +
GitHub-native SLSA attestation) — no Go toolchain or compilation needed:

```yaml
jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v7      # pin to a SHA in real workflows
        with:
          fetch-depth: 0               # gobottle reads the tag + commit time
      - uses: isometry/gobottle-setup@v1
      - run: gobottle release
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GOBOTTLE_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

Action inputs: `version` (`latest`, exact pin like `1.2.3`, or prefix like
`1` / `1.2` → newest matching stable), `repository`, `token`, `verify`
(SLSA provenance check, default on; checksums always enforced). Outputs:
`version`, `path`, `cache-hit`.

**Already running goreleaser in the same job?** Don't rebuild — bottle
what it just built. With gobottle ≥0.7, `--source local` reads
goreleaser's `dist/artifacts.json` and bottles the **raw binaries**
directly (no archive round-trip; multi-binary aware); dists without that
manifest fall back to archive discovery (tar.gz/zip/tar.bz2, verified
against goreleaser's checksum manifest):

```yaml
      - uses: goreleaser/goreleaser-action@v7
        with:
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      - uses: isometry/gobottle-setup@v1
      - run: gobottle release --source local
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GOBOTTLE_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

Choosing: dist-reuse skips a full cross-compile and bottles the exact
bytes of the release build — one build across both distribution
channels; `source: go` gives commit-reproducible bottles and needs no
goreleaser at all.

**Provenance (recommended with dist-reuse):** attest every distribution
surface so `gh attestation verify --repo <owner>/<repo>` works directly
against release archives, raw/installed binaries, and the bottle
tarballs brew downloads. The job needs `id-token: write` and
`attestations: write` permissions, and the attest step goes after the
bottles are built:

```yaml
      - uses: actions/attest-build-provenance@v4
        with:
          subject-path: |
            dist/*.zip
            dist/*_SHA256SUMS
            dist/*/<binary>
            bottles/*.tar.gz
```

Your users can then verify an installed binary end-to-end:
`gh attestation verify "$(brew --prefix)/bin/<binary>" --repo <owner>/<repo>`.

## Migrating from goreleaser `brews:`

If a repo publishes to a tap via goreleaser's `brews:` section, migration
is: delete the `brews:` block, add `.gobottle.yaml` (the field mapping is
mostly 1:1), and add two steps to the release workflow. goreleaser keeps
building the GitHub-release archives; gobottle takes over everything
Homebrew. The old goreleaser-generated formula is overwritten by
gobottle's first release commit — no cleanup needed. Users get real
bottles, integrity-pinned installs, and working shell completions.

Follow [references/goreleaser-migration.md](references/goreleaser-migration.md)
for the field-mapping table, a before/after example, and the first-release
checklist.

## Deeper internals

For the Homebrew bottle format itself (tarball layout, INSTALL_RECEIPT,
OCI publishing contract), see the `homebrew-bottles` skill in the gobottle
repository (`.claude/skills/homebrew-bottles/`).
