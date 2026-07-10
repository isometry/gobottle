// Package oci publishes Homebrew bottles to an OCI registry using the exact
// conventions brew expects when pouring from GHCR (Homebrew's
// github_packages.rb is the reference implementation):
//
//   - image path:   <host>/<root path>/<formula> — the formula name is the
//     last path element because brew appends it to the formula's root_url
//   - one OCI *index* per version, tagged "version[-rebuild]" (brew always
//     parses the manifest response as an index, even for a single platform)
//   - index children carry sh.brew.bottle.digest, sh.brew.tab and
//     org.opencontainers.image.ref.name="version.tag[.rebuild]" annotations —
//     all three are read by brew at install time
//   - the layer blob is the bottle tar.gz byte-for-byte, so its digest equals
//     the sha256 in the formula's bottle block
package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/isometry/gobottle/internal/bottle"
)

// PublishOptions holds metadata rendered into OCI annotations.
type PublishOptions struct {
	SourceURL   string    // org.opencontainers.image.source (links the GHCR package to a repo)
	Homepage    string    // org.opencontainers.image.url
	License     string    // org.opencontainers.image.licenses + sh.brew.license
	Description string    // org.opencontainers.image.description
	Created     time.Time // org.opencontainers.image.created; zero omits it (keeps pushes deterministic)
}

// Publisher pushes bottles for a single formula to an OCI registry.
type Publisher struct {
	repo name.Repository
	auth *Authenticator
	opts PublishOptions
}

// Result describes one pushed (or to-be-pushed) index.
type Result struct {
	Tag     string // e.g. "1.2.3" or "1.2.3-1"
	Ref     string // full tagged reference
	Digest  string // index digest (empty on dry runs)
	Bottles []*bottle.Bottle
}

// ImageFormulaName sanitizes a formula name for use as an OCI path element,
// mirroring GitHubPackages.image_formula_name: "@" -> "/", "+" -> "x".
func ImageFormulaName(formula string) string {
	return strings.ReplaceAll(strings.ReplaceAll(formula, "@", "/"), "+", "x")
}

// NewPublisher creates a publisher for <host>/<rootPath>/<formula>.
// rootPath is everything between the registry host and the formula name
// (e.g. "myorg/tap"); brew's root_url is then "https://<host>/v2/<rootPath>".
func NewPublisher(host, rootPath, formula, token string, opts PublishOptions) (*Publisher, error) {
	if host == "" || rootPath == "" || formula == "" {
		return nil, fmt.Errorf("registry host, root path and formula are all required")
	}
	if token == "" {
		return nil, fmt.Errorf("registry token is required")
	}

	repoPath := strings.ToLower(fmt.Sprintf("%s/%s/%s", host, rootPath, ImageFormulaName(formula)))
	repo, err := name.NewRepository(repoPath)
	if err != nil {
		return nil, fmt.Errorf("invalid repository %s: %w", repoPath, err)
	}

	return &Publisher{
		repo: repo,
		auth: NewAuthenticator(repo.RegistryStr(), token),
		opts: opts,
	}, nil
}

// Repository returns the full image path (host/rootPath/formula).
func (p *Publisher) Repository() string {
	return p.repo.Name()
}

// Push publishes all bottles, one OCI index per version[-rebuild] tag.
// If an index already exists at the tag, other-platform children are
// preserved (brew pr-upload's --keep-old behavior) and matching platforms
// are replaced.
func (p *Publisher) Push(ctx context.Context, bottles []*bottle.Bottle, log func(string)) ([]Result, error) {
	if len(bottles) == 0 {
		return nil, fmt.Errorf("no bottles to push")
	}
	if log == nil {
		log = func(string) {}
	}

	groups := map[string][]*bottle.Bottle{}
	var tags []string
	for _, b := range bottles {
		tag := b.VersionRebuild()
		if _, seen := groups[tag]; !seen {
			tags = append(tags, tag)
		}
		groups[tag] = append(groups[tag], b)
	}

	var results []Result
	for _, tag := range tags {
		res, err := p.pushIndex(ctx, tag, groups[tag], log)
		if err != nil {
			return results, err
		}
		results = append(results, *res)
	}

	return results, nil
}

// pushIndex assembles and pushes the OCI index for one version tag.
func (p *Publisher) pushIndex(ctx context.Context, tag string, bottles []*bottle.Bottle, log func(string)) (*Result, error) {
	tagRef := p.repo.Tag(tag)
	remoteOpts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(p.auth.Keychain()),
	}

	// Start from the existing index when present so other platforms survive
	// a partial re-publish; drop children we are about to replace.
	var idx v1.ImageIndex = mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
	if existing, err := remote.Index(tagRef, remoteOpts...); err == nil {
		replaced := map[string]bool{}
		for _, b := range bottles {
			replaced[b.RefName()] = true
		}
		idx = mutate.RemoveManifests(existing, func(desc v1.Descriptor) bool {
			return replaced[desc.Annotations["org.opencontainers.image.ref.name"]]
		})
		log(fmt.Sprintf("appending to existing index %s", tagRef.String()))
	}

	adds := make([]mutate.IndexAddendum, 0, len(bottles))
	for _, b := range bottles {
		img, annotations, platform, err := p.bottleImage(b)
		if err != nil {
			return nil, err
		}
		adds = append(adds, mutate.IndexAddendum{
			Add: img,
			Descriptor: v1.Descriptor{
				Platform:    platform,
				Annotations: annotations,
			},
		})
	}

	idx = mutate.AppendManifests(idx, adds...)
	idx = mutate.Annotations(idx, p.indexAnnotations(tag, bottles[0])).(v1.ImageIndex)

	if err := remote.WriteIndex(tagRef, idx, remoteOpts...); err != nil {
		return nil, fmt.Errorf("failed to push index %s: %w", tagRef.String(), err)
	}

	digest, err := idx.Digest()
	if err != nil {
		return nil, fmt.Errorf("failed to compute index digest: %w", err)
	}

	log(fmt.Sprintf("pushed %s (%d platforms)", tagRef.String(), len(bottles)))

	return &Result{
		Tag:     tag,
		Ref:     tagRef.String(),
		Digest:  digest.String(),
		Bottles: bottles,
	}, nil
}

