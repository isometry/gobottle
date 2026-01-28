# GHCR Publishing Reference

How to publish Homebrew bottles to GitHub Container Registry (ghcr.io).

## Overview

Homebrew stores bottles in GitHub Container Registry as OCI (Open Container Initiative) images. Each bottle is a single-layer OCI image with the bottle tarball as the layer content.

## URL Structure

### Registry Base

```
ghcr.io/v2/<owner>/<repo>
```

For homebrew-core: `ghcr.io/v2/homebrew/core`
For custom taps: `ghcr.io/v2/<user>/homebrew-<tap>`

### Endpoints

| Endpoint | Purpose |
|----------|---------|
| `/v2/<name>/manifests/<tag>` | Get/push manifest |
| `/v2/<name>/blobs/<digest>` | Get/push blob (layer) |
| `/v2/<name>/blobs/uploads/` | Initiate blob upload |

## OCI Image Structure

### Image Index (Manifest List)

For multi-platform bottles, an image index lists all platform-specific images:

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:abc123...",
      "size": 1234,
      "platform": {
        "architecture": "arm64",
        "os": "darwin",
        "os.version": "macOS 14.0"
      },
      "annotations": {
        "sh.brew.bottle.digest": "sha256:def456...",
        "org.opencontainers.image.ref.name": "1.0.0.arm64_sonoma"
      }
    }
  ]
}
```

### Image Manifest

Each platform has its own manifest:

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {
    "mediaType": "application/vnd.oci.image.config.v1+json",
    "digest": "sha256:empty...",
    "size": 2
  },
  "layers": [
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:bottle_sha...",
      "size": 5000000,
      "annotations": {
        "org.opencontainers.image.title": "gobottle-1.0.0.arm64_sonoma.bottle.tar.gz"
      }
    }
  ],
  "annotations": {
    "sh.brew.bottle.digest": "sha256:bottle_sha...",
    "sh.brew.tab": "{\"homebrew_version\":\"4.4.0\",...}",
    "org.opencontainers.image.created": "2024-01-23T12:00:00Z",
    "org.opencontainers.image.description": "Homebrew bottle for gobottle",
    "org.opencontainers.image.title": "gobottle",
    "org.opencontainers.image.version": "1.0.0"
  }
}
```

### Key Annotations

| Annotation | Purpose |
|------------|---------|
| `sh.brew.bottle.digest` | **Critical**: SHA256 of bottle tarball |
| `sh.brew.tab` | JSON-encoded Tab (INSTALL_RECEIPT.json content) |
| `org.opencontainers.image.title` | Formula name |
| `org.opencontainers.image.version` | Version string |
| `org.opencontainers.image.ref.name` | Full version with platform |

## Authentication

### Public Read Access

Homebrew uses a default token for public access:

```
Authorization: Bearer QQ==
```

`QQ==` is base64-encoded empty string.

### For Publishing (Write Access)

Use a GitHub Personal Access Token (PAT) with `write:packages` scope:

```bash
# Get token
echo $GITHUB_TOKEN | base64
# or
echo -n "username:ghp_xxxx" | base64
```

```
Authorization: Bearer <base64_encoded_token>
```

## Upload Process

### Step 1: Upload Blob (Bottle Tarball)

```bash
# 1. Initiate upload
UPLOAD_URL=$(curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  "https://ghcr.io/v2/user/bottles/gobottle/blobs/uploads/" \
  -D - | grep -i location | cut -d' ' -f2 | tr -d '\r')

# 2. Upload blob with digest
DIGEST="sha256:$(sha256sum gobottle-1.0.0.arm64_sonoma.bottle.tar.gz | cut -d' ' -f1)"

curl -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @gobottle-1.0.0.arm64_sonoma.bottle.tar.gz \
  "${UPLOAD_URL}&digest=${DIGEST}"
```

### Step 2: Upload Config Blob

OCI requires a config blob (can be empty for bottles):

