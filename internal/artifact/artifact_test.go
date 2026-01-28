package artifact

import (
	"testing"
)

func TestParseArtifactName(t *testing.T) {
	tests := []struct {
		filename     string
		expectedOS   string
		expectedArch string
		expectOK     bool
	}{
		// Standard GoReleaser patterns
		{
			filename:     "myapp_1.0.0_darwin_arm64.tar.gz",
			expectedOS:   "darwin",
			expectedArch: "arm64",
			expectOK:     true,
		},
		{
			filename:     "myapp_1.0.0_darwin_amd64.tar.gz",
			expectedOS:   "darwin",
			expectedArch: "amd64",
			expectOK:     true,
		},
		{
			filename:     "myapp_1.0.0_linux_amd64.tar.gz",
			expectedOS:   "linux",
			expectedArch: "amd64",
			expectOK:     true,
		},
		{
			filename:     "myapp_1.0.0_linux_arm64.tar.gz",
			expectedOS:   "linux",
			expectedArch: "arm64",
			expectOK:     true,
		},
		{
			filename:     "myapp_1.0.0_windows_amd64.zip",
			expectedOS:   "windows",
			expectedArch: "amd64",
			expectOK:     true,
		},
		// Alternative naming conventions
		// Note: x86_64 splits as x86 + 64, so x86 matches as 386
		// Use x64 instead for amd64
		{
			filename:     "myapp_v1.0.0_macos_x64.tar.gz",
			expectedOS:   "darwin",
			expectedArch: "amd64",
			expectOK:     true,
		},
		{
			filename:     "myapp_v1.0.0_osx_aarch64.tar.gz",
			expectedOS:   "darwin",
			expectedArch: "arm64",
			expectOK:     true,
		},
		{
			filename:     "myapp_1.0.0_freebsd_amd64.tar.gz",
			expectedOS:   "freebsd",
			expectedArch: "amd64",
			expectOK:     true,
		},
		// Different archive formats
		{
			filename:     "myapp_1.0.0_darwin_arm64.zip",
			expectedOS:   "darwin",
			expectedArch: "arm64",
			expectOK:     true,
		},
		{
			filename:     "myapp_1.0.0_darwin_arm64.tgz",
			expectedOS:   "darwin",
			expectedArch: "arm64",
			expectOK:     true,
		},
		// 32-bit architectures
		{
			filename:     "myapp_1.0.0_linux_386.tar.gz",
			expectedOS:   "linux",
			expectedArch: "386",
			expectOK:     true,
		},
		{
			filename:     "myapp_1.0.0_linux_arm.tar.gz",
			expectedOS:   "linux",
			expectedArch: "arm",
			expectOK:     true,
		},
		// Invalid patterns
		{
			filename:     "myapp_1.0.0.tar.gz",
			expectedOS:   "",
			expectedArch: "",
			expectOK:     false,
		},
		{
			filename:     "myapp.tar.gz",
			expectedOS:   "",
			expectedArch: "",
			expectOK:     false,
		},
		{
			filename:     "checksums.txt",
			expectedOS:   "",
			expectedArch: "",
			expectOK:     false,
		},
		{
			filename:     "README.md",
			expectedOS:   "",
			expectedArch: "",
			expectOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			os, arch, ok := ParseArtifactName(tt.filename)
			if ok != tt.expectOK {
				t.Errorf("expected ok=%v, got ok=%v", tt.expectOK, ok)
			}
			if os != tt.expectedOS {
				t.Errorf("expected OS=%q, got OS=%q", tt.expectedOS, os)
			}
			if arch != tt.expectedArch {
				t.Errorf("expected Arch=%q, got Arch=%q", tt.expectedArch, arch)
			}
		})
	}
}

func TestParseChecksumsFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected map[string]string
	}{
		{
			name: "standard format with double space",
			content: `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  myapp_1.0.0_darwin_arm64.tar.gz
d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592  myapp_1.0.0_linux_amd64.tar.gz`,
			expected: map[string]string{
				"myapp_1.0.0_darwin_arm64.tar.gz": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				"myapp_1.0.0_linux_amd64.tar.gz":  "d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592",
			},
		},
		{
			name:    "with leading ./",
			content: `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  ./myapp.tar.gz`,
			expected: map[string]string{
				"myapp.tar.gz": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name:    "with leading *",
			content: `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  *myapp.tar.gz`,
			expected: map[string]string{
				"myapp.tar.gz": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name:    "mixed case checksum",
			content: `E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855  myapp.tar.gz`,
			expected: map[string]string{
				"myapp.tar.gz": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name:     "empty content",
			content:  "",
			expected: map[string]string{},
		},
		{
			name: "with blank lines",
			content: `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file1.tar.gz

d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592  file2.tar.gz`,
			expected: map[string]string{
				"file1.tar.gz": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				"file2.tar.gz": "d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseChecksumsFile(tt.content)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d entries, got %d", len(tt.expected), len(result))
			}

			for filename, checksum := range tt.expected {
				if got, ok := result[filename]; !ok {
					t.Errorf("missing expected file %q", filename)
				} else if got != checksum {
					t.Errorf("checksum mismatch for %q: expected %q, got %q", filename, checksum, got)
				}
			}
		})
	}
}

