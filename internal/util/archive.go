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

// extractTar walks a tar stream, writing entries under destDir.
//
// Every write goes through os.Root, which confines all filesystem access to
// destDir at the OS level as each path component (including symlinks) is
// actually resolved. That is the real defence: a lexical check on header
// text can be fooled by a chain of symlinks whose individual hops each look
// safe in isolation but whose combined resolution escapes destDir (e.g. a
// link "s" that lands back inside destDir, followed by a link "u" whose
// remaining ".." components are then counted from s's real target rather
// than from u's own directory). The header.Name and Linkname lexical checks
// below stay because they give an early, specific error for the common
// cases; os.Root is what actually prevents escape.
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

		// Construct target path
		target := filepath.Join(destDir, header.Name)

		// Ensure target is within destDir (security check)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", header.Name)
		}
		// Derive relName from the already-validated target rather than from
		// header.Name directly: a member name with a leading "/" (e.g. from
		// `tar -P`) stays absolute through filepath.Clean, and os.Root
		// rejects an absolute path outright even though target above
		// resolves safely under destDir.
		relName, err := filepath.Rel(filepath.Clean(destDir), filepath.Clean(target))
		if err != nil {
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

			// Create file
			outFile, err := root.OpenFile(relName, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			outFile.Close()

		case tar.TypeSymlink:
			// Reject a link target that escapes destDir, whether given as an
			// absolute path or as a relative path that climbs out via "..".
			if filepath.IsAbs(header.Linkname) {
				return fmt.Errorf("invalid file path: %s -> %s", header.Name, header.Linkname)
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(target), header.Linkname))
			if !strings.HasPrefix(resolved, filepath.Clean(destDir)+string(os.PathSeparator)) {
				return fmt.Errorf("invalid file path: %s -> %s", header.Name, header.Linkname)
			}

			// Create parent directory if needed
			if err := root.MkdirAll(filepath.Dir(relName), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			if err := root.Symlink(header.Linkname, relName); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
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

// ExtractZip extracts a .zip archive to a destination directory
func ExtractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip archive: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		// Construct target path
		target := filepath.Join(destDir, f.Name)

		// Ensure target is within destDir (security check)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		// Create parent directory if needed
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Create file
		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open file in archive: %w", err)
		}

		if _, err := io.Copy(outFile, rc); err != nil {
			rc.Close()
			outFile.Close()
			return fmt.Errorf("failed to write file: %w", err)
		}

		rc.Close()
		outFile.Close()
	}

	return nil
}
