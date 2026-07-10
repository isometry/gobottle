package oci

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/isometry/gobottle/internal/bottle"
)

// bottleLayer exposes a bottle tarball as an OCI layer whose compressed
// digest is byte-identical to the bottle file (brew addresses the blob by the
// formula's sha256) and whose diff_id is the digest of the raw tar stream.
type bottleLayer struct {
	path   string
	digest v1.Hash
	diffID v1.Hash
	size   int64
}

func newBottleLayer(b *bottle.Bottle) (*bottleLayer, error) {
	size, err := b.FileSize()
	if err != nil {
		return nil, fmt.Errorf("failed to stat bottle %s: %w", b.Path, err)
	}
	if b.SHA256 == "" || b.UncompressedSHA256 == "" {
		return nil, fmt.Errorf("bottle %s is missing digests", b.Path)
	}
	return &bottleLayer{
		path:   b.Path,
		digest: v1.Hash{Algorithm: "sha256", Hex: b.SHA256},
		diffID: v1.Hash{Algorithm: "sha256", Hex: b.UncompressedSHA256},
		size:   size,
	}, nil
}

func (l *bottleLayer) Digest() (v1.Hash, error)          { return l.digest, nil }
func (l *bottleLayer) DiffID() (v1.Hash, error)          { return l.diffID, nil }
func (l *bottleLayer) Size() (int64, error)              { return l.size, nil }
func (l *bottleLayer) MediaType() (types.MediaType, error) {
	return types.OCILayer, nil
}

func (l *bottleLayer) Compressed() (io.ReadCloser, error) {
	return os.Open(l.path)
}

func (l *bottleLayer) Uncompressed() (io.ReadCloser, error) {
	f, err := os.Open(l.path)
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &gzipReadCloser{gz: gz, f: f}, nil
}

type gzipReadCloser struct {
	gz *gzip.Reader
	f  *os.File
}

func (r *gzipReadCloser) Read(p []byte) (int, error) { return r.gz.Read(p) }

func (r *gzipReadCloser) Close() error {
	gzErr := r.gz.Close()
	fErr := r.f.Close()
	if gzErr != nil {
		return gzErr
	}
	return fErr
}
