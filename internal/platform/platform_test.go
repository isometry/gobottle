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
		LinuxArches: []string{"x86_64", "aarch64"},
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
		if platforms[0].Tag != "aarch64_linux" {
			t.Errorf("expected tag aarch64_linux, got %q", platforms[0].Tag)
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
