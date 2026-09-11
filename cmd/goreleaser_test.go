package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranslateGoreleaserTemplate(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  string
	}{
		{in: "-s -w", want: "-s -w"},
		{in: "-X main.v={{ .Version }}", want: "-X main.v={{.Version}}"},
		{in: "-X main.v={{.Version}}", want: "-X main.v={{.Version}}"},
		{in: "-X main.t={{- .Tag -}}", want: "-X main.t={{.Tag}}"},
		{in: "-X main.c={{ .Commit }}", want: "-X main.c={{.Commit}}"},
		{in: "-X main.c={{ .FullCommit }}", want: "-X main.c={{.Commit}}"},
		{in: "-X main.c={{ .ShortCommit }}", want: "-X main.c={{.ShortCommit}}"},
		{in: "-X main.d={{ .Date }}", want: "-X main.d={{.Date}}"},
		{in: "-X main.d={{ .CommitDate }}", want: "-X main.d={{.Date}}"},
		{
			in:   "-X a={{ .Version }} -X b={{ .ShortCommit }} -X c={{ .CommitDate }}",
			want: "-X a={{.Version}} -X b={{.ShortCommit}} -X c={{.Date}}",
		},
		{in: "-X main.e={{ .Env.FOO }}", wantErr: "{{ .Env.FOO }}"},
		{in: "-X main.t={{ .CommitTimestamp }}", wantErr: ".CommitTimestamp"},
		{in: `-X main.x={{ printf "%s" .Version }}`, wantErr: "printf"},
	}
	for _, tt := range tests {
		got, err := translateGoreleaserTemplate(tt.in)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("translate(%q) err = %v, want containing %q", tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("translate(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("translate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestImportGoReleaserBuild(t *testing.T) {
	b := goreleaserBuild{
		Binary: "mytool",
		Main:   "./cmd/mytool",
		Dir:    "src",
		Ldflags: []string{
			"-s -w",
			"-X main.version={{ .Version }} -X main.commit={{ .ShortCommit }}",
		},
		Flags: []string{"-trimpath", "-mod=vendor"},
		Env:   []string{"CGO_ENABLED=1", "GOFLAGS=-buildvcs=false"},
		Tags:  []string{"netgo"},
	}
	got := importGoReleaserBuild(b)

	if got.Binary != "mytool" {
		t.Errorf("Binary = %q", got.Binary)
	}
	if len(got.Packages) != 1 || got.Packages[0] != "./cmd/mytool" {
		t.Errorf("Packages = %q", got.Packages)
	}
	if got.ModDir != "src" {
		t.Errorf("ModDir = %q", got.ModDir)
	}
	if want := "-s -w -X main.version={{.Version}} -X main.commit={{.ShortCommit}}"; got.Ldflags != want {
		t.Errorf("Ldflags = %q, want %q", got.Ldflags, want)
	}
	if len(got.Flags) != 1 || got.Flags[0] != "-mod=vendor" {
		t.Errorf("Flags = %q, want -trimpath dropped", got.Flags)
	}
	if !got.CGOEnabled {
		t.Error("CGO_ENABLED=1 should set CGOEnabled")
	}
	if len(got.Env) != 1 || got.Env[0] != "GOFLAGS=-buildvcs=false" {
		t.Errorf("Env = %q, want CGO_ENABLED stripped", got.Env)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "netgo" {
		t.Errorf("Tags = %q", got.Tags)
	}
	if len(got.Warnings) != 0 {
		t.Errorf("Warnings = %q", got.Warnings)
	}
}

func TestImportGoReleaserBuildUnsupportedLdflags(t *testing.T) {
	got := importGoReleaserBuild(goreleaserBuild{
		Ldflags: []string{"-X main.v={{ .Env.VERSION }}"},
		Env:     []string{"CGO_ENABLED=0"},
		Dir:     ".",
	})
	if got.Ldflags != "" {
		t.Errorf("Ldflags = %q, want empty for an untranslatable template", got.Ldflags)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], ".Env.VERSION") {
		t.Errorf("Warnings = %q", got.Warnings)
	}
	if got.CGOEnabled || len(got.Env) != 0 || got.ModDir != "" {
		t.Errorf("CGOEnabled = %v, Env = %q, ModDir = %q", got.CGOEnabled, got.Env, got.ModDir)
	}
}

func TestReadGoReleaserConfigShapes(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "scalar fields",
			yaml: `
project_name: mytool
builds:
  - main: ./cmd/mytool
    ldflags: -s -w -X main.v={{ .Version }}
    flags: -trimpath
    env: CGO_ENABLED=0
`,
		},
		{
			name: "list fields",
			yaml: `
project_name: mytool
builds:
  - main: ./cmd/mytool
    ldflags:
      - -s -w
      - -X main.v={{ .Version }}
    flags:
      - -trimpath
    env:
      - CGO_ENABLED=0
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ".goreleaser.yaml"), []byte(tt.yaml), 0644); err != nil {
				t.Fatal(err)
			}
			cfg, err := readGoReleaserConfig(dir)
			if err != nil {
				t.Fatalf("readGoReleaserConfig: %v", err)
			}
			if cfg == nil || len(cfg.Builds) != 1 {
				t.Fatalf("cfg = %+v", cfg)
			}
			b := importGoReleaserBuild(cfg.Builds[0])
			if b.Ldflags != "-s -w -X main.v={{.Version}}" {
				t.Errorf("Ldflags = %q", b.Ldflags)
			}
			if len(b.Flags) != 0 {
				t.Errorf("Flags = %q", b.Flags)
			}
			if b.CGOEnabled || len(b.Env) != 0 {
				t.Errorf("CGOEnabled = %v, Env = %q", b.CGOEnabled, b.Env)
			}
		})
	}
}

func TestReadGoReleaserConfigMissing(t *testing.T) {
	cfg, err := readGoReleaserConfig(t.TempDir())
	if err != nil || cfg != nil {
		t.Errorf("missing config: cfg = %v, err = %v", cfg, err)
	}
}

func TestDetectGoReleaserImportsBuild(t *testing.T) {
	dir := t.TempDir()
	const yamlText = `
version: 2
builds:
  - main: .
    binary: gobottle
    env:
      - CGO_ENABLED=0
    flags:
      - -trimpath
    ldflags:
      - >-
        -s -w
        -X github.com/acme/gobottle/cmd.Version={{ .Version }}
        -X github.com/acme/gobottle/cmd.Commit={{ .ShortCommit }}
        -X github.com/acme/gobottle/cmd.Date={{ .CommitDate }}
`
	if err := os.WriteFile(filepath.Join(dir, ".goreleaser.yaml"), []byte(yamlText), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &initFileConfig{}
	cfg.Source.Build.Packages = []string{"./cmd/other"}
	imported, err := detectGoReleaser(cfg, dir)
	if err != nil {
		t.Fatalf("detectGoReleaser: %v", err)
	}
	if !imported {
		t.Fatal("expected the goreleaser build to be imported")
	}
	if cfg.Formula.Name != "gobottle" {
		t.Errorf("Formula.Name = %q", cfg.Formula.Name)
	}
	if len(cfg.Source.Build.Packages) != 1 || cfg.Source.Build.Packages[0] != "." {
		t.Errorf("Packages = %q", cfg.Source.Build.Packages)
	}
	want := "-s -w -X github.com/acme/gobottle/cmd.Version={{.Version}} -X github.com/acme/gobottle/cmd.Commit={{.ShortCommit}} -X github.com/acme/gobottle/cmd.Date={{.Date}}"
	if cfg.Source.Build.Ldflags != want {
		t.Errorf("Ldflags = %q\n want %q", cfg.Source.Build.Ldflags, want)
	}
	if len(cfg.Source.Build.Flags) != 0 || len(cfg.Source.Build.Env) != 0 || cfg.Source.Build.CGOEnabled {
		t.Errorf("Flags = %q, Env = %q, CGOEnabled = %v", cfg.Source.Build.Flags, cfg.Source.Build.Env, cfg.Source.Build.CGOEnabled)
	}
}

func TestDetectGoReleaserNoConfig(t *testing.T) {
	cfg := &initFileConfig{}
	cfg.Source.Build.Packages = []string{"."}
	imported, err := detectGoReleaser(cfg, t.TempDir())
	if err != nil || imported {
		t.Errorf("imported = %v, err = %v", imported, err)
	}
	if len(cfg.Source.Build.Packages) != 1 || cfg.Source.Build.Packages[0] != "." {
		t.Errorf("defaults must be untouched: %q", cfg.Source.Build.Packages)
	}
}
