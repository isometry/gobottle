package util

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractArchive extracts an archive to a destination directory,
// dispatching on the file extension. The supported set must stay in sync
// with artifact.IsArchive so discovery never accepts what extraction
// cannot handle.
func ExtractArchive(archivePath, destDir string) error {
	name := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return ExtractTarGz(archivePath, destDir)
	case strings.HasSuffix(name, ".zip"):
		return ExtractZip(archivePath, destDir)
	case strings.HasSuffix(name, ".tar.bz2"):
		return ExtractTarBz2(archivePath, destDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Base(archivePath))
	}
}

// ExtractTarGz extracts a .tar.gz archive to a destination directory
func ExtractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	return extractTar(gzr, destDir)
}

// ExtractTarBz2 extracts a .tar.bz2 archive to a destination directory
func ExtractTarBz2(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer f.Close()

	return extractTar(bzip2.NewReader(f), destDir)
}

// entryRelPath normalises an archive member name to a path relative to the
// extraction root, suitable for os.Root. A leading "/" (from `tar -P`) is
// stripped, and "." is returned for a root entry such as "./". Names that
// would climb out of the root are rejected.
func entryRelPath(name string) (string, error) {
	rel := filepath.Clean(strings.TrimLeft(filepath.FromSlash(name), string(os.PathSeparator)))
	if rel != "." && !filepath.IsLocal(rel) {
		return "", fmt.Errorf("invalid file path: %s", name)
	}
	return rel, nil
}

// writeSymlink creates a symlink at rel, creating parent directories as
// needed. The target is stored verbatim and not validated.
func writeSymlink(root *os.Root, rel, linkname string) error {
	if err := root.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}
	if err := root.Symlink(linkname, rel); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}
	return nil
}

// extractTar walks a tar stream, writing entries under destDir.
//
// Every write goes through os.Root, which confines all filesystem access to
// destDir at the OS level as each path component (including symlinks) is
// actually resolved. That covers writes that pass through a link created
// earlier in the archive, however the chain of links is arranged. Symlinks
// are created as-is: os.Root does not constrain where a link points, only
// what we write through it, so consumers must resolve links with their own
// containment check (as bottle.ResolveBinary does) before trusting them.
func extractTar(r io.Reader, destDir string) error {
	// os.Root requires destDir to already exist.
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return fmt.Errorf("failed to open destination directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		relName, err := entryRelPath(header.Name)
		if err != nil {
			return err
		}
		// A root entry such as "./" is just destDir, which already exists.
		if relName == "." {
			if header.Typeflag == tar.TypeDir {
				continue
			}
			return fmt.Errorf("invalid file path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(relName, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// Create parent directory if needed
			if err := root.MkdirAll(filepath.Dir(relName), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Create file. Perm() drops setuid/setgid/sticky and any type
			// bits, which os.Root.OpenFile refuses.
			outFile, err := root.OpenFile(relName, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode).Perm())
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			outFile.Close()

		case tar.TypeSymlink:
			if err := writeSymlink(root, relName, header.Linkname); err != nil {
				return err
			}
		}
	}

	return nil
}

// CreateTarGz creates a .tar.gz archive from a map of archive paths to local paths
// The files map is: archivePath -> localPath (e.g., "myapp/1.0.0/bin/myapp" -> "/tmp/myapp")
func CreateTarGz(outputPath string, files map[string]string) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	gzw := gzip.NewWriter(outFile)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	for archivePath, localPath := range files {
		if err := addToTar(tw, archivePath, localPath); err != nil {
			return fmt.Errorf("failed to add %s: %w", localPath, err)
		}
	}

	return nil
}

// addToTar adds a file or directory to a tar writer
func addToTar(tw *tar.Writer, archivePath, localPath string) error {
	info, err := os.Lstat(localPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Handle symlinks
	var link string
	if info.Mode()&os.ModeSymlink != 0 {
		link, err = os.Readlink(localPath)
		if err != nil {
			return fmt.Errorf("failed to read symlink: %w", err)
		}
	}

	header, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return fmt.Errorf("failed to create tar header: %w", err)
	}

	// Use the archive path instead of the local path
	header.Name = archivePath

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	// If it's a regular file, write its contents
	if info.Mode().IsRegular() {
		file, err := os.Open(localPath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer file.Close()

		if _, err := io.Copy(tw, file); err != nil {
			return fmt.Errorf("failed to write file contents: %w", err)
		}
	}

	return nil
}

// ExtractZip extracts a .zip archive to a destination directory. Like
// extractTar, writes are confined to destDir by os.Root and symlink entries
// are created as-is.
func ExtractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip archive: %w", err)
	}
	defer r.Close()

	// os.Root requires destDir to already exist.
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return fmt.Errorf("failed to open destination directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	for _, f := range r.File {
		relName, err := entryRelPath(f.Name)
		if err != nil {
			return err
		}
		if err := extractZipEntry(root, f, relName); err != nil {
			return err
		}
	}

	return nil
}

// maxZipLinkTarget caps how much of a symlink entry's body is read as its
// target, well above PATH_MAX.
const maxZipLinkTarget = 4096

// extractZipEntry writes one zip member to relName under root.
func extractZipEntry(root *os.Root, f *zip.File, relName string) error {
	mode := f.Mode()

	// A root entry such as "./" is just destDir, which already exists.
	if relName == "." {
		if mode.IsDir() {
			return nil
		}
		return fmt.Errorf("invalid file path: %s", f.Name)
	}

	if mode.IsDir() {
		if err := root.MkdirAll(relName, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
		return nil
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in archive: %w", err)
	}
	defer rc.Close()

	// A symlink entry's body is its target.
	if mode&os.ModeSymlink != 0 {
		linkname, err := io.ReadAll(io.LimitReader(rc, maxZipLinkTarget))
		if err != nil {
			return fmt.Errorf("failed to read symlink target: %w", err)
		}
		return writeSymlink(root, relName, string(linkname))
	}

	// Create parent directory if needed
	if err := root.MkdirAll(filepath.Dir(relName), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Create file
	outFile, err := root.OpenFile(relName, os.O_CREATE|os.O_RDWR|os.O_TRUNC, mode.Perm())
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, rc); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}
