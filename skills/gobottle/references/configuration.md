# .gobottle.yaml reference

Precedence (highest first): CLI flag > `GOBOTTLE_*` env > config file >
git-derived defaults > built-in defaults. Env vars map dots to
underscores: `formula.install.completions` → `GOBOTTLE_FORMULA_INSTALL_COMPLETIONS`.

Shorthands: `formula: mytool` ≡ `formula: {name: mytool}`;
`binaries: [a, b]` ≡ `binaries: [{name: a}, {name: b}]`.

## formula — the generated formula (gobottle owns it outright)

| Key            | Default                                   | Notes |
| -------------- | ----------------------------------------- | ----- |
| `name`         | repo name                                 | also the positional arg / `--formula` |
| `description`  | —                                         | `desc` stanza; brew audit warns if empty |
| `homepage`     | `https://github.com/<owner>/<repo>`       | |
| `license`      | —                                         | SPDX id; omitted if empty |
| `head`         | `true` when a git URL is derivable        | emits `head "<repo>.git", branch:`; set `false` to suppress |
| `head_branch`  | `main`                                    | branch for the head stanza |
| `dependencies` | `[]`                                      | `depends_on` lines |
| `conflicts`    | `[]`                                      | `conflicts_with` lines |
| `caveats`      | —                                         | verbatim caveats heredoc |
| `service`      | —                                         | verbatim `service do` body |
| `template`     | —                                         | path to a Go template overriding the built-in formula template |

### formula.install

| Key                   | Default        | Notes |
| --------------------- | -------------- | ----- |
| `completions`         | `false`        | generate completions at build time, ship in bottles, and emit the install-block line |
| `completions_command` | `[completion]` | invoked as `<binary> <command...> <shell>` for bash/zsh/fish |
| `extra`               | `[]`           | verbatim lines appended to `def install` |

### formula.build — compiling from source on the user's machine

Drives the generated `def install`, so `brew install --build-from-source`
and `brew install --HEAD` compile correctly instead of failing on a
`bin.install` that finds no binary. Every key defaults from the matching
`source.build` key — the same recipe gobottle cross-compiles the bottles
with — so existing configurations get this with no edits. Pouring a bottle
never runs `def install`, so none of this affects the normal install path.

| Key        | Default                          | Notes |
| ---------- | -------------------------------- | ----- |
| `enabled`  | `true`                           | `false` restores the legacy `bin.install` block (bottle-only) |
| `go`       | `go`                             | `depends_on` spec; e.g. `go@1.23` to pin the toolchain |
| `packages` | `source.build.packages`, else `["."]` | paired with `binaries:` by index, exactly as the cross-compiler pairs them |
| `ldflags`  | `source.build.ldflags`           | same template vars; rendered with Ruby interpolations (see below) |
| `tags`     | `[]`                             | `std_go_args(tags: [...])` |
| `flags`    | `source.build.flags`             | extra `go build` flags |
| `env`      | `source.build.env`               | `ENV["KEY"] = "value"` lines; `CGO_ENABLED` is always derived from `source.build.cgo_enabled` |
| `mod_dir`  | `source.build.mod_dir`           | wrapped in `cd "<dir>" do … end` when not `.` |

The ldflags template renders to Ruby interpolations: `{{.Version}}` →
`#{version}`, `{{.Tag}}` → `v#{version}`, `{{.Date}}` → `#{time.iso8601}`,
`{{.Commit}}` → `#{commit}`, where `commit` is the release tag's actual
commit, baked in as a literal with a `build.head?` branch reading the
checkout for `--HEAD` builds. Bare `-s`/`-w` are dropped and `-trimpath`
is never passed: Homebrew's `std_go_args` supplies all three itself, and
forwarding them would defeat `brew install --debug-symbols`. Each `%W[]`
element is one whitespace-separated token, so ldflags values containing
spaces are unsupported (as for the host build).

With more than one binary and no `packages` configured in either section,
the package↔binary mapping cannot be guessed: gobottle warns and emits the
legacy `bin.install` block instead.

### formula.test

| Key       | Default       | Notes |
| --------- | ------------- | ----- |
| `command` | `[--version]` | renders `system bin/"<first bin binary>", <args...>` |
| `raw`     | —             | replaces the whole test body verbatim |

## version

Version being bottled; `v` prefix stripped. Default: latest semver tag at
HEAD (falls back to the latest reachable tag). Flag: `--version`.

## source — where artifacts come from

| Key         | Default | Notes |
| ----------- | ------- | ----- |
| `type`      | `local` | `go` (compile the module), `local` (GoReleaser dist/), `github` (release archives) |
| `owner`     | derived from git remote | GitHub coordinates |
| `repo`      | derived from git remote | |
| `tag`       | —       | release tag for `--source github` |
| `dist_path` | `dist`  | for `--source local` |
| `url`       | derived | source tarball URL for the formula |
| `sha256`    | computed | source tarball SHA256 (fetched if empty) |

### source.build (for `type: go`)

| Key           | Default    | Notes |
| ------------- | ---------- | ----- |
| `packages`    | —          | e.g. `["./cmd/mytool"]` |
| `ldflags`     | —          | template: `{{.Version}}`, `{{.Commit}}`, `{{.Date}}`, `{{.Tag}}`; Date derives from the HEAD commit for reproducibility |
| `env`         | `[]`       | `KEY=value` strings |
| `cgo_enabled` | `false`    | also drives `ENV["CGO_ENABLED"]` in the generated install block |
| `trimpath`    | `true`     | set `false` to opt out; the source-built formula always gets it from `std_go_args` |
| `flags`       | `[]`       | extra `go build` flags |
| `mod_dir`     | `.`        | |
| `parallel`    | CPU count  | |

## bottle

| Key                 | Default                 | Notes |
| ------------------- | ----------------------- | ----- |
| `cellar`            | `:any_skip_relocation`  | or `:any`, or an absolute cellar path |
| `platforms`         | all discovered          | e.g. `[arm64_sonoma]` |
| `exclude_platforms` | `[]`                    | |
| `rebuild`           | `0`                     | bump when re-bottling the same version |
| `refresh_platforms` | `false`                 | force platform-cache refresh |

## registry

| Key         | Default                                  | Notes |
| ----------- | ---------------------------------------- | ----- |
| `host`      | `ghcr.io`                                | |
| `owner`     | `source.owner`                           | |
| `root_path` | `<owner>/<tap.repo minus homebrew->`     | env: `GOBOTTLE_REPO` (ko-style) or `GOBOTTLE_REGISTRY_ROOT_PATH`; formula `root_url` becomes `https://<host>/v2/<root_path>` |
| `token`     | `$GITHUB_TOKEN`                          | `write:packages` |

## tap

| Key              | Default                                | Notes |
| ---------------- | -------------------------------------- | ----- |
| `owner`          | `source.owner`                         | |
| `repo`           | `homebrew-tap`                         | |
| `branch`         | the tap's default branch (API-resolved) | set only to target a non-default branch |
| `formula_path`   | `Formula`                              | |
| `commit_message` | `Update {{ .Formula }} to {{ .Version }}` | |
| `token`          | registry token                          | env: `GOBOTTLE_TAP_TOKEN`; needs contents write on the tap repo |

## binaries

List of `{name, install_path}`; default: one binary named after the
formula, installed to `bin`. `install_path: libexec` (or any keg-relative
path) renders the matching install line; only `bin`-installed binaries
drive the default test and completions.