```bash
# Empty config
echo -n '{}' > config.json
CONFIG_DIGEST="sha256:$(sha256sum config.json | cut -d' ' -f1)"

# Upload
curl -X POST ... # same as above with config.json
```

### Step 3: Push Manifest

```bash
MANIFEST='{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {
    "mediaType": "application/vnd.oci.image.config.v1+json",
    "digest": "'$CONFIG_DIGEST'",
    "size": 2
  },
  "layers": [{
    "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
    "digest": "'$DIGEST'",
    "size": '$SIZE'
  }],
  "annotations": {
    "sh.brew.bottle.digest": "'$DIGEST'"
  }
}'

curl -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/vnd.oci.image.manifest.v1+json" \
  -d "$MANIFEST" \
  "https://ghcr.io/v2/user/bottles/gobottle/manifests/1.0.0.arm64_sonoma"
```

## Using skopeo

Easier than raw HTTP:

```bash
# Create OCI layout directory
mkdir -p oci-image/blobs/sha256

# Copy bottle as blob
cp gobottle-1.0.0.arm64_sonoma.bottle.tar.gz oci-image/blobs/sha256/<sha256>

# Create oci-layout file
echo '{"imageLayoutVersion": "1.0.0"}' > oci-image/oci-layout

# Create index.json and manifest files...

# Push to GHCR
skopeo copy \
  --dest-creds="username:$GITHUB_TOKEN" \
  oci:oci-image:1.0.0.arm64_sonoma \
  docker://ghcr.io/user/bottles/gobottle:1.0.0.arm64_sonoma
```

## Go Implementation

### Using go-containerregistry

```go
import (
    "github.com/google/go-containerregistry/pkg/authn"
    "github.com/google/go-containerregistry/pkg/name"
    "github.com/google/go-containerregistry/pkg/v1"
    "github.com/google/go-containerregistry/pkg/v1/empty"
    "github.com/google/go-containerregistry/pkg/v1/mutate"
    "github.com/google/go-containerregistry/pkg/v1/remote"
    "github.com/google/go-containerregistry/pkg/v1/tarball"
    "github.com/google/go-containerregistry/pkg/v1/types"
)

func PublishBottle(bottlePath, repo, tag string) error {
    // Parse destination reference
    ref, err := name.ParseReference(fmt.Sprintf("%s:%s", repo, tag))
    if err != nil {
        return err
    }

    // Create layer from bottle tarball
    layer, err := tarball.LayerFromFile(bottlePath,
        tarball.WithMediaType(types.OCILayer),
    )
    if err != nil {
        return err
    }

    // Get layer digest for annotation
    digest, err := layer.Digest()
    if err != nil {
        return err
    }

    // Create image with layer
    img, err := mutate.AppendLayers(empty.Image, layer)
    if err != nil {
        return err
    }

    // Add annotations
    img = mutate.Annotations(img, map[string]string{
        "sh.brew.bottle.digest":           digest.String(),
        "org.opencontainers.image.title":   "gobottle",
        "org.opencontainers.image.version": "1.0.0",
    }).(v1.Image)

    // Push to registry
    auth := authn.FromConfig(authn.AuthConfig{
        Username: "username",
        Password: os.Getenv("GITHUB_TOKEN"),
    })

    return remote.Write(ref, img, remote.WithAuth(auth))
}
```

### Multi-Platform Index

```go
func PublishMultiPlatform(bottles []Bottle, repo string) error {
    var manifests []mutate.IndexAddendum

    for _, b := range bottles {
        img, err := createImage(b)
        if err != nil {
            return err
        }

        manifests = append(manifests, mutate.IndexAddendum{
            Add: img,
            Descriptor: v1.Descriptor{
                Platform: &v1.Platform{
                    Architecture: b.OCIArch(),
                    OS:           b.OCIOS(),
                },
                Annotations: map[string]string{
                    "sh.brew.bottle.digest":                b.SHA256,
                    "org.opencontainers.image.ref.name":    b.Tag(),
                },
            },
        })
    }

    // Create index
    idx := mutate.AppendManifests(empty.Index, manifests...)

    // Push
    ref, _ := name.ParseReference(repo + ":latest")
    return remote.WriteIndex(ref, idx, remote.WithAuth(auth))
}
```

