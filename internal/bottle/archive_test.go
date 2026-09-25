package bottle

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWriteTarGzPinsSymlinkMode ensures a symlink discovered via Lstat gets
// a host-independent mode in the output header, not whatever the local
// filesystem reports (which varies by platform and would make bottle
// digests depend on the build host).
func TestWriteTarGzPinsSymlinkMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("mytool", filepath.Join(dir, "kubectl-mytool")); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(dir, "out.tar.gz")
	files := map[string]string{
		"mytool/1.2.3/bin/kubectl-mytool": filepath.Join(dir, "kubectl-mytool"),
	}
	if _, err := writeTarGz(outputPath, files, time.Unix(1700000000, 0)); err != nil {
		t.Fatalf("writeTarGz: %v", err)
	}

	f, err := os.Open(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gzr)
	var hdr *tar.Header
	for {
		h, err := tr.Next()
		if err != nil {
			t.Fatalf("symlink entry not found: %v", err)
		}
		if h.Name == "mytool/1.2.3/bin/kubectl-mytool" {
			hdr = h
			break
		}
	}
	if hdr.Typeflag != tar.TypeSymlink {
		t.Fatalf("typeflag = %v, want TypeSymlink", hdr.Typeflag)
	}
	if hdr.Linkname != "mytool" {
		t.Errorf("linkname = %q, want %q", hdr.Linkname, "mytool")
	}
	if hdr.Mode != 0777 {
		t.Errorf("mode = %o, want 0777", hdr.Mode)
	}
}
