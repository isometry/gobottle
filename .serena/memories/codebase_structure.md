# gobottle - Codebase Structure (post-overhaul, v0.1.0)

```
gobottle/
├── main.go                 # Entry point; exit code 2 for validation errors
├── .gobottle.yaml          # Dogfood config (new formula: section schema)
│
├── cmd/                    # Thin cobra verbs
│   ├── root.go             # Global flags (--config, --log-level, --dry-run), GOBOTTLE_* env setup
│   ├── build.go            # `build`: compile/collect -> bottles + bottles.json (runBuild)
│   ├── push.go             # `push`: manifest -> OCI index per version on GHCR (runPush)
│   ├── release.go          # `release`: formula generation + tap commit; one-shot pipeline (runRelease)
│   ├── common.go           # bindFlags (RunE-time viper binding!), loadConfig, formulaModel
│   ├── manifest.go         # bottles.json schema (schemaVersion 1) + read/write helpers
│   ├── init.go, platforms.go, version.go, completion.go
│
├── internal/
│   ├── artifact/           # Sources: go.go (flagship ko-like builder), local.go (dist/), github.go
│   ├── bottle/             # builder.go, archive.go (deterministic tar.gz), tab.go, bottle.go
│   ├── config/             # config.go (Config + FormulaConfig schema), load.go (viper.Unmarshal + hooks)
│   ├── formula/            # formula.go (full model), generator.go (template, conditional stanzas)
│   ├── git/                # go-git helpers (owner/repo, tags, HEAD commit)
│   ├── oci/                # pusher.go (Publisher: brew-exact OCI indexes), layer.go, auth.go
│   ├── platform/           # Homebrew platform discovery + cache (oldest-macOS-per-arch strategy)
│   ├── tap/                # GitHub Contents API updater (returns commit SHA)
│   └── util/               # ExtractTarGz, sha256 helpers
```

## Key invariants (verified against Homebrew source)
- root_url NEVER contains the formula name (brew appends it); image = <host>/<root_path>/<formula>
- always push an OCI *index* tagged version[-rebuild], children annotated with
  sh.brew.bottle.digest, sh.brew.tab, org.opencontainers.image.ref.name=version.tag[.rebuild]
- layer digest == bottle tar.gz sha256 (bottleLayer keeps bytes exact)
- bottles are deterministic (sorted tar entries, zeroed owners, SOURCE_DATE_EPOCH)
- flag->viper binding happens in RunE (bindFlags), NOT at command construction

## Verb pipeline
`gobottle build -o json | gobottle push -o json | gobottle release` or one-shot `gobottle release`.
JSON on stdout, progress on stderr. bottles.json is the contract between stages.
