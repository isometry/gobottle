package util

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// openExtractRoot creates destDir if needed and opens it as an os.Root.
func openExtractRoot(destDir string) (*os.Root, error) {
	// os.Root requires destDir to already exist.
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}
	root, err := os.OpenRoot(destDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open destination directory: %w", err)
	}
	return root, nil
}

// isRootEntry reports whether rel (from entryRelPath) is the extraction root
// itself, as for a "./" member. That is only valid for a directory entry,
// since destDir already exists; anything else is rejected.
func isRootEntry(rel, name string, isDir bool) (bool, error) {
	if rel != "." {
		return false, nil
	}
	if !isDir {
		return false, fmt.Errorf("invalid file path: %s", name)
	}
	return true, nil
}

// prepareEntry readies rel for a new non-directory entry: it creates parent
// directories and removes whatever already exists there, so a later member
// replaces an earlier one of the same name. Removal unlinks a symlink itself,
// never its target, which is what stops a file following a link from being
// written through it.
func prepareEntry(root *os.Root, rel string) error {
	if err := root.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}
	info, err := root.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to stat existing entry: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot replace directory with non-directory: %s", rel)
	}
	if err := root.Remove(rel); err != nil {
		return fmt.Errorf("failed to replace existing entry: %w", err)
	}
	return nil
}

// writeFile writes r to a new regular file at rel with permissions perm,
// replacing any existing entry. The mode is set explicitly afterwards so the
// process umask does not alter it.
func writeFile(root *os.Root, rel string, perm os.FileMode, r io.Reader) error {
	if err := prepareEntry(root, rel); err != nil {
		return err
	}
	f, err := root.OpenFile(rel, os.O_CREATE|os.O_WRONLY|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to write file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	if err := root.Chmod(rel, perm); err != nil {
		return fmt.Errorf("failed to set file mode: %w", err)
	}
	return nil
}

// writeSymlink creates a symlink at rel, replacing any existing entry. The
// target is stored verbatim and not validated.
func writeSymlink(root *os.Root, rel, linkname string) error {
	if err := prepareEntry(root, rel); err != nil {
		return err
	}
	if err := root.Symlink(linkname, rel); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}
	return nil
}

// writeHardlink creates a hardlink at rel to the existing entry oldRel,
// replacing any existing entry at rel. Both paths stay inside root.
func writeHardlink(root *os.Root, oldRel, rel string) error {
	if err := prepareEntry(root, rel); err != nil {
		return err
	}
	if err := root.Link(oldRel, rel); err != nil {
		return fmt.Errorf("failed to create hardlink: %w", err)
	}
	return nil
}

// extractTar walks a tar stream, writing entries under destDir.
//
// Every write goes through os.Root, which confines all filesystem access to
// destDir at the OS level as each path component (including symlinks) is
// actually resolved. That covers writes that pass through a link created
// earlier in the archive, however the chain of links is arranged. A later
// member with the same name as an earlier one replaces it (a link is removed,
// never written through). Symlinks are created as-is: os.Root does not
// constrain where a link points, only what we write through it, so consumers
// must resolve links with their own containment check (as
// bottle.ResolveBinary does) before trusting them. Hardlinks are confined to
// destDir. Devices and FIFOs are skipped.
func extractTar(r io.Reader, destDir string) error {
	root, err := openExtractRoot(destDir)
	if err != nil {
		return err
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
		if skip, err := isRootEntry(relName, header.Name, header.Typeflag == tar.TypeDir); err != nil {
			return err
		} else if skip {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(relName, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// Perm() drops setuid/setgid/sticky and any type bits, which
			// os.Root.OpenFile refuses.
			if err := writeFile(root, relName, os.FileMode(header.Mode).Perm(), tr); err != nil {
				return err
			}

		case tar.TypeSymlink:
			if err := writeSymlink(root, relName, header.Linkname); err != nil {
				return err
			}

		case tar.TypeLink:
			// A hardlink's Linkname is another archive member's path.
			oldRel, err := entryRelPath(header.Linkname)
			if err != nil {
				return err
			}
			if err := writeHardlink(root, oldRel, relName); err != nil {
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
// extractTar, writes are confined to destDir by os.Root, a later member
// replaces an earlier one of the same name (a link is removed, never written
// through), and symlink entries are created as-is, so consumers must check
// containment themselves (as bottle.ResolveBinary does). Devices, FIFOs and
// sockets are skipped.
func ExtractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip archive: %w", err)
	}
	defer r.Close()

	root, err := openExtractRoot(destDir)
	if err != nil {
		return err
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
	if skip, err := isRootEntry(relName, f.Name, mode.IsDir()); err != nil {
		return err
	} else if skip {
		return nil
	}

	if mode.IsDir() {
		if err := root.MkdirAll(relName, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
		return nil
	}

	isLink := mode&os.ModeSymlink != 0
	if !isLink && !mode.IsRegular() {
		return nil
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in archive: %w", err)
	}
	defer rc.Close()

	// A symlink entry's body is its target. Reading it to EOF also makes the
	// zip reader verify its CRC.
	if isLink {
		linkname, err := io.ReadAll(io.LimitReader(rc, maxZipLinkTarget+1))
		if err != nil {
			return fmt.Errorf("failed to read symlink target: %w", err)
		}
		if len(linkname) > maxZipLinkTarget {
			return fmt.Errorf("symlink target for %s exceeds %d bytes", f.Name, maxZipLinkTarget)
		}
		return writeSymlink(root, relName, string(linkname))
	}

	return writeFile(root, relName, mode.Perm(), rc)
}
