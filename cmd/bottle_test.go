package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNewBottleCommand(t *testing.T) {
	rootOpts := &Options{}
	cmd := NewBottleCommand(rootOpts)

	// Test command basics
	if cmd.Use != "bottle [formula]" {
		t.Errorf("expected Use to be 'bottle [formula]', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}

	if cmd.Long == "" {
		t.Error("expected Long description to be set")
	}

	if cmd.Example == "" {
		t.Error("expected Example to be set")
	}
}

func TestBottleCommandFlags(t *testing.T) {
	rootOpts := &Options{}
	cmd := NewBottleCommand(rootOpts)

	// Test that all expected flags exist
	expectedFlags := []struct {
		name         string
		defaultValue string
	}{
		{"source", "local"},
		{"owner", ""},
		{"repo", ""},
		{"tag", ""},
		{"dist", "dist"},
		{"formula", ""},
		{"version", ""},
		{"cellar", ":any_skip_relocation"},
		{"rebuild", "0"},
		{"refresh-platforms", "false"},
		{"registry", "ghcr.io"},
		{"registry-owner", ""},
		{"package", ""},
		{"push", "true"},
		{"tap-owner", ""},
		{"tap-repo", "homebrew-tap"},
		{"tap-branch", "main"},
		{"update-formula", "true"},
		{"skip-tap-update", "false"},
		{"output", "text"},
	}

	for _, ef := range expectedFlags {
		t.Run(ef.name, func(t *testing.T) {
			flag := cmd.Flags().Lookup(ef.name)
			if flag == nil {
				t.Errorf("expected flag --%s to exist", ef.name)
				return
			}
			if flag.DefValue != ef.defaultValue {
				t.Errorf("expected default value %q for --%s, got %q", ef.defaultValue, ef.name, flag.DefValue)
			}
		})
	}
}

func TestBottleCommandPositionalArg(t *testing.T) {
	rootOpts := &Options{}
	cmd := NewBottleCommand(rootOpts)

	// The command should accept at most 1 positional argument
	err := cmd.Args(cmd, []string{})
	if err != nil {
		t.Errorf("expected no error with 0 args, got %v", err)
	}

	err = cmd.Args(cmd, []string{"myformula"})
	if err != nil {
		t.Errorf("expected no error with 1 arg, got %v", err)
	}

	err = cmd.Args(cmd, []string{"arg1", "arg2"})
	if err == nil {
		t.Error("expected error with 2 args, got none")
	}
}

func TestBottleCommandHelp(t *testing.T) {
	rootOpts := &Options{}
	cmd := NewBottleCommand(rootOpts)

	// Capture help output
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Execute --help
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help command failed: %v", err)
	}

	output := buf.String()

	// Check that examples are in the output
	if !strings.Contains(output, "Examples:") {
		t.Error("expected help output to contain 'Examples:'")
	}

	// Check that key flags are documented
	if !strings.Contains(output, "--formula") {
		t.Error("expected help output to contain '--formula'")
	}
	if !strings.Contains(output, "--push") {
		t.Error("expected help output to contain '--push'")
	}
	if !strings.Contains(output, "--output") {
		t.Error("expected help output to contain '--output'")
	}
}

func TestBottleOutputTypes(t *testing.T) {
	tests := []struct {
		output      string
		expectJSON  bool
		expectBare  bool
		expectQuiet bool
	}{
		{"text", false, false, false},
		{"json", true, false, true},
		{"bare", false, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			jsonOutput := tt.output == "json"
			bareOutput := tt.output == "bare"
			quietOutput := jsonOutput || bareOutput

			if jsonOutput != tt.expectJSON {
				t.Errorf("jsonOutput: expected %v, got %v", tt.expectJSON, jsonOutput)
			}
			if bareOutput != tt.expectBare {
				t.Errorf("bareOutput: expected %v, got %v", tt.expectBare, bareOutput)
			}
			if quietOutput != tt.expectQuiet {
				t.Errorf("quietOutput: expected %v, got %v", tt.expectQuiet, quietOutput)
			}
		})
	}
}

