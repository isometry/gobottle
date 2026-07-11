# gobottle - Project Overview

## Purpose
gobottle is "ko for Homebrew": it cross-compiles Go binaries, packages them as
real Homebrew bottles, publishes them to GHCR with homebrew-core's exact OCI
conventions, and fully generates the tap formula. Pure Go, no Ruby/skopeo, no
per-OS build runners. Status: experimental (v0.1.0, July 2026); positioning is
experiment/learning per the maintainer.

## Verified end-to-end (2026-07-11)
`gobottle release` published gobottle itself to ghcr.io/isometry/test/gobottle
and committed Formula/gobottle.rb to isometry/homebrew-test; `brew install
isometry/test/gobottle` poured successfully on macOS (Homebrew 6.0.9), and
`brew test` passed. Private-package pours work with
HOMEBREW_DOCKER_REGISTRY_TOKEN=$(gh auth token | base64).

## CLI contract
- `gobottle build` -> bottles + bottles.json (no token needed)
- `gobottle push` -> OCI index per version[-rebuild] tag (GITHUB_TOKEN, write:packages)
- `gobottle release` -> regenerated formula committed to tap (or --tap-path local write)
- stdout = JSON (-o json), stderr = progress; exit 2 = validation error
- config: .gobottle.yaml with `formula:` content section; env GOBOTTLE_*, GOBOTTLE_REPO

## Design sources
Homebrew's github_packages.rb / bottle.rb / resource.rb / utils/bottles.rb are
the reference implementation; the "oldest supported macOS tag per arch"
strategy is valid because brew falls back to older-or-equal same-arch bottles
(extend/os/mac/utils/bottles.rb).

## Known gaps / follow-ups
- formula.install.completions emits generate_completions_from_executable in
  def install, which does NOT run on bottle pours - completions should be
  generated at bottle-build time and shipped inside the bottle (open gap).
- {{.Date}} ldflags stamping uses build wall-clock -> breaks bottle
  determinism; should default to the commit timestamp.
- MinSupportedMacOSMajor=12 (monterey) hardcoded in platform/discover.go -
  deliberate max-compatibility choice, revisit as Homebrew drops versions.
- GHCR package visibility must be flipped public manually (UI) for anonymous
  pours; gobottle warns after push.

## Tech Stack
Go 1.25; cobra + viper (single Unmarshal in config.Load); go-git;
google/go-containerregistry (custom bottleLayer keeps bottle bytes exact);
go-github for tap commits.