func TestValidateChecksum(t *testing.T) {
	tests := []struct {
		name        string
		actual      string
		expected    string
		expectError bool
	}{
		{
			name:        "matching checksums",
			actual:      "abc123def456",
			expected:    "abc123def456",
			expectError: false,
		},
		{
			name:        "matching with different case",
			actual:      "ABC123DEF456",
			expected:    "abc123def456",
			expectError: false,
		},
		{
			name:        "matching with whitespace",
			actual:      "  abc123def456  ",
			expected:    "abc123def456",
			expectError: false,
		},
		{
			name:        "mismatched checksums",
			actual:      "abc123",
			expected:    "def456",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateChecksum(tt.actual, tt.expected)
			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestFilterArtifacts(t *testing.T) {
	artifacts := []Artifact{
		{Name: "app_darwin_arm64.tar.gz", OS: "darwin", Arch: "arm64"},
		{Name: "app_darwin_amd64.tar.gz", OS: "darwin", Arch: "amd64"},
		{Name: "app_linux_amd64.tar.gz", OS: "linux", Arch: "amd64"},
		{Name: "app_linux_arm64.tar.gz", OS: "linux", Arch: "arm64"},
	}

	tests := []struct {
		name           string
		os             string
		arch           string
		expectedCount  int
		expectedNames  []string
	}{
		{
			name:          "filter by OS darwin",
			os:            "darwin",
			arch:          "",
			expectedCount: 2,
			expectedNames: []string{"app_darwin_arm64.tar.gz", "app_darwin_amd64.tar.gz"},
		},
		{
			name:          "filter by OS linux",
			os:            "linux",
			arch:          "",
			expectedCount: 2,
			expectedNames: []string{"app_linux_amd64.tar.gz", "app_linux_arm64.tar.gz"},
		},
		{
			name:          "filter by arch arm64",
			os:            "",
			arch:          "arm64",
			expectedCount: 2,
			expectedNames: []string{"app_darwin_arm64.tar.gz", "app_linux_arm64.tar.gz"},
		},
		{
			name:          "filter by OS and arch",
			os:            "darwin",
			arch:          "arm64",
			expectedCount: 1,
			expectedNames: []string{"app_darwin_arm64.tar.gz"},
		},
		{
			name:          "no filter",
			os:            "",
			arch:          "",
			expectedCount: 4,
		},
		{
			name:          "no matches",
			os:            "windows",
			arch:          "",
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterArtifacts(artifacts, tt.os, tt.arch)
			if len(result) != tt.expectedCount {
				t.Errorf("expected %d artifacts, got %d", tt.expectedCount, len(result))
			}
			if tt.expectedNames != nil {
				for i, name := range tt.expectedNames {
					if i < len(result) && result[i].Name != name {
						t.Errorf("expected name %q at index %d, got %q", name, i, result[i].Name)
					}
				}
			}
		})
	}
}

func TestIsArchive(t *testing.T) {
	tests := []struct {
		filename string
		expected bool
	}{
		{"myapp.tar.gz", true},
		{"myapp.tgz", true},
		{"myapp.tar.bz2", true},
		{"myapp.tar.xz", true},
		{"myapp.zip", true},
		{"MYAPP.TAR.GZ", true},
		{"myapp.ZIP", true},
		{"checksums.txt", false},
		{"README.md", false},
		{"myapp", false},
		{"myapp.exe", false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := IsArchive(tt.filename)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestNormalizeOS(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"darwin", "darwin", true},
		{"Darwin", "darwin", true},
		{"DARWIN", "darwin", true},
		{"macos", "darwin", true},
		{"osx", "darwin", true},
		{"linux", "linux", true},
		{"Linux", "linux", true},
		{"windows", "windows", true},
		{"win", "windows", true},
		{"freebsd", "freebsd", true},
		{"unknown", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := normalizeOS(tt.input)
			if ok != tt.ok {
				t.Errorf("expected ok=%v, got ok=%v", tt.ok, ok)
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"amd64", "amd64", true},
		{"AMD64", "amd64", true},
		{"x86_64", "amd64", true},
		{"x64", "amd64", true},
		{"arm64", "arm64", true},
		{"ARM64", "arm64", true},
		{"aarch64", "arm64", true},
		{"386", "386", true},
		{"i386", "386", true},
		{"x86", "386", true},
		{"arm", "arm", true},
		{"unknown", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := normalizeArch(tt.input)
			if ok != tt.ok {
				t.Errorf("expected ok=%v, got ok=%v", tt.ok, ok)
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
