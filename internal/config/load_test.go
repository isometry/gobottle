package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func loadFromYAML(t *testing.T, yaml string) *Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".gobottle.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	cfg, err := Load(v)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestLoadFullConfig(t *testing.T) {
	cfg := loadFromYAML(t, `
formula:
  name: mytool
  description: Does things
  homepage: https://example.com
  license: MIT
  head: true
  dependencies: [git]
  conflicts: [other]
  caveats: Be careful.
  install:
    completions: true
    extra: ['man1.install "docs/mytool.1"']
  test:
    command: [version]
  service: run [opt_bin/"mytool"]
version: 1.2.3
source:
  type: go
  owner: acme
  repo: mytool
  build:
    packages: [./cmd/mytool]
    ldflags: "-X main.version={{.Version}}"
    env:
      - FOO=bar
bottle:
  cellar: ":any"
  rebuild: 1
registry:
  host: ghcr.io
  root_path: acme/bottles
tap:
  owner: acme
  repo: homebrew-things
  branch: trunk
binaries:
  - mytool
  - name: helper
    install_path: libexec
`)

	if cfg.Formula.Name != "mytool" || cfg.Formula.Description != "Does things" {
		t.Errorf("formula section not loaded: %+v", cfg.Formula)
	}
	if cfg.Formula.Head == nil || !*cfg.Formula.Head || !cfg.Formula.Install.Completions {
		t.Error("formula booleans not loaded")
	}
	if len(cfg.Formula.Dependencies) != 1 || cfg.Formula.Dependencies[0] != "git" {
		t.Errorf("dependencies = %v", cfg.Formula.Dependencies)
	}
	if got := cfg.Formula.Test.Command; len(got) != 1 || got[0] != "version" {
		t.Errorf("test.command = %v", got)
	}
	if cfg.Source.Type != "go" {
		t.Errorf("source.type = %s (the historic dead-config bug)", cfg.Source.Type)
	}
	if len(cfg.Source.Build.Packages) != 1 || cfg.Source.Build.Packages[0] != "./cmd/mytool" {
		t.Errorf("build.packages = %v", cfg.Source.Build.Packages)
	}
	if len(cfg.Source.Build.Env) != 1 || cfg.Source.Build.Env[0] != "FOO=bar" {
		t.Errorf("build.env = %v", cfg.Source.Build.Env)
	}
	if cfg.Bottle.Rebuild != 1 || cfg.Bottle.Cellar != ":any" {
		t.Errorf("bottle section = %+v", cfg.Bottle)
	}
	if cfg.Registry.RootPath != "acme/bottles" {
		t.Errorf("registry.root_path = %s", cfg.Registry.RootPath)
	}
	if cfg.Tap.Repo != "homebrew-things" || cfg.Tap.Branch != "trunk" {
		t.Errorf("tap section = %+v", cfg.Tap)
	}
	// Mixed binaries: string shorthand + full struct
	if len(cfg.Binaries) != 2 || cfg.Binaries[0].Name != "mytool" || cfg.Binaries[1].InstallPath != "libexec" {
		t.Errorf("binaries = %+v", cfg.Binaries)
	}
}

func TestLoadFormulaStringShorthand(t *testing.T) {
	cfg := loadFromYAML(t, "formula: mytool\n")
	if cfg.Formula.Name != "mytool" {
		t.Errorf("string shorthand not accepted: %+v", cfg.Formula)
	}
	// The shorthand hook builds a bare FormulaConfig, so the tri-state
	// pointers must stay nil for SetDefaults to resolve them.
	if cfg.Formula.Head != nil {
		t.Errorf("head should be unset by the string shorthand: %v", *cfg.Formula.Head)
	}
	if cfg.Formula.Build.Enabled != nil {
		t.Errorf("build.enabled should be unset by the string shorthand: %v", *cfg.Formula.Build.Enabled)
	}
}

func TestLoadHeadTriState(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want *bool
	}{
		{name: "unset", yaml: "formula:\n  name: mytool\n"},
		{name: "true", yaml: "formula:\n  name: mytool\n  head: true\n", want: boolPtr(true)},
		{name: "false", yaml: "formula:\n  name: mytool\n  head: false\n", want: boolPtr(false)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loadFromYAML(t, tt.yaml).Formula.Head
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("head = %v, want unset", *got)
			case tt.want != nil && got == nil:
				t.Errorf("head unset, want %v", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("head = %v, want %v", *got, *tt.want)
			}
		})
	}
}

func TestLoadFormulaBuild(t *testing.T) {
	cfg := loadFromYAML(t, `
formula:
  name: mytool
  build:
    enabled: true
    go: go@1.23
    packages: [./cmd/mytool]
    ldflags: "-X main.version={{.Version}}"
    tags: [netgo, osusergo]
    flags: ["-mod=vendor"]
    env:
      - FOO=bar
    mod_dir: src
source:
  type: go
  build:
    trimpath: false
`)

	b := cfg.Formula.Build
	if b.Enabled == nil || !*b.Enabled {
		t.Errorf("build.enabled = %v", b.Enabled)
	}
	if b.Go != "go@1.23" {
		t.Errorf("build.go = %q", b.Go)
	}
	if len(b.Packages) != 1 || b.Packages[0] != "./cmd/mytool" {
		t.Errorf("build.packages = %v", b.Packages)
	}
	if b.Ldflags != "-X main.version={{.Version}}" {
		t.Errorf("build.ldflags = %q", b.Ldflags)
	}
	if len(b.Tags) != 2 || b.Tags[0] != "netgo" || b.Tags[1] != "osusergo" {
		t.Errorf("build.tags = %v", b.Tags)
	}
	if len(b.Flags) != 1 || b.Flags[0] != "-mod=vendor" {
		t.Errorf("build.flags = %v", b.Flags)
	}
	if len(b.Env) != 1 || b.Env[0] != "FOO=bar" {
		t.Errorf("build.env = %v", b.Env)
	}
	if b.ModDir != "src" {
		t.Errorf("build.mod_dir = %q", b.ModDir)
	}
	if cfg.Source.Build.Trimpath == nil || *cfg.Source.Build.Trimpath {
		t.Errorf("source.build.trimpath = %v, want an explicit false", cfg.Source.Build.Trimpath)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("GOBOTTLE_REGISTRY_TOKEN", "env-token")

	dir := t.TempDir()
	path := filepath.Join(dir, ".gobottle.yaml")
	if err := os.WriteFile(path, []byte("registry:\n  token: file-token\n"), 0644); err != nil {
		t.Fatal(err)
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	v.SetEnvPrefix("GOBOTTLE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Registry.Token != "env-token" {
		t.Errorf("env should override file: got %s", cfg.Registry.Token)
	}
}

func TestSetDefaultsVersionNormalization(t *testing.T) {
	cfg := &Config{Version: "v1.2.3"}
	cfg.SetDefaults()
	if cfg.Version != "1.2.3" {
		t.Errorf("version = %s, want 1.2.3", cfg.Version)
	}
}
