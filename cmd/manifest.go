package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/isometry/gobottle/internal/bottle"
	"github.com/isometry/gobottle/internal/platform"
)

// Manifest is the machine-readable contract between the build, push, and
// release verbs (`bottles.json`). Every verb emits it on stdout with
// -o json; push and release accept it on stdin or via --input.
type Manifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Formula       string          `json:"formula"`
	Version       string          `json:"version"`
	Rebuild       int             `json:"rebuild,omitempty"`
	RootURL       string          `json:"rootURL"`
	Description   string          `json:"description,omitempty"`
	Homepage      string          `json:"homepage,omitempty"`
	License       string          `json:"license,omitempty"`
	Source        ManifestSource  `json:"source"`
	Registry      ManifestReg     `json:"registry"`
	Tap           ManifestTap     `json:"tap"`
	Bottles       []ManifestEntry `json:"bottles"`
}

type ManifestSource struct {
	Type   string `json:"type"`
	URL    string `json:"url,omitempty"`    // source tarball URL
	SHA256 string `json:"sha256,omitempty"` // source tarball SHA256
	Commit string `json:"commit,omitempty"`
	Tag    string `json:"tag,omitempty"`
}

type ManifestReg struct {
	Host     string `json:"host"`
	RootPath string `json:"rootPath"`
}

type ManifestTap struct {
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	Branch      string `json:"branch"`
	FormulaPath string `json:"formulaPath"`
	CommitSHA   string `json:"commitSHA,omitempty"`
}

type ManifestEntry struct {
	Platform           string          `json:"platform"`
	OS                 string          `json:"os"`
	Arch               string          `json:"arch"`
	OSVersion          string          `json:"osVersion,omitempty"`
	OSVersionMajor     int             `json:"osVersionMajor,omitempty"`
	SHA256             string          `json:"sha256"`
	UncompressedSHA256 string          `json:"uncompressedSHA256"`
	UncompressedSize   int64           `json:"uncompressedSize"`
	Cellar             string          `json:"cellar"`
	Path               string          `json:"path,omitempty"`
	Ref                string          `json:"ref,omitempty"`
	Pushed             bool            `json:"pushed"`
	Tab                json.RawMessage `json:"tab,omitempty"`
}

// manifestFromBottles builds a Manifest from freshly built bottles.
func manifestEntry(b *bottle.Bottle) (ManifestEntry, error) {
	entry := ManifestEntry{
		Platform:           b.Platform.Tag,
		OS:                 b.Platform.OS,
		Arch:               b.Platform.Arch,
		OSVersion:          b.Platform.OSVersion,
		OSVersionMajor:     b.Platform.OSVersionMajor,
		SHA256:             b.SHA256,
		UncompressedSHA256: b.UncompressedSHA256,
		UncompressedSize:   b.UncompressedSize,
		Cellar:             b.Cellar,
		Path:               b.Path,
	}
	if b.Tab != nil {
		tabJSON, err := b.Tab.Marshal()
		if err != nil {
			return entry, fmt.Errorf("failed to marshal tab: %w", err)
		}
		entry.Tab = tabJSON
	}
	return entry, nil
}

// toBottle reconstructs a bottle.Bottle from a manifest entry (for push).
func (e ManifestEntry) toBottle(m *Manifest) (*bottle.Bottle, error) {
	b := &bottle.Bottle{
		Formula: m.Formula,
		Version: m.Version,
		Platform: platform.Platform{
			Tag:            e.Platform,
			OS:             e.OS,
			Arch:           e.Arch,
			OSVersion:      e.OSVersion,
			OSVersionMajor: e.OSVersionMajor,
		},
		SHA256:             e.SHA256,
		UncompressedSHA256: e.UncompressedSHA256,
		UncompressedSize:   e.UncompressedSize,
		Path:               e.Path,
		Cellar:             e.Cellar,
		Rebuild:            m.Rebuild,
	}
	if len(e.Tab) > 0 {
		var tab bottle.Tab
		if err := json.Unmarshal(e.Tab, &tab); err != nil {
			return nil, fmt.Errorf("invalid tab in manifest for %s: %w", e.Platform, err)
		}
		b.Tab = &tab
	}
	if b.Tab == nil {
		return nil, fmt.Errorf("manifest entry for %s has no tab (regenerate with 'gobottle build')", e.Platform)
	}
	return b, nil
}

// readManifest loads a manifest from the given path, or stdin when path is
// "-" or empty and stdin is piped.
func readManifest(path string) (*Manifest, error) {
	var r io.Reader
	switch {
	case path != "" && path != "-":
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open manifest: %w", err)
		}
		defer f.Close()
		r = f
	default:
		stat, err := os.Stdin.Stat()
		if err != nil || (stat.Mode()&os.ModeCharDevice) != 0 {
			return nil, fmt.Errorf("no manifest: pass --input bottles.json or pipe one on stdin")
		}
		r = os.Stdin
	}

	var m Manifest
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	if m.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported manifest schemaVersion %d (expected 1)", m.SchemaVersion)
	}
	return &m, nil
}

// writeManifestFile writes the manifest JSON to a file.
func writeManifestFile(m *Manifest, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// emitManifest prints the manifest JSON to stdout.
func emitManifest(m *Manifest) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}
