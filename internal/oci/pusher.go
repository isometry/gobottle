package oci

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/isometry/gobottle/internal/bottle"
)

const (
	// MediaTypes as required by Homebrew/OCI spec
	ConfigMediaType = types.OCIConfigJSON          // "application/vnd.oci.image.config.v1+json"
	LayerMediaType  = types.OCILayer              // "application/vnd.oci.image.layer.v1.tar+gzip"
)

// Pusher pushes bottles to an OCI registry
type Pusher struct {
	auth     *Authenticator
	registry string // e.g., "ghcr.io"
	owner    string // e.g., "myorg"
	pkg      string // package name
}

// NewPusher creates a new OCI pusher
func NewPusher(host, owner, pkg, token string) (*Pusher, error) {
	if host == "" {
		return nil, fmt.Errorf("host cannot be empty")
	}
	if owner == "" {
		return nil, fmt.Errorf("owner cannot be empty")
	}
	if pkg == "" {
		return nil, fmt.Errorf("package name cannot be empty")
	}
	if token == "" {
		return nil, fmt.Errorf("token cannot be empty")
	}

	return &Pusher{
		auth:     NewAuthenticator(host, token),
		registry: host,
		owner:    owner,
		pkg:      pkg,
	}, nil
}

// Push pushes a bottle to the registry
// Returns the full image reference (e.g., ghcr.io/myorg/myapp:1.0.0)
func (p *Pusher) Push(ctx context.Context, b *bottle.Bottle) (string, error) {
	// Build the image reference
	ref := p.GetFullReference(b)

	// Parse the reference
	imageRef, err := name.ParseReference(ref)
	if err != nil {
		return "", fmt.Errorf("failed to parse reference %s: %w", ref, err)
	}

	// Create OCI image from bottle
	img, err := p.createImage(b)
	if err != nil {
		return "", fmt.Errorf("failed to create OCI image for %s: %w", b.BottleName(), err)
	}

	// Push the image
	err = remote.Write(imageRef, img, remote.WithContext(ctx), remote.WithAuthFromKeychain(p.auth.Keychain()))
	if err != nil {
		return "", fmt.Errorf("failed to push image %s: %w", ref, err)
	}

	return ref, nil
}

// PushAll pushes multiple bottles and creates an index
// This is useful when you have bottles for multiple platforms
func (p *Pusher) PushAll(ctx context.Context, bottles []*bottle.Bottle) error {
	if len(bottles) == 0 {
		return fmt.Errorf("no bottles to push")
	}

	// Group bottles by version to create multi-platform manifests
	versionGroups := make(map[string][]*bottle.Bottle)
	for _, b := range bottles {
		versionGroups[b.Version] = append(versionGroups[b.Version], b)
	}

	// Push each version group
	for version, groupBottles := range versionGroups {
		if len(groupBottles) == 1 {
			// Single platform - just push the image
			ref, err := p.Push(ctx, groupBottles[0])
			if err != nil {
				return err
			}
			fmt.Printf("Pushed %s\n", ref)
		} else {
			// Multiple platforms - create an index
			err := p.pushIndex(ctx, version, groupBottles)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// pushIndex creates and pushes a multi-platform index for bottles of the same version
func (p *Pusher) pushIndex(ctx context.Context, version string, bottles []*bottle.Bottle) error {
	// Build the reference for the index
	ref := fmt.Sprintf("%s/%s/%s:%s", p.registry, p.owner, p.pkg, version)
	indexRef, err := name.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("failed to parse index reference %s: %w", ref, err)
	}

	// Create index
	adds := make([]mutate.IndexAddendum, 0, len(bottles))

	for _, b := range bottles {
		// Create image for this bottle
		img, err := p.createImage(b)
		if err != nil {
			return fmt.Errorf("failed to create image for %s: %w", b.BottleName(), err)
		}

		// Add to index with platform descriptor
		adds = append(adds, mutate.IndexAddendum{
			Add: img,
			Descriptor: v1.Descriptor{
				Platform: p.getPlatform(b),
				Annotations: map[string]string{
					"org.opencontainers.image.title": b.BottleName(),
					"sh.brew.bottle.digest":           b.SHA256,
				},
			},
		})
	}

	// Create the index from scratch
	idx := mutate.AppendManifests(empty.Index, adds...)

	// Push the index
	err = remote.WriteIndex(indexRef, idx, remote.WithContext(ctx), remote.WithAuthFromKeychain(p.auth.Keychain()))
	if err != nil {
		return fmt.Errorf("failed to push index %s: %w", ref, err)
	}

	fmt.Printf("Pushed multi-platform index %s (%d platforms)\n", ref, len(bottles))
	return nil
}

// createImage creates an OCI image from a bottle
func (p *Pusher) createImage(b *bottle.Bottle) (v1.Image, error) {
	// Start with an empty image with the correct config media type
	img := empty.Image

	// Set the config media type to OCI
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, ConfigMediaType)

	// Load the bottle tarball as a layer
	layer, err := tarball.LayerFromFile(b.Path, tarball.WithMediaType(LayerMediaType))
	if err != nil {
		return nil, fmt.Errorf("failed to create layer from %s: %w", b.Path, err)
	}

	// Add the layer to the image
	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		return nil, fmt.Errorf("failed to append layer: %w", err)
	}

	// Add annotations to the image config
	img = mutate.Annotations(img, map[string]string{
		"org.opencontainers.image.title":       b.BottleName(),
		"org.opencontainers.image.version":     b.Version,
		"org.opencontainers.image.description": fmt.Sprintf("Homebrew bottle for %s %s (%s)", b.Formula, b.Version, b.Platform.Tag),
		"sh.brew.bottle.digest":                 b.SHA256,
		"sh.brew.bottle.platform":               b.Platform.Tag,
	}).(v1.Image)

	return img, nil
}

