package cmd

import (
	"testing"

	"github.com/spf13/viper"
)

// The tap token is optional: a dedicated GOBOTTLE_TAP_TOKEN wins, otherwise
// the standard GITHUB_TOKEN serves both registry and tap.

func TestInitConfigDedicatedTapToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "standard")
	t.Setenv("GOBOTTLE_TAP_TOKEN", "dedicated")
	viper.Reset()
	t.Cleanup(viper.Reset)

	if err := initConfig(&Options{}); err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if got := viper.GetString("tap.token"); got != "dedicated" {
		t.Errorf("tap.token = %q, want %q", got, "dedicated")
	}
	if got := viper.GetString("registry.token"); got != "standard" {
		t.Errorf("registry.token = %q, want %q", got, "standard")
	}
}

func TestInitConfigTapTokenFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "standard")
	// An empty env var must behave as unset (an absent CI secret expands to
	// ""), leaving the standard-token fallback intact.
	t.Setenv("GOBOTTLE_TAP_TOKEN", "")
	viper.Reset()
	t.Cleanup(viper.Reset)

	if err := initConfig(&Options{}); err != nil {
		t.Fatalf("initConfig: %v", err)
	}
	if got := viper.GetString("tap.token"); got != "standard" {
		t.Errorf("tap.token = %q, want fallback %q", got, "standard")
	}
}
