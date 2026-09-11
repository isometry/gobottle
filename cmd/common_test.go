package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isometry/gobottle/internal/config"
	"github.com/isometry/gobottle/internal/formula"
)

// baseConfig is a realized configuration (SetDefaults applied) for a
// single-binary go-source project.
func baseConfig(t *testing.T, mutate func(*config.Config)) *config.Config {
	t.Helper()
	cfg := &config.Config{
		Formula: config.FormulaConfig{Name: "mytool"},
		Version: "1.2.3",
		Source: config.SourceConfig{
			Type:  "go",
			Owner: "acme",
			Repo:  "mytool",
			Build: config.BuildConfig{
				Packages: []string{"."},
				Ldflags:  "-s -w -X main.version={{.Version}} -X main.commit={{.Commit}}",
			},
		},
	}
	if mutate != nil {
		mutate(cfg)
	}
	cfg.SetDefaults()
	return cfg
}

func TestFormulaSourceBuild(t *testing.T) {
	cfg := baseConfig(t, nil)

	build, err := formulaSourceBuild(cfg, "abc123", true)
	if err != nil {
		t.Fatalf("formulaSourceBuild: %v", err)
	}
	if build == nil {
		t.Fatal("expected a source-build block")
	}
	if build.GoDependency != "go" {
		t.Errorf("GoDependency = %q", build.GoDependency)
	}
	if len(build.Targets) != 1 || build.Targets[0].Package != "." ||
		build.Targets[0].Binary != "mytool" || build.Targets[0].InstallPath != "bin" {
		t.Errorf("Targets = %+v", build.Targets)
	}
	if len(build.Env) != 1 || build.Env[0] != (formula.EnvVar{Key: "CGO_ENABLED", Value: "0"}) {
		t.Errorf("Env = %+v", build.Env)
	}
	// std_go_args supplies -s -w itself.
	for _, tok := range build.Ldflags {
		if tok == "-s" || tok == "-w" {
			t.Errorf("ldflags must not carry %q: %q", tok, build.Ldflags)
		}
	}
	if got := strings.Join(build.Ldflags, " "); got != "-X main.version=#{version} -X main.commit=#{commit}" {
		t.Errorf("Ldflags = %q", got)
	}
	if build.Commit != "abc123" || !build.HeadCommit {
		t.Errorf("Commit = %q, HeadCommit = %v", build.Commit, build.HeadCommit)
	}
}

func TestFormulaSourceBuildCGOEnabled(t *testing.T) {
	cfg := baseConfig(t, func(c *config.Config) { c.Source.Build.CGOEnabled = true })

	build, err := formulaSourceBuild(cfg, "", false)
	if err != nil {
		t.Fatalf("formulaSourceBuild: %v", err)
	}
	if build.Env[0] != (formula.EnvVar{Key: "CGO_ENABLED", Value: "1"}) {
		t.Errorf("Env[0] = %+v, want CGO_ENABLED=1", build.Env[0])
	}
}

func TestFormulaSourceBuildEnv(t *testing.T) {
	cfg := baseConfig(t, func(c *config.Config) {
		c.Source.Build.Env = []string{"GOFLAGS=-mod=vendor", "GOPRIVATE=", "bogus"}
	})

	build, err := formulaSourceBuild(cfg, "", false)
	if err != nil {
		t.Fatalf("formulaSourceBuild: %v", err)
	}
	want := []formula.EnvVar{
		{Key: "CGO_ENABLED", Value: "0"},
		{Key: "GOFLAGS", Value: "-mod=vendor"},
		{Key: "GOPRIVATE", Value: ""},
	}
	if len(build.Env) != len(want) {
		t.Fatalf("Env = %+v, want %+v", build.Env, want)
	}
	for i := range want {
		if build.Env[i] != want[i] {
			t.Errorf("Env[%d] = %+v, want %+v", i, build.Env[i], want[i])
		}
	}
}

func TestFormulaSourceBuildDisabled(t *testing.T) {
	cfg := baseConfig(t, func(c *config.Config) {
		c.Formula.Build.Enabled = new(bool) // explicit false
	})

	build, err := formulaSourceBuild(cfg, "abc123", true)
	if err != nil {
		t.Fatalf("formulaSourceBuild: %v", err)
	}
	if build != nil {
		t.Errorf("expected the legacy bin.install block, got %+v", build)
	}
}

