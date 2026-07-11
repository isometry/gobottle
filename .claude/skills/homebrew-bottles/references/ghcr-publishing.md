# GHCR Publishing Reference

How to publish Homebrew bottles to GitHub Container Registry (ghcr.io) so
that `brew install` can actually pour them.

Ground truth: `Library/Homebrew/github_packages.rb` (publish side) and
`bottle.rb` / `resource.rb` / `utils/bottles.rb` (pour side) in Homebrew/brew.
Everything below was verified against those files and a live `brew install`
from a scratch tap (July 2026).

## URL Structure — the #1 way to get this wrong

**Brew appends the formula name to `root_url` itself.** The formula's
`root_url` must therefore NOT include the formula name:

```
image path:  ghcr.io/<root_path>/<formula>          (what you push to)
root_url:    https://ghcr.io/v2/<root_path>          (what the formula declares)
manifest:    <root_url>/<formula>/manifests/<version>[-<rebuild>]   (brew fetches)
blob:        <root_url>/<formula>/blobs/sha256:<bottle sha256>      (brew fetches)
```

- homebrew-core: image `ghcr.io/homebrew/core/wget`, root_url
  `https://ghcr.io/v2/homebrew/core`
- personal tap: image `ghcr.io/<user>/<tap-minus-homebrew->/<formula>`,
  root_url `https://ghcr.io/v2/<user>/<tap-minus-homebrew->`

Formula-name sanitization (`GitHubPackages.image_formula_name`): `@` → `/`,
`+` → `x`. The whole repository path must be lowercase.

If root_url includes the formula name, brew requests
`.../<formula>/<formula>/...` and every install 404s.

## Tagging model

- **One OCI *index* per version, tagged `<version>[-<rebuild>]`**
  (e.g. `1.2.3`, `1.2.3-1` for rebuild 1). Tag grammar:
  `^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`.
- **Per-platform image manifests are referenced by digest from the index** —
  they are NOT tagged individually.
- **Always push an index, even for a single platform.** Brew parses the
  manifest response for a `manifests[]` array and fails with
  "Missing 'manifests' section." on a bare image manifest (`resource.rb`).

## OCI structure

### Index (what the version tag points at)

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:<child manifest digest>",
      "size": 1234,
      "platform": {
        "architecture": "arm64",
        "os": "darwin",
        "os.version": "macOS 14"
      },
      "annotations": {
        "org.opencontainers.image.ref.name": "1.0.0.arm64_sonoma",
        "sh.brew.bottle.digest": "<bottle tar.gz sha256, BARE HEX no sha256: prefix>",
        "sh.brew.bottle.size": "5000000",
        "sh.brew.bottle.installed_size": "17065472",
        "sh.brew.tab": "{\"homebrew_version\":\"4.4.0\",\"built_as_bottle\":true,...}"
      }
    }
  ],
  "annotations": {
    "com.github.package.type": "homebrew_bottle",
    "org.opencontainers.image.ref.name": "1.0.0",
    "org.opencontainers.image.source": "https://github.com/user/homebrew-tap"
  }
}
```

### How brew selects and validates (resource.rb)

1. Fetch the index with `Accept: application/vnd.oci.image.index.v1+json`.
2. Find the child whose annotations satisfy **both**:
   - `sh.brew.bottle.digest` == the formula's bottle sha256 (bare hex)
   - `org.opencontainers.image.ref.name` == `<version>.<platform_tag>[.<rebuild>]`
3. **Read `sh.brew.tab` from that child** (runtime dependencies). Missing or
   blank → hard error: *"Couldn't find tab from manifest."* — install aborts.
4. Fetch the layer blob by the formula's sha256 and verify.

All three annotations are therefore load-bearing; `sh.brew.bottle.size`,
`installed_size`, licenses etc. are informational.

### Child image manifest

- media type `application/vnd.oci.image.manifest.v1+json`
- **real config blob** (`application/vnd.oci.image.config.v1+json`) with
  `architecture`, `os`, `os.version` (e.g. `"macOS 14"`), and
  `rootfs.diff_ids` = [sha256 of the *uncompressed* tar] — this is what brew
  itself publishes (not an empty `{}` config)
- exactly one layer, `application/vnd.oci.image.layer.v1.tar+gzip`, whose
  digest is the bottle file's sha256 — **the blob must be the bottle tar.gz
  byte-for-byte** since that digest is what the formula records
- brew mirrors the descriptor annotations onto the manifest itself

## Go implementation (go-containerregistry, verified working)

Key insight: don't let the library re-encode the bottle. Implement `v1.Layer`
so `Digest()` returns the file's sha256 and `DiffID()` the uncompressed-tar
sha256; `Compressed()` streams the file as-is.

```go
// bottleLayer preserves the bottle bytes exactly.
type bottleLayer struct {
    path   string
    digest v1.Hash // sha256 of the .tar.gz file == formula sha256
    diffID v1.Hash // sha256 of the raw tar stream
    size   int64
}
func (l *bottleLayer) MediaType() (types.MediaType, error) { return types.OCILayer, nil }
func (l *bottleLayer) Compressed() (io.ReadCloser, error)  { return os.Open(l.path) }
// ... Digest, DiffID, Size, Uncompressed (gzip reader over the file)

