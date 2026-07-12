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
| `head`         | `false`                                   | emits `head "<repo>.git", branch:` |
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
| `cgo_enabled` | `false`    | |
| `trimpath`    | `true`     | |
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