## Formula Integration

### Bottle Block from Published Bottles

After publishing, update formula with bottle checksums:

```ruby
bottle do
  root_url "https://ghcr.io/v2/user/bottles/gobottle"
  sha256 cellar: :any_skip_relocation, arm64_sonoma:  "abc123..."
  sha256 cellar: :any_skip_relocation, sonoma:        "def456..."
  sha256 cellar: :any_skip_relocation, x86_64_linux:  "ghi789..."
end
```

### Root URL Format

| Format | Example |
|--------|---------|
| GHCR | `https://ghcr.io/v2/user/bottles/formula` |
| GitHub Releases | `https://github.com/user/repo/releases/download/v1.0.0` |
| Custom S3 | `https://bottles.example.com` |

## Fetching Bottles

### Using go-containerregistry

```go
func FetchBottle(repo, tag string) (io.ReadCloser, error) {
    ref, err := name.ParseReference(fmt.Sprintf("%s:%s", repo, tag))
    if err != nil {
        return nil, err
    }

    img, err := remote.Image(ref, remote.WithAuth(authn.Anonymous))
    if err != nil {
        return nil, err
    }

    layers, err := img.Layers()
    if err != nil || len(layers) == 0 {
        return nil, errors.New("no layers")
    }

    return layers[0].Compressed()
}
```

### Using curl

```bash
# 1. Get token
TOKEN=$(curl -s "https://ghcr.io/token?scope=repository:user/bottles/gobottle:pull" | jq -r .token)

# 2. Get manifest
MANIFEST=$(curl -s \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/vnd.oci.image.manifest.v1+json" \
  "https://ghcr.io/v2/user/bottles/gobottle/manifests/1.0.0.arm64_sonoma")

# 3. Extract blob digest
BLOB=$(echo "$MANIFEST" | jq -r '.layers[0].digest')

# 4. Download blob
curl -L \
  -H "Authorization: Bearer $TOKEN" \
  "https://ghcr.io/v2/user/bottles/gobottle/blobs/$BLOB" \
  -o gobottle.bottle.tar.gz
```

## Visibility and Permissions

### Package Visibility

GHCR packages can be:
- **Private**: Only accessible with authentication
- **Public**: Accessible with default `QQ==` token

### Setting Visibility

```bash
# Via GitHub UI: Settings > Packages > Package settings > Danger Zone

# Or via API
gh api -X PATCH /user/packages/container/gobottle \
  -f visibility=public
```

### Repository Connection

For automatic visibility inheritance, connect package to repository:

1. Go to package settings on GitHub
2. Connect to a repository
3. Package inherits repository visibility

## Error Handling

### Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| `UNAUTHORIZED` | Missing or invalid token | Check GITHUB_TOKEN |
| `DENIED` | No write permission | Ensure `write:packages` scope |
| `NAME_UNKNOWN` | Package doesn't exist | Push creates it |
| `MANIFEST_UNKNOWN` | Tag doesn't exist | Check tag name |
| `BLOB_UNKNOWN` | Layer not uploaded | Upload blob first |

### Retry Strategy

```go
func withRetry(fn func() error, maxAttempts int) error {
    var lastErr error
    for i := 0; i < maxAttempts; i++ {
        if err := fn(); err == nil {
            return nil
        } else {
            lastErr = err
            time.Sleep(time.Second * time.Duration(i+1))
        }
    }
    return lastErr
}
```

## Best Practices

1. **Use go-containerregistry** - Handles OCI complexity correctly
2. **Include annotations** - `sh.brew.bottle.digest` is required
3. **Multi-platform index** - Easier for users than separate tags
4. **Reproducible digests** - Same bottle = same digest
5. **Verify after push** - Pull and compare digest
6. **Public visibility** - For open-source projects
7. **Connect to repo** - For visibility inheritance
