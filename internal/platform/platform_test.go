package platform

import (
	"testing"
)

func TestGetPlatformsForOS(t *testing.T) {
	pi := &PlatformInfo{
		MacOSVersions: []MacOSVersion{
			{Major: 15, Symbol: "sequoia"},
			{Major: 14, Symbol: "sonoma"},
		},
		LinuxArches: []string{"x86_64", "arm64"},
	}

	t.Run("darwin arm64 returns oldest version only", func(t *testing.T) {
		platforms := pi.GetPlatformsForOS("darwin", "arm64")
		if len(platforms) != 1 {
			t.Errorf("expected 1 platform, got %d", len(platforms))
			return
		}
		if platforms[0].Tag != "arm64_sonoma" {
			t.Errorf("expected tag arm64_sonoma, got %q", platforms[0].Tag)
		}
	})

	t.Run("darwin amd64 returns oldest version only", func(t *testing.T) {
		platforms := pi.GetPlatformsForOS("darwin", "amd64")
		if len(platforms) != 1 {
			t.Errorf("expected 1 platform, got %d", len(platforms))
			return
		}
		if platforms[0].Tag != "sonoma" {
			t.Errorf("expected tag sonoma, got %q", platforms[0].Tag)
		}
	})

	t.Run("linux amd64 returns single platform", func(t *testing.T) {
		platforms := pi.GetPlatformsForOS("linux", "amd64")
		if len(platforms) != 1 {
			t.Errorf("expected 1 platform, got %d", len(platforms))
			return
		}
		if platforms[0].Tag != "x86_64_linux" {
			t.Errorf("expected tag x86_64_linux, got %q", platforms[0].Tag)
		}
	})

	t.Run("unsupported OS returns nil", func(t *testing.T) {
		platforms := pi.GetPlatformsForOS("windows", "amd64")
		if platforms != nil {
			t.Errorf("expected nil for unsupported OS, got %v", platforms)
		}
	})

	t.Run("empty MacOSVersions returns nil for darwin", func(t *testing.T) {
		emptyPi := &PlatformInfo{
			MacOSVersions: []MacOSVersion{},
			LinuxArches:   []string{"x86_64"},
		}
		platforms := emptyPi.GetPlatformsForOS("darwin", "arm64")
		if platforms != nil {
			t.Errorf("expected nil for empty MacOSVersions, got %v", platforms)
		}
	})

	t.Run("linux arm64 returns single platform", func(t *testing.T) {
		platforms := pi.GetPlatformsForOS("linux", "arm64")
		if len(platforms) != 1 {
			t.Errorf("expected 1 platform, got %d", len(platforms))
			return
		}
		if platforms[0].Tag != "arm64_linux" {
			t.Errorf("expected tag arm64_linux, got %q", platforms[0].Tag)
		}
	})
}

func TestFilterPlatforms(t *testing.T) {
	platforms := []Platform{
		{Tag: "arm64_sequoia"},
		{Tag: "arm64_sonoma"},
	}

	t.Run("include filter", func(t *testing.T) {
		result := FilterPlatforms(platforms, []string{"arm64_sonoma"}, nil)
		if len(result) != 1 {
			t.Errorf("expected 1 platform, got %d", len(result))
		}
	})

	t.Run("exclude filter", func(t *testing.T) {
		result := FilterPlatforms(platforms, nil, []string{"arm64_sequoia"})
		if len(result) != 1 {
			t.Errorf("expected 1 platform, got %d", len(result))
		}
	})

	t.Run("no filter returns all", func(t *testing.T) {
		result := FilterPlatforms(platforms, nil, nil)
		if len(result) != 2 {
			t.Errorf("expected 2 platforms, got %d", len(result))
		}
	})
}

func TestFilterPlatformsLegacyLinuxTag(t *testing.T) {
	platforms := []Platform{
		{Tag: "arm64_linux", OS: "linux", Arch: "arm64"},
		{Tag: "x86_64_linux", OS: "linux", Arch: "amd64"},
	}
	// Filters written against older releases (aarch64_linux) keep matching.
	got := FilterPlatforms(platforms, []string{"aarch64_linux"}, nil)
	if len(got) != 1 || got[0].Tag != "arm64_linux" {
		t.Errorf("include aarch64_linux = %+v, want the arm64_linux platform", got)
	}
	got = FilterPlatforms(platforms, nil, []string{"aarch64_linux"})
	if len(got) != 1 || got[0].Tag != "x86_64_linux" {
		t.Errorf("exclude aarch64_linux = %+v, want only x86_64_linux", got)
	}
}

func TestParsePlatformTagLinux(t *testing.T) {
	for _, tag := range []string{"arm64_linux", "aarch64_linux"} {
		os, arch, version := ParsePlatformTag(tag)
		if os != "linux" || arch != "arm64" || version != "" {
			t.Errorf("ParsePlatformTag(%q) = %q/%q/%q", tag, os, arch, version)
		}
	}
}
