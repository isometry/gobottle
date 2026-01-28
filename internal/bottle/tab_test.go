package bottle

import (
	"encoding/json"
	"os"
	"testing"
)

func TestNewTab(t *testing.T) {
	tests := []struct {
		name     string
		opts     TabOptions
		expected struct {
			arch     string
			os       string
			compiler string
			tapEmpty bool
		}
	}{
		{
			name: "darwin arm64",
			opts: TabOptions{
				Formula:   "myapp",
				Version:   "1.0.0",
				Arch:      "arm64",
				OS:        "darwin",
				OSVersion: "14.0",
				Tap:       "user/homebrew-tap",
			},
			expected: struct {
				arch     string
				os       string
				compiler string
				tapEmpty bool
			}{
				arch:     "arm64",
				os:       "darwin",
				compiler: "go",
				tapEmpty: false,
			},
		},
		{
			name: "darwin amd64",
			opts: TabOptions{
				Formula:   "myapp",
				Version:   "1.0.0",
				Arch:      "amd64",
				OS:        "darwin",
				OSVersion: "14.0",
			},
			expected: struct {
				arch     string
				os       string
				compiler string
				tapEmpty bool
			}{
				arch:     "x86_64",
				os:       "darwin",
				compiler: "go",
				tapEmpty: true,
			},
		},
		{
			name: "linux arm64",
			opts: TabOptions{
				Formula:  "myapp",
				Version:  "1.0.0",
				Arch:     "arm64",
				OS:       "linux",
				Compiler: "clang",
			},
			expected: struct {
				arch     string
				os       string
				compiler string
				tapEmpty bool
			}{
				arch:     "arm64",
				os:       "linux",
				compiler: "clang",
				tapEmpty: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tab := NewTab(tt.opts)

			if tab.Arch != tt.expected.arch {
				t.Errorf("expected Arch=%q, got %q", tt.expected.arch, tab.Arch)
			}
			if tab.BuiltOn.OS != tt.expected.os {
				t.Errorf("expected OS=%q, got %q", tt.expected.os, tab.BuiltOn.OS)
			}
			if tab.Compiler != tt.expected.compiler {
				t.Errorf("expected Compiler=%q, got %q", tt.expected.compiler, tab.Compiler)
			}
			if tt.expected.tapEmpty && tab.Source.Tap != "" {
				t.Errorf("expected empty Tap, got %q", tab.Source.Tap)
			}
			if !tt.expected.tapEmpty && tab.Source.Tap == "" {
				t.Error("expected non-empty Tap")
			}

			// Verify built_as_bottle is always true
			if !tab.BuiltAsBottle {
				t.Error("expected BuiltAsBottle=true")
			}

			// Verify version is set
			if tab.Source.Versions.Stable != tt.opts.Version {
				t.Errorf("expected Version=%q, got %q", tt.opts.Version, tab.Source.Versions.Stable)
			}
		})
	}
}

func TestTab_Marshal(t *testing.T) {
	tab := NewTab(TabOptions{
		Formula:   "testapp",
		Version:   "2.0.0",
		Arch:      "arm64",
		OS:        "darwin",
		OSVersion: "14.0",
		Tap:       "myorg/homebrew-tap",
	})

	data, err := tab.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify it's valid JSON
	var unmarshaled Tab
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify key fields
	if unmarshaled.Source.Versions.Stable != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %s", unmarshaled.Source.Versions.Stable)
	}
	if unmarshaled.BuiltAsBottle != true {
		t.Error("expected built_as_bottle=true")
	}
	if unmarshaled.Source.Tap != "myorg/homebrew-tap" {
		t.Errorf("expected tap myorg/homebrew-tap, got %s", unmarshaled.Source.Tap)
	}
}

func TestTab_SourceDateEpoch(t *testing.T) {
	// Set SOURCE_DATE_EPOCH
	originalEpoch := os.Getenv("SOURCE_DATE_EPOCH")
	os.Setenv("SOURCE_DATE_EPOCH", "1609459200") // 2021-01-01 00:00:00 UTC
	defer os.Setenv("SOURCE_DATE_EPOCH", originalEpoch)

	tab := NewTab(TabOptions{
		Formula: "testapp",
		Version: "1.0.0",
		Arch:    "arm64",
		OS:      "darwin",
	})

	if tab.Time != 1609459200 {
		t.Errorf("expected Time=1609459200, got %d", tab.Time)
	}
	if tab.SourceModifiedTime != 1609459200 {
		t.Errorf("expected SourceModifiedTime=1609459200, got %d", tab.SourceModifiedTime)
	}
}

func TestTab_JSONStructure(t *testing.T) {
	tab := NewTab(TabOptions{
		Formula:   "myapp",
		Version:   "1.0.0",
		Arch:      "arm64",
		OS:        "darwin",
		OSVersion: "14.0",
		Tap:       "user/tap",
	})

	data, err := tab.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify JSON structure matches Homebrew expectations
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	// Check required top-level fields
	requiredFields := []string{
		"homebrew_version",
		"used_options",
		"unused_options",
		"built_as_bottle",
		"poured_from_bottle",
		"installed_as_dependency",
		"installed_on_request",
		"changed_files",
		"time",
		"source_modified_time",
		"compiler",
		"aliases",
		"runtime_dependencies",
		"source",
		"arch",
		"built_on",
	}

	for _, field := range requiredFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// Check source structure
	source, ok := raw["source"].(map[string]any)
	if !ok {
		t.Fatal("source should be an object")
	}

	if _, ok := source["spec"]; !ok {
		t.Error("source.spec is required")
	}
	if _, ok := source["versions"]; !ok {
		t.Error("source.versions is required")
	}

	// Check built_on structure
	builtOn, ok := raw["built_on"].(map[string]any)
	if !ok {
		t.Fatal("built_on should be an object")
	}

	if _, ok := builtOn["os"]; !ok {
		t.Error("built_on.os is required")
	}
}