// getPlatform converts a bottle platform to OCI platform descriptor
func (p *Pusher) getPlatform(b *bottle.Bottle) *v1.Platform {
	// Map platform OS and arch to OCI platform
	platform := &v1.Platform{
		OS:           b.Platform.OS,
		Architecture: p.mapArchitecture(b.Platform.Arch),
	}

	// Add OS version for macOS
	if b.Platform.OS == "darwin" && b.Platform.OSVersion != "" {
		platform.OSVersion = b.Platform.OSVersion
	}

	return platform
}

// mapArchitecture maps Go architecture to OCI architecture
func (p *Pusher) mapArchitecture(arch string) string {
	switch arch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return arch
	}
}

// ValidateReference checks if a reference is valid
func ValidateReference(ref string) error {
	_, err := name.ParseReference(ref)
	return err
}

// GetFullReference returns the full OCI reference for a bottle
func (p *Pusher) GetFullReference(b *bottle.Bottle) string {
	return fmt.Sprintf("%s/%s/%s:%s", p.registry, p.owner, p.pkg, b.Version)
}

// GetDigest retrieves the digest of a pushed image
func (p *Pusher) GetDigest(ctx context.Context, ref string) (string, error) {
	imageRef, err := name.ParseReference(ref)
	if err != nil {
		return "", fmt.Errorf("failed to parse reference %s: %w", ref, err)
	}

	desc, err := remote.Get(imageRef, remote.WithContext(ctx), remote.WithAuthFromKeychain(p.auth.Keychain()))
	if err != nil {
		return "", fmt.Errorf("failed to get descriptor for %s: %w", ref, err)
	}

	return desc.Digest.String(), nil
}

// ImageExists checks if an image exists in the registry
func (p *Pusher) ImageExists(ctx context.Context, ref string) (bool, error) {
	imageRef, err := name.ParseReference(ref)
	if err != nil {
		return false, fmt.Errorf("failed to parse reference %s: %w", ref, err)
	}

	_, err = remote.Head(imageRef, remote.WithContext(ctx), remote.WithAuthFromKeychain(p.auth.Keychain()))
	if err != nil {
		// Check if it's a "not found" error
		if err.Error() == "MANIFEST_UNKNOWN" {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// GetImageAnnotations retrieves annotations from a pushed image
func (p *Pusher) GetImageAnnotations(ctx context.Context, ref string) (map[string]string, error) {
	imageRef, err := name.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to parse reference %s: %w", ref, err)
	}

	img, err := remote.Image(imageRef, remote.WithContext(ctx), remote.WithAuthFromKeychain(p.auth.Keychain()))
	if err != nil {
		return nil, fmt.Errorf("failed to get image %s: %w", ref, err)
	}

	manifest, err := img.Manifest()
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	return manifest.Annotations, nil
}

// PushWithProgress pushes a bottle with progress reporting
func (p *Pusher) PushWithProgress(ctx context.Context, b *bottle.Bottle, progressFn func(update string)) (string, error) {
	if progressFn != nil {
		progressFn(fmt.Sprintf("Creating OCI image for %s", filepath.Base(b.Path)))
	}

	// Build the image reference
	ref := p.GetFullReference(b)

	// Parse the reference
	imageRef, err := name.ParseReference(ref)
	if err != nil {
		return "", fmt.Errorf("failed to parse reference %s: %w", ref, err)
	}

	// Create OCI image from bottle
	img, err := p.createImage(b)
	if err != nil {
		return "", fmt.Errorf("failed to create OCI image for %s: %w", b.BottleName(), err)
	}

	if progressFn != nil {
		progressFn(fmt.Sprintf("Pushing %s to %s", b.BottleName(), ref))
	}

	// Push the image
	err = remote.Write(imageRef, img, remote.WithContext(ctx), remote.WithAuthFromKeychain(p.auth.Keychain()))
	if err != nil {
		return "", fmt.Errorf("failed to push image %s: %w", ref, err)
	}

	if progressFn != nil {
		progressFn(fmt.Sprintf("Successfully pushed %s", ref))
	}

	return ref, nil
}