func TestFormulaSourceBuildPackageDefaults(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*config.Config)
		wantNil  bool
		wantPkgs []string
		wantBins []string
	}{
		{
			name:     "single binary without packages assumes the module root",
			mutate:   func(c *config.Config) { c.Source.Build.Packages = nil },
			wantPkgs: []string{"."},
			wantBins: []string{"mytool"},
		},
		{
			name: "multiple binaries without packages fall back to bin.install",
			mutate: func(c *config.Config) {
				c.Source.Build.Packages = nil
				c.Binaries = []config.BinaryConfig{{Name: "mytool"}, {Name: "helper"}}
			},
			wantNil: true,
		},
		{
			name: "explicit packages pair with binaries by index",
			mutate: func(c *config.Config) {
				c.Source.Build.Packages = []string{"./cmd/mytool", "./cmd/helper"}
				c.Binaries = []config.BinaryConfig{
					{Name: "mytool"},
					{Name: "helper", InstallPath: "libexec"},
				}
			},
			wantPkgs: []string{"./cmd/mytool", "./cmd/helper"},
			wantBins: []string{"mytool", "helper"},
		},
		{
			name: "binary names derive from the package basename on overflow",
			mutate: func(c *config.Config) {
				c.Source.Build.Packages = []string{"./cmd/mytool", "./cmd/helper"}
			},
			wantPkgs: []string{"./cmd/mytool", "./cmd/helper"},
			wantBins: []string{"mytool", "helper"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig(t, tt.mutate)
			build, err := formulaSourceBuild(cfg, "", false)
			if err != nil {
				t.Fatalf("formulaSourceBuild: %v", err)
			}
			if tt.wantNil {
				if build != nil {
					t.Fatalf("expected the legacy bin.install block, got %+v", build.Targets)
				}
				return
			}
			if build == nil {
				t.Fatal("expected a source-build block")
			}
			if len(build.Targets) != len(tt.wantPkgs) {
				t.Fatalf("Targets = %+v, want %d", build.Targets, len(tt.wantPkgs))
			}
			for i, target := range build.Targets {
				if target.Package != tt.wantPkgs[i] || target.Binary != tt.wantBins[i] {
					t.Errorf("Targets[%d] = %+v, want %s -> %s", i, target, tt.wantPkgs[i], tt.wantBins[i])
				}
			}
		})
	}
}

func TestFormulaModelHead(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*config.Config)
		wantHead bool
	}{
		{name: "defaults on with a derivable git URL", wantHead: true},
		{
			name:   "explicit false suppresses the stanza",
			mutate: func(c *config.Config) { c.Formula.Head = new(bool) },
		},
		{
			name: "no git URL means no head stanza",
			mutate: func(c *config.Config) {
				c.Source.Owner = ""
				c.Source.Repo = ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig(t, tt.mutate)
			model, _, err := formulaModel(cfg, "abc123")
			if err != nil {
				t.Fatalf("formulaModel: %v", err)
			}
			if (model.Head != nil) != tt.wantHead {
				t.Errorf("Head = %+v, want head: %v", model.Head, tt.wantHead)
			}
			// The build.head? branch exists exactly when a head spec does.
			if model.Build != nil && model.Build.HeadCommit != tt.wantHead {
				t.Errorf("Build.HeadCommit = %v, want %v", model.Build.HeadCommit, tt.wantHead)
			}
		})
	}
}

func TestFormulaModelHeadRequiresSourceBuild(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{
			name: "explicit head with the source build disabled",
			mutate: func(c *config.Config) {
				c.Formula.Head = boolPtr(true)
				c.Formula.Build.Enabled = boolPtr(false)
			},
		},
		{
			name: "multi-binary fallback to bin.install",
			mutate: func(c *config.Config) {
				c.Source.Build.Packages = nil
				c.Binaries = []config.BinaryConfig{{Name: "mytool"}, {Name: "helper"}}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseConfig(t, tt.mutate)
			model, _, err := formulaModel(cfg, "abc123")
			if err != nil {
				t.Fatalf("formulaModel: %v", err)
			}
			if model.Build != nil {
				t.Fatalf("expected the legacy bin.install block, got %+v", model.Build)
			}
			if model.Head != nil {
				t.Errorf("head stanza %+v emitted although the install block cannot build a checkout", model.Head)
			}
		})
	}
}

func TestFormulaSourceBuildDropsTrimpath(t *testing.T) {
	cfg := baseConfig(t, func(c *config.Config) {
		c.Source.Build.Flags = []string{"-trimpath", "-mod=vendor"}
		c.Source.Build.Tags = []string{"netgo"}
	})
	build, err := formulaSourceBuild(cfg, "abc123", true)
	if err != nil {
		t.Fatalf("formulaSourceBuild: %v", err)
	}
	if len(build.Flags) != 1 || build.Flags[0] != "-mod=vendor" {
		t.Errorf("Flags = %q, want -trimpath dropped (std_go_args adds it)", build.Flags)
	}
	if len(build.Tags) != 1 || build.Tags[0] != "netgo" {
		t.Errorf("Tags = %q, want inherited from source.build.tags", build.Tags)
	}
}

func TestHasMainPackage(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("lib.go", "package lib\n")
	write("lib_test.go", "package main\n") // tests don't count
	write("cmd/tool/main.go", "package main\n\nfunc main() {}\n")
	write("cmd/broken/main.go", "not go\n")

	tests := []struct {
		pkg  string
		want bool
	}{
		{".", false},
		{"./cmd/tool", true},
		{"./cmd/broken", false},
		{"./cmd/missing", true},            // unreadable: assume fine
		{"github.com/acme/tool/cmd", true}, // import path: not checked
	}
	for _, tt := range tests {
		if got := hasMainPackage(dir, tt.pkg); got != tt.want {
			t.Errorf("hasMainPackage(%q) = %v, want %v", tt.pkg, got, tt.want)
		}
	}
}

func boolPtr(b bool) *bool { return &b }