// bottleImage builds the per-platform OCI image: a real config blob
// (architecture/os/os.version + rootfs diff_ids, as brew publishes) plus the
// bottle tarball as its single layer.
func (p *Publisher) bottleImage(b *bottle.Bottle) (v1.Image, map[string]string, *v1.Platform, error) {
	layer, err := newBottleLayer(b)
	if err != nil {
		return nil, nil, nil, err
	}

	platform := &v1.Platform{
		OS:           b.Platform.OS,
		Architecture: ociArch(b.Platform.Arch),
	}
	if b.Platform.OS == "darwin" && b.Platform.OSVersionMajor > 0 {
		platform.OSVersion = fmt.Sprintf("macOS %d", b.Platform.OSVersionMajor)
	}

	img := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON)

	img, err = mutate.ConfigFile(img, &v1.ConfigFile{
		Architecture: platform.Architecture,
		OS:           platform.OS,
		OSVersion:    platform.OSVersion,
		RootFS:       v1.RootFS{Type: "layers"},
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to set config for %s: %w", b.BottleName(), err)
	}

	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to append layer for %s: %w", b.BottleName(), err)
	}

	annotations, err := p.bottleAnnotations(b, layer.size)
	if err != nil {
		return nil, nil, nil, err
	}
	img = mutate.Annotations(img, annotations).(v1.Image)

	return img, annotations, platform, nil
}

// bottleAnnotations builds the annotation set brew reads from the index's
// child descriptors (and which we mirror onto the image manifests, as brew
// itself does).
func (p *Publisher) bottleAnnotations(b *bottle.Bottle, fileSize int64) (map[string]string, error) {
	tabJSON, err := b.Tab.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tab for %s: %w", b.BottleName(), err)
	}

	annotations := map[string]string{
		"org.opencontainers.image.ref.name":    b.RefName(),
		"org.opencontainers.image.title":       b.BottleName(),
		"org.opencontainers.image.version":     b.Version,
		"org.opencontainers.image.description": fmt.Sprintf("Homebrew bottle for %s %s (%s)", b.Formula, b.Version, b.Platform.Tag),
		"sh.brew.bottle.digest":                b.SHA256,
		"sh.brew.bottle.size":                  strconv.FormatInt(fileSize, 10),
		"sh.brew.bottle.installed_size":        strconv.FormatInt(b.UncompressedSize, 10),
		"sh.brew.tab":                          string(tabJSON),
	}
	p.addCommonAnnotations(annotations)
	return annotations, nil
}

// indexAnnotations builds the index-level annotation set.
func (p *Publisher) indexAnnotations(tag string, sample *bottle.Bottle) map[string]string {
	annotations := map[string]string{
		"com.github.package.type":              "homebrew_bottle",
		"org.opencontainers.image.ref.name":    tag,
		"org.opencontainers.image.title":       fmt.Sprintf("%s bottles", sample.Formula),
		"org.opencontainers.image.version":     sample.Version,
		"org.opencontainers.image.description": fmt.Sprintf("Homebrew bottles for %s %s", sample.Formula, sample.Version),
	}
	p.addCommonAnnotations(annotations)
	return annotations
}

func (p *Publisher) addCommonAnnotations(annotations map[string]string) {
	if p.opts.Description != "" {
		annotations["org.opencontainers.image.description"] = p.opts.Description
	}
	if p.opts.SourceURL != "" {
		annotations["org.opencontainers.image.source"] = p.opts.SourceURL
	}
	if p.opts.Homepage != "" {
		annotations["org.opencontainers.image.url"] = p.opts.Homepage
	}
	if p.opts.License != "" {
		annotations["org.opencontainers.image.licenses"] = p.opts.License
		annotations["sh.brew.license"] = p.opts.License
	}
	if !p.opts.Created.IsZero() {
		annotations["org.opencontainers.image.created"] = p.opts.Created.UTC().Format(time.RFC3339)
	}
}

// ociArch maps Go arch names to OCI architecture values.
func ociArch(arch string) string {
	switch arch {
	case "amd64", "arm64":
		return arch
	default:
		return arch
	}
}

// AnonymouslyAccessible reports whether the given tag can be fetched without
// credentials — the way brew pulls (Bearer QQ==). A false result almost
// always means the GHCR package is still private and must be made public
// before anyone can `brew install` from it.
func (p *Publisher) AnonymouslyAccessible(ctx context.Context, tag string) (bool, error) {
	_, err := remote.Head(p.repo.Tag(tag), remote.WithContext(ctx))
	if err == nil {
		return true, nil
	}
	var terr *transport.Error
	if errors.As(err, &terr) {
		if terr.StatusCode == http.StatusUnauthorized || terr.StatusCode == http.StatusForbidden ||
			terr.StatusCode == http.StatusNotFound {
			// GHCR reports private packages as 401/403/404 to anonymous pulls.
			return false, nil
		}
	}
	return false, err
}
