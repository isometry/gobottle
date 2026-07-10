package bottle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// archiveResult carries the digests of a written bottle tarball.
type archiveResult struct {
	SHA256             string // digest of the .tar.gz (== formula sha256 == OCI layer digest)
	UncompressedSHA256 string // digest of the raw tar stream (== OCI diff_id)
	UncompressedSize   int64  // raw tar size (basis for installed_size)
}

// writeTarGz writes a reproducible bottle tarball: entries (including parent
// directories) sorted lexically, uid/gid 0, no user/group names, all
// timestamps fixed to mtime, gzip header without name or modtime. Identical
// inputs always produce byte-identical output, so bottle SHA256s are stable
// across runs — the property brew's blob-addressed pours and idempotent
// re-pushes depend on.
func writeTarGz(outputPath string, files map[string]string, mtime time.Time) (*archiveResult, error) {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	gzw := gzip.NewWriter(outFile)

	tarHash := sha256.New()
	counter := &countingWriter{}
	tw := tar.NewWriter(io.MultiWriter(gzw, tarHash, counter))

	mtime = mtime.UTC().Truncate(time.Second)

	// Collect implicit parent directories.
	dirs := map[string]bool{}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
		for dir := path.Dir(name); dir != "." && dir != "/"; dir = path.Dir(dir) {
			dirs[dir] = true
		}
	}
	for dir := range dirs {
		names = append(names, dir+"/")
	}
	sort.Strings(names)

	for _, name := range names {
		if strings.HasSuffix(name, "/") {
			hdr := &tar.Header{
				Typeflag: tar.TypeDir,
				Name:     name,
				Mode:     0755,
				ModTime:  mtime,
				Format:   tar.FormatPAX,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return nil, fmt.Errorf("failed to write dir header %s: %w", name, err)
			}
			continue
		}

		localPath := files[name]
		info, err := os.Lstat(localPath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat %s: %w", localPath, err)
		}

		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(localPath); err != nil {
				return nil, fmt.Errorf("failed to read symlink %s: %w", localPath, err)
			}
		}

		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return nil, fmt.Errorf("failed to create tar header for %s: %w", localPath, err)
		}
		hdr.Name = name
		hdr.ModTime = mtime
		hdr.AccessTime = time.Time{}
		hdr.ChangeTime = time.Time{}
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = ""
		hdr.Gname = ""
		hdr.Format = tar.FormatPAX

		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("failed to write tar header %s: %w", name, err)
		}

		if info.Mode().IsRegular() {
			f, err := os.Open(localPath)
			if err != nil {
				return nil, fmt.Errorf("failed to open %s: %w", localPath, err)
			}
			_, err = io.Copy(tw, f)
			f.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to write %s: %w", localPath, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize tar: %w", err)
	}
	if err := gzw.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize gzip: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close output: %w", err)
	}

	gzSHA, err := fileSHA256(outputPath)
	if err != nil {
		return nil, err
	}

	return &archiveResult{
		SHA256:             gzSHA,
		UncompressedSHA256: hex.EncodeToString(tarHash.Sum(nil)),
		UncompressedSize:   counter.n,
	}, nil
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
