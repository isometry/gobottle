# Migrating from goreleaser `brews:` to gobottle

goreleaser's `brews:` writes a formula whose `url` points at per-OS
release archives. It works, but it isn't Homebrew's native distribution
model: there are no bottles (so no integrity-pinned pour, no
INSTALL_RECEIPT, no keg-shipped completions), the install/test blocks are
limited templates, and every field lives inside the goreleaser config.
gobottle replaces exactly that section — goreleaser keeps doing what it's
good at (GitHub-release archives, changelogs); gobottle takes over
everything Homebrew.

## Field mapping

| goreleaser `brews:`        | gobottle `.gobottle.yaml`                         |
| -------------------------- | ------------------------------------------------- |
| `name`                     | `formula.name` (default: repo name)               |
| `description`              | `formula.description`                             |
| `homepage`                 | `formula.homepage` (default: GitHub URL)          |
| `license`                  | `formula.license`                                 |
| `repository.owner`         | `tap.owner` (default: source owner)               |
| `repository.name`          | `tap.repo` (default: `homebrew-tap`)              |
| `repository.branch`        | `tap.branch` (default: the tap's default branch)  |
| `repository.token`         | `GOBOTTLE_TAP_TOKEN` env (falls back to `GITHUB_TOKEN`) |
| `directory`                | `tap.formula_path` (default: `Formula`)           |
| `commit_msg_template`      | `tap.commit_message`                              |
| `install: bin.install ...` | `binaries:` list (+ `formula.install.extra` for anything beyond binary installs) |
| `test:`                    | `formula.test.raw` (verbatim) or `formula.test.command` |
| `caveats`                  | `formula.caveats`                                 |
| `dependencies`             | `formula.dependencies`                            |
| `conflicts`                | `formula.conflicts`                               |
| `service`                  | `formula.service`                                 |
| `skip_upload`              | `--dry-run`                                       |

Not needed anymore: `url_template`, `download_strategy`, per-OS URL logic —
bottles are fetched from GHCR via the formula's `bottle do` block, and the
source `url` stanza is derived automatically.

## Before / after

goreleaser `brews:` (delete this whole section from `.goreleaser.yml`):

```yaml
brews:
  - repository:
      owner: acme
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"
    directory: Formula
    description: Lightning-fast widget frobnicator
    homepage: https://acme.dev/mytool
    test: |
      system "#{bin}/mytool --help"
    install: |
      bin.install "mytool"
```

Equivalent `.gobottle.yaml`:

```yaml
formula:
  name: mytool
  description: Lightning-fast widget frobnicator
  homepage: https://acme.dev/mytool
  install:
    completions: true      # bonus: cobra completions shipped in bottles
  test:
    raw: |
      system "#{bin}/mytool --help"

source:
  type: go
  build:
    packages: ["."]

tap:
  owner: acme
```

(`tap.repo: homebrew-tap`, `directory: Formula`, and the binary name all
match the defaults, so they're omitted.)

## Workflow changes

In the release workflow, after the goreleaser step (which keeps
`contents: write`), add `packages: write` to permissions and:

```yaml
      - uses: isometry/gobottle-setup@v1   # pin to a SHA in real workflows

      - name: Publish bottles
        run: gobottle release
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GOBOTTLE_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

Notes:

- `HOMEBREW_TAP_GITHUB_TOKEN` is the same secret goreleaser's `brews:`
  already used — no new credentials. It needs contents write on the tap
  repo only; `GITHUB_TOKEN` covers GHCR.
- Checkout needs `fetch-depth: 0`: gobottle derives the version from the
  semver tag at HEAD and timestamps from the commit.
- Source choice: `source.type: go` has gobottle cross-compile
  deterministically (bottles are reproducible per commit). Alternatively
  `--source local` consumes goreleaser's `dist/` so released archives and
  bottles contain byte-identical binaries — choose it when that identity
  matters more than gobottle-controlled reproducibility. (gobottle's
  extractor expects tar.gz archives; zip-only goreleaser configs need
  `source.type: go`.)

## First-release checklist

1. Tag and push as usual; the workflow publishes bottles to
   `ghcr.io/<root_path>/<formula>` and commits the formula.
2. Make the new GHCR package **public** (Package settings → Change
   visibility) — brew pours anonymously.
3. The goreleaser-era formula file is simply overwritten by gobottle's
   commit; no manual cleanup.
4. Verify: `brew trust <owner>/<tap>` (required once on Homebrew ≥6 —
   third-party taps are untrusted by default and installs fail without
   it), then `brew update && brew install <owner>/<tap>/<formula>` should
   say "Pouring <formula>--<version>...bottle.tar.gz"; completions land in
   `$(brew --prefix)/share/zsh/site-functions/_<formula>` et al.
5. Existing users upgrade seamlessly: same formula name, same tap; the
   next `brew upgrade` pours a bottle instead of downloading an archive.
   Users who upgraded to Homebrew ≥6 need the same one-time
   `brew trust <owner>/<tap>` — document it in your install instructions.