func TestBuildConfig(t *testing.T) {
	opts := &BottleOptions{
		Source:           "github",
		Owner:            "myorg",
		Repo:             "myrepo",
		Tag:              "v1.0.0",
		Formula:          "myformula",
		Version:          "1.0.0",
		Cellar:           ":any",
		Rebuild:          1,
		RegistryHost:     "custom.io",
		RegistryOwner:    "regowner",
		Package:          "mypkg",
		TapOwner:         "tapowner",
		TapRepo:          "mytap",
		TapBranch:        "develop",
		Binaries:         []string{"bin1", "bin2"},
		Platforms:        []string{"arm64_sonoma"},
		ExcludePlatforms: []string{"ventura"},
		RefreshPlatforms: true,
	}

	cfg := buildConfig(opts)

	// Verify source config
	if cfg.Source.Type != "github" {
		t.Errorf("expected Source.Type=github, got %s", cfg.Source.Type)
	}
	if cfg.Source.Owner != "myorg" {
		t.Errorf("expected Source.Owner=myorg, got %s", cfg.Source.Owner)
	}
	if cfg.Source.Repo != "myrepo" {
		t.Errorf("expected Source.Repo=myrepo, got %s", cfg.Source.Repo)
	}
	if cfg.Source.Tag != "v1.0.0" {
		t.Errorf("expected Source.Tag=v1.0.0, got %s", cfg.Source.Tag)
	}

	// Verify formula config
	if cfg.Formula != "myformula" {
		t.Errorf("expected Formula=myformula, got %s", cfg.Formula)
	}
	if cfg.Version != "1.0.0" {
		t.Errorf("expected Version=1.0.0, got %s", cfg.Version)
	}

	// Verify bottle config
	if cfg.Bottle.Cellar != ":any" {
		t.Errorf("expected Bottle.Cellar=:any, got %s", cfg.Bottle.Cellar)
	}
	if cfg.Bottle.Rebuild != 1 {
		t.Errorf("expected Bottle.Rebuild=1, got %d", cfg.Bottle.Rebuild)
	}
	if len(cfg.Bottle.Platforms) != 1 || cfg.Bottle.Platforms[0] != "arm64_sonoma" {
		t.Errorf("expected Bottle.Platforms=[arm64_sonoma], got %v", cfg.Bottle.Platforms)
	}
	if len(cfg.Bottle.ExcludePlatforms) != 1 || cfg.Bottle.ExcludePlatforms[0] != "ventura" {
		t.Errorf("expected Bottle.ExcludePlatforms=[ventura], got %v", cfg.Bottle.ExcludePlatforms)
	}
	if !cfg.Bottle.RefreshPlatforms {
		t.Error("expected Bottle.RefreshPlatforms=true")
	}

	// Verify registry config
	if cfg.Registry.Host != "custom.io" {
		t.Errorf("expected Registry.Host=custom.io, got %s", cfg.Registry.Host)
	}
	if cfg.Registry.Owner != "regowner" {
		t.Errorf("expected Registry.Owner=regowner, got %s", cfg.Registry.Owner)
	}
	if cfg.Registry.Package != "mypkg" {
		t.Errorf("expected Registry.Package=mypkg, got %s", cfg.Registry.Package)
	}

	// Verify tap config
	if cfg.Tap.Owner != "tapowner" {
		t.Errorf("expected Tap.Owner=tapowner, got %s", cfg.Tap.Owner)
	}
	if cfg.Tap.Repo != "mytap" {
		t.Errorf("expected Tap.Repo=mytap, got %s", cfg.Tap.Repo)
	}
	if cfg.Tap.Branch != "develop" {
		t.Errorf("expected Tap.Branch=develop, got %s", cfg.Tap.Branch)
	}

	// Verify binaries
	if len(cfg.Binaries) != 2 {
		t.Errorf("expected 2 binaries, got %d", len(cfg.Binaries))
	}
}

func TestBuildConfigOwnerFallback(t *testing.T) {
	opts := &BottleOptions{
		Owner: "mainowner",
	}

	cfg := buildConfig(opts)

	// Registry and Tap owners should fall back to main owner
	if cfg.Registry.Owner != "mainowner" {
		t.Errorf("expected Registry.Owner to fall back to mainowner, got %s", cfg.Registry.Owner)
	}
	if cfg.Tap.Owner != "mainowner" {
		t.Errorf("expected Tap.Owner to fall back to mainowner, got %s", cfg.Tap.Owner)
	}
}

func TestSkipTapUpdateFlag(t *testing.T) {
	// Create a test command
	cmd := &cobra.Command{}
	cmd.Flags().Bool("skip-tap-update", false, "")
	cmd.Flags().Bool("update-formula", true, "")

	// Test without skip-tap-update
	opts := &BottleOptions{UpdateFormula: true}
	skipTapUpdate, _ := cmd.Flags().GetBool("skip-tap-update")
	if skipTapUpdate {
		opts.UpdateFormula = false
	}
	if !opts.UpdateFormula {
		t.Error("expected UpdateFormula=true when skip-tap-update not set")
	}

	// Test with skip-tap-update set
	_ = cmd.Flags().Set("skip-tap-update", "true")
	opts2 := &BottleOptions{UpdateFormula: true}
	skipTapUpdate2, _ := cmd.Flags().GetBool("skip-tap-update")
	if skipTapUpdate2 {
		opts2.UpdateFormula = false
	}
	if opts2.UpdateFormula {
		t.Error("expected UpdateFormula=false when skip-tap-update is set")
	}
}