func bottleImage(b *Bottle) (v1.Image, error) {
    img := mutate.MediaType(empty.Image, types.OCIManifestSchema1) // force OCI:
    img = mutate.ConfigMediaType(img, types.OCIConfigJSON)         // empty.Image defaults to Docker types!
    img, _ = mutate.ConfigFile(img, &v1.ConfigFile{
        Architecture: "arm64", OS: "darwin", OSVersion: "macOS 14",
        RootFS: v1.RootFS{Type: "layers"}, // diff_ids appended by mutate.AppendLayers
    })
    img, err := mutate.AppendLayers(img, layer)
    // annotations: ref.name, sh.brew.bottle.digest, sh.brew.tab, ...
    return mutate.Annotations(img, annotations).(v1.Image), err
}

// One index per version tag; remote.WriteIndex pushes children by digest.
idx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
idx = mutate.AppendManifests(idx, adds...) // IndexAddendum{Add: img, Descriptor: {Platform, Annotations}}
idx = mutate.Annotations(idx, indexAnnotations).(v1.ImageIndex)
err := remote.WriteIndex(repo.Tag("1.2.3"), idx, remote.WithAuthFromKeychain(kc))
```

**Keep-old / append semantics** (what `brew pr-upload --keep-old` does):
fetch the existing index, drop children being replaced, append new ones:

```go
if existing, err := remote.Index(tagRef, opts...); err == nil {
    idx = mutate.RemoveManifests(existing, func(d v1.Descriptor) bool {
        return replaced[d.Annotations["org.opencontainers.image.ref.name"]]
    })
}
idx = mutate.AppendManifests(idx, adds...)
```

Content-addressed pushes make re-runs idempotent **iff bottles are
deterministic** (sorted tar entries, zeroed owners, fixed mtimes).

Auth: `authn.AuthConfig{Username: "oauth2", Password: GITHUB_TOKEN}` (GHCR
ignores the username) via a keychain scoped to the registry host.

## Authentication

| Purpose | Mechanism |
|---------|-----------|
| Anonymous pour (public package) | `Authorization: Bearer QQ==` (base64 empty string) |
| Pour from private package | `HOMEBREW_DOCKER_REGISTRY_TOKEN=$(echo -n $PAT \| base64)` — verified working with `gh auth token` |
| Push | PAT / gh token with `write:packages` (fine-grained PATs and OIDC are NOT accepted by GHCR); inside Actions, `GITHUB_TOKEN` with `packages: write` |
| Registry token exchange (manual curl) | `curl -u "x:$PAT" "https://ghcr.io/token?scope=repository:<path>:pull"` |

## Visibility — the first-push trap

- **The first push always creates a PRIVATE package.** Anonymous `QQ==`
  pours fail (401/403) until visibility is flipped.
- **The packages REST API cannot change visibility** — it's UI-only:
  package page → Package settings → Danger Zone → Change visibility.
  (Do not trust snippets claiming `gh api -X PATCH ... -f visibility=public`
  works; it doesn't.)
- Publishing tools should HEAD the tag anonymously after pushing and warn
  when it isn't publicly readable.
- `org.opencontainers.image.source` on the index links the package to a
  repository (UI association; it does not auto-publicize the package).

## Verifying a published bottle

```bash
TOKEN=$(curl -s -u "x:$(gh auth token)" \
  "https://ghcr.io/token?scope=repository:user/tap/formula:pull" | jq -r .token)

# Must be an INDEX with manifests[].annotations carrying digest/ref.name/tab
curl -s -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/vnd.oci.image.index.v1+json" \
  "https://ghcr.io/v2/user/tap/formula/manifests/1.0.0" \
  | jq '.manifests[].annotations | keys'

# The blob at the formula's sha256 must be the bottle file byte-for-byte
curl -sL -H "Authorization: Bearer $TOKEN" \
  "https://ghcr.io/v2/user/tap/formula/blobs/sha256:<bottle sha>" \
  | shasum -a 256
```

## Formula integration

```ruby
bottle do
  root_url "https://ghcr.io/v2/user/tap"   # NO formula name here
  rebuild 1                                 # only when rebuild > 0
  sha256 cellar: :any_skip_relocation, arm64_sonoma: "abc123..."
  sha256 cellar: :any_skip_relocation, x86_64_linux: "def456..."
end
```

The stable `url`/`sha256` above the bottle block are never fetched during a
pour (only `brew audit` / `--build-from-source` touch them).

## Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| 404 on manifests/blobs at install | root_url includes the formula name | root_url = registry path WITHOUT formula |
| "Missing 'manifests' section." | pushed a bare image manifest | always push an index |
| "Couldn't find tab from manifest." | child lacks `sh.brew.tab` annotation | annotate descriptors with the tab JSON |
| "Couldn't find manifest matching bottle checksum." | digest/ref.name annotations don't match the formula | bare-hex `sh.brew.bottle.digest`; ref.name `<version>.<tag>[.<rebuild>]` |
| 401/403 pulling anonymously | package still private | flip visibility in the UI (API can't) |
| `DENIED` on push | token lacks `write:packages` | classic PAT or Actions `packages: write` |
| brew 6: tap doesn't evaluate | tap trust | `brew trust user/repo` or `HOMEBREW_NO_REQUIRE_TAP_TRUST=1` |

## Best Practices

1. **go-containerregistry over raw HTTP/skopeo** — but keep the bottle bytes
   exact (custom layer, not re-encoding)
2. **Deterministic bottles** ⇒ stable digests ⇒ idempotent re-pushes and safe
   CI retries
3. **Warn about visibility after first push** — it's the most common
   silent-failure for new taps
4. **Verify after push** by fetching the index and blob back (see above)
5. **Test the real thing**: `brew install` from a scratch tap is the only
   proof; the manifest structure has multiple independently-fatal details
