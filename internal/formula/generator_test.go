package formula

import (
	"strings"
	"testing"
)

func TestGenerateFull(t *testing.T) {
	f := &Formula{
		Name:        "my-tool",
		Description: "Does useful things",
		Homepage:    "https://github.com/acme/my-tool",
		URL:         "https://github.com/acme/my-tool/archive/refs/tags/v1.2.3.tar.gz",
		SHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		License:     "MIT",
		Head:        &Head{URL: "https://github.com/acme/my-tool.git", Branch: "main"},
		RootURL:     "https://ghcr.io/v2/acme/tap",
		Rebuild:     1,
		Bottles: []BottleSpec{
			{Platform: "arm64_sonoma", SHA256: "bbbb", Cellar: ":any_skip_relocation"},
			{Platform: "x86_64_linux", SHA256: "cccc", Cellar: "/home/linuxbrew/.linuxbrew/Cellar"},
		},
		Dependencies: []string{"git"},
		Conflicts:    []string{"other-tool"},
		Binaries:     []BinaryInstall{{Name: "my-tool", InstallPath: "bin"}},
		Completions:  []string{"completion"},
		Caveats:      "Remember to breathe.",
		Test:         Test{Command: []string{"version"}},
	}

	got, err := Generate(f, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	want := `class MyTool < Formula
  desc "Does useful things"
  homepage "https://github.com/acme/my-tool"
  url "https://github.com/acme/my-tool/archive/refs/tags/v1.2.3.tar.gz"
  sha256 "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  license "MIT"
  head "https://github.com/acme/my-tool.git", branch: "main"

  bottle do
    root_url "https://ghcr.io/v2/acme/tap"
    rebuild 1
    sha256 cellar: :any_skip_relocation, arm64_sonoma: "bbbb"
    sha256 cellar: "/home/linuxbrew/.linuxbrew/Cellar", x86_64_linux: "cccc"
  end

  depends_on "git"

  conflicts_with "other-tool"

  def install
    bin.install "my-tool"
    generate_completions_from_executable(bin/"my-tool", "completion")
  end

  def caveats
    <<~EOS
      Remember to breathe.
    EOS
  end

  test do
    system bin/"my-tool", "version"
  end
end
`
	if got != want {
		t.Errorf("formula mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestGenerateSourceBuild(t *testing.T) {
	ldflags, err := RubyLdflags("-s -w -X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}}", "commit")
	if err != nil {
		t.Fatalf("RubyLdflags: %v", err)
	}

	f := &Formula{
		Name:        "gobottle",
		Description: "Build and publish Homebrew bottles for Go projects",
		Homepage:    "https://github.com/isometry/gobottle",
		URL:         "https://github.com/isometry/gobottle/archive/refs/tags/v0.8.0.tar.gz",
		SHA256:      "aaaa",
		License:     "MIT",
		Head:        &Head{URL: "https://github.com/isometry/gobottle.git", Branch: "main"},
		RootURL:     "https://ghcr.io/v2/isometry/tap",
		Bottles: []BottleSpec{
			{Platform: "arm64_sequoia", SHA256: "bbbb", Cellar: ":any_skip_relocation"},
		},
		Binaries:    []BinaryInstall{{Name: "gobottle", InstallPath: "bin"}},
		Completions: []string{"completion"},
		Test:        Test{Command: []string{"version"}},
		Build: &SourceBuild{
			GoDependency: "go",
			Env:          []EnvVar{{Key: "CGO_ENABLED", Value: "0"}},
			ModDir:       ".",
			Ldflags:      ldflags,
			Commit:       "4cce4f5000000000000000000000000000000000",
			HeadCommit:   true,
			Targets:      []GoTarget{{Package: ".", Binary: "gobottle", InstallPath: "bin"}},
		},
	}

	got, err := Generate(f, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	want := `class Gobottle < Formula
  desc "Build and publish Homebrew bottles for Go projects"
  homepage "https://github.com/isometry/gobottle"
  url "https://github.com/isometry/gobottle/archive/refs/tags/v0.8.0.tar.gz"
  sha256 "aaaa"
  license "MIT"
  head "https://github.com/isometry/gobottle.git", branch: "main"

  bottle do
    root_url "https://ghcr.io/v2/isometry/tap"
    sha256 cellar: :any_skip_relocation, arm64_sequoia: "bbbb"
  end

  depends_on "go" => :build

  def install
    ENV["CGO_ENABLED"] = "0"
    commit = build.head? ? Utils.git_head(buildpath, safe: false) : "4cce4f5000000000000000000000000000000000"
    ldflags = %W[
      -X main.version=#{version}
      -X main.commit=#{commit}
      -X main.date=#{time.iso8601}
    ]
    system "go", "build", *std_go_args(ldflags: ldflags), "."
    generate_completions_from_executable(bin/"gobottle", "completion")
  end

  test do
    system bin/"gobottle", "version"
  end
end
`
	if got != want {
		t.Errorf("formula mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestGenerateSourceBuildMultiBinary(t *testing.T) {
	f := &Formula{
		Name:         "tool",
		URL:          "https://example.com/tool-1.0.0.tar.gz",
		Dependencies: []string{"git", "jq"},
		Binaries: []BinaryInstall{
			{Name: "tool", InstallPath: "bin"},
			{Name: "helper", InstallPath: "libexec"},
			{Name: "plugin", InstallPath: "share/tool/plugins"},
		},
		Build: &SourceBuild{
			GoDependency: "go@1.23",
			ModDir:       "src",
			Ldflags:      []string{"-X", "main.version=#{version}"},
			Tags:         []string{"netgo", "osusergo"},
			Flags:        []string{"-mod=vendor"},
			Targets: []GoTarget{
				{Package: "./cmd/tool", Binary: "tool", InstallPath: "bin"},
				{Package: "./cmd/helper", Binary: "helper", InstallPath: "libexec"},
				{Package: "./cmd/plugin", Binary: "plugin", InstallPath: "share/tool/plugins"},
			},
		},
	}

	got, err := Generate(f, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	want := `class Tool < Formula
  url "https://example.com/tool-1.0.0.tar.gz"

  depends_on "go@1.23" => :build
  depends_on "git"
  depends_on "jq"

  def install
    cd "src" do
      ldflags = %W[
        -X main.version=#{version}
      ]
      system "go", "build", *std_go_args(output: bin/"tool", ldflags: ldflags, tags: ["netgo", "osusergo"]), "-mod=vendor", "./cmd/tool"
      system "go", "build", *std_go_args(output: libexec/"helper", ldflags: ldflags, tags: ["netgo", "osusergo"]), "-mod=vendor", "./cmd/helper"
      system "go", "build", *std_go_args(output: prefix/"share/tool/plugins/plugin", ldflags: ldflags, tags: ["netgo", "osusergo"]), "-mod=vendor", "./cmd/plugin"
    end
  end

  test do
    system bin/"tool", "--version"
  end
end
`
	if got != want {
		t.Errorf("formula mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestGenerateSourceBuildCommit(t *testing.T) {
	tests := []struct {
		name       string
		ldflags    []string
		commit     string
		headCommit bool
		want       string
		absent     string
	}{
		{
			name:       "head ternary with the release commit",
			ldflags:    []string{"-X", "main.commit=#{commit}"},
			commit:     "abc123",
			headCommit: true,
			want:       `    commit = build.head? ? Utils.git_head(buildpath, safe: false) : "abc123"`,
		},
		{
			name:       "head ternary falls back to tap.user",
			ldflags:    []string{"-X", "main.commit=#{commit}"},
			headCommit: true,
			want:       `    commit = build.head? ? Utils.git_head(buildpath, safe: false) : tap.user`,
		},
		{
			name:    "bare literal without a head spec",
			ldflags: []string{"-X", "main.commit=#{commit}"},
			commit:  "abc123",
			want:    `    commit = "abc123"`,
		},
		{
			name:    "unknown commit without a head spec",
			ldflags: []string{"-X", "main.commit=#{commit}"},
			want:    `    commit = tap.user`,
		},
		{
			name:       "no local when the ldflags never reference it",
			ldflags:    []string{"-X", "main.version=#{version}"},
			commit:     "abc123",
			headCommit: true,
			absent:     "commit =",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Formula{
				Name:     "tool",
				URL:      "https://example.com/tool-1.0.0.tar.gz",
				Binaries: []BinaryInstall{{Name: "tool"}},
				Build: &SourceBuild{
					GoDependency: "go",
					Ldflags:      tt.ldflags,
					Commit:       tt.commit,
					HeadCommit:   tt.headCommit,
					Targets:      []GoTarget{{Package: ".", Binary: "tool", InstallPath: "bin"}},
				},
			}
			got, err := Generate(f, "")
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Errorf("formula missing %q:\n%s", tt.want, got)
			}
			if tt.absent != "" && strings.Contains(got, tt.absent) {
				t.Errorf("formula must not contain %q:\n%s", tt.absent, got)
			}
		})
	}
}

func TestGenerateSourceBuildOmitsStdGoArgsDefaults(t *testing.T) {
	tests := []struct {
		name    string
		formula string
		targets []GoTarget
		want    string
	}{
		{
			name:    "single bin target matching the formula name omits output",
			formula: "tool",
			targets: []GoTarget{{Package: ".", Binary: "tool", InstallPath: "bin"}},
			want:    `    system "go", "build", *std_go_args(), "."`,
		},
		{
			name:    "binary differing from the formula name keeps output",
			formula: "tool",
			targets: []GoTarget{{Package: "./cmd/cli", Binary: "cli", InstallPath: "bin"}},
			want:    `    system "go", "build", *std_go_args(output: bin/"cli"), "./cmd/cli"`,
		},
		{
			name:    "empty install path is treated as bin",
			formula: "tool",
			targets: []GoTarget{{Package: ".", Binary: "tool"}},
			want:    `    system "go", "build", *std_go_args(), "."`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Formula{
				Name:     tt.formula,
				URL:      "https://example.com/tool-1.0.0.tar.gz",
				Binaries: []BinaryInstall{{Name: "tool"}},
				Build:    &SourceBuild{Targets: tt.targets},
			}
			got, err := Generate(f, "")
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("formula missing %q:\n%s", tt.want, got)
			}
		})
	}
}

func TestGenerateSourceBuildNoTargets(t *testing.T) {
	f := &Formula{
		Name:     "tool",
		URL:      "https://example.com/tool-1.0.0.tar.gz",
		Binaries: []BinaryInstall{{Name: "tool"}},
		Build:    &SourceBuild{GoDependency: "go"},
	}
	if _, err := Generate(f, ""); err == nil {
		t.Fatal("expected error for a source-build block without targets")
	}
}

func TestGenerateLegacyInstallBlock(t *testing.T) {
	f := &Formula{
		Name:         "tool",
		URL:          "https://example.com/tool-1.0.0.tar.gz",
		Dependencies: []string{"git", "jq"},
		Binaries:     []BinaryInstall{{Name: "tool"}},
	}

	got, err := Generate(f, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Runtime dependencies form one group even without a source build.
	if want := "\n  depends_on \"git\"\n  depends_on \"jq\"\n"; !strings.Contains(got, want) {
		t.Errorf("dependencies not grouped:\n%s", got)
	}
	if want := `    bin.install "tool"`; !strings.Contains(got, want) {
		t.Errorf("formula missing %q:\n%s", want, got)
	}
	for _, forbidden := range []string{"depends_on \"go\"", "std_go_args", "ldflags"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("legacy formula must not contain %q:\n%s", forbidden, got)
		}
	}
}

func TestGenerateMinimal(t *testing.T) {
	f := &Formula{
		Name:     "tool",
		URL:      "https://example.com/tool-1.0.0.tar.gz",
		Binaries: []BinaryInstall{{Name: "tool"}},
	}

	got, err := Generate(f, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	for _, forbidden := range []string{`desc ""`, `license ""`, `sha256 ""`, "bottle do", "depends_on", "caveats"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("minimal formula must not contain %q:\n%s", forbidden, got)
		}
	}
	for _, required := range []string{"class Tool < Formula", `url "https://example.com/tool-1.0.0.tar.gz"`, `system bin/"tool", "--version"`} {
		if !strings.Contains(got, required) {
			t.Errorf("minimal formula missing %q:\n%s", required, got)
		}
	}
}

func TestGenerateNoBinaries(t *testing.T) {
	_, err := Generate(&Formula{Name: "x", URL: "https://e.com/x.tar.gz"}, "")
	if err == nil {
		t.Fatal("expected error for formula without binaries")
	}
}

func TestGenerateInstallPaths(t *testing.T) {
	f := &Formula{
		Name: "tool",
		URL:  "https://example.com/tool-1.0.0.tar.gz",
		Binaries: []BinaryInstall{
			{Name: "tool", InstallPath: "bin"},
			{Name: "helper", InstallPath: "libexec"},
			{Name: "plugin", InstallPath: "share/tool/plugins"},
		},
		Completions: []string{"completion"},
	}

	got, err := Generate(f, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	for _, required := range []string{
		`    bin.install "tool"`,
		`    libexec.install "helper"`,
		`    (prefix/"share/tool/plugins").install "plugin"`,
		`    generate_completions_from_executable(bin/"tool", "completion")`,
		`    system bin/"tool", "--version"`,
	} {
		if !strings.Contains(got, required) {
			t.Errorf("formula missing %q:\n%s", required, got)
		}
	}
	for _, forbidden := range []string{
		`generate_completions_from_executable(bin/"helper"`,
		`generate_completions_from_executable(bin/"plugin"`,
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("completions generated for non-bin binary %q:\n%s", forbidden, got)
		}
	}
}

func TestGenerateCompletionsCommand(t *testing.T) {
	f := &Formula{
		Name:        "tool",
		URL:         "https://example.com/tool-1.0.0.tar.gz",
		Binaries:    []BinaryInstall{{Name: "tool"}},
		Completions: []string{"gen", "completion"},
	}

	got, err := Generate(f, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if want := `    generate_completions_from_executable(bin/"tool", "gen", "completion")`; !strings.Contains(got, want) {
		t.Errorf("formula missing %q:\n%s", want, got)
	}
}

func TestGenerateNoBinBinaryDefaultTest(t *testing.T) {
	f := &Formula{
		Name:     "tool",
		URL:      "https://example.com/tool-1.0.0.tar.gz",
		Binaries: []BinaryInstall{{Name: "helper", InstallPath: "libexec"}},
	}
	if _, err := Generate(f, ""); err == nil {
		t.Fatal("expected error: default test with no bin-installed binary")
	}

	f.Test.Raw = `assert_match "ok", shell_output(libexec/"helper")`
	if _, err := Generate(f, ""); err != nil {
		t.Fatalf("raw test should allow libexec-only formula: %v", err)
	}
}

func TestGenerateCustomTemplate(t *testing.T) {
	f := &Formula{Name: "x", URL: "u", Binaries: []BinaryInstall{{Name: "x"}}}
	got, err := Generate(f, "# custom {{ .Name }}\n")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "# custom x\n" {
		t.Errorf("custom template output = %q", got)
	}
}

func TestGenerateRawTest(t *testing.T) {
	f := &Formula{
		Name:     "x",
		URL:      "u",
		Binaries: []BinaryInstall{{Name: "x"}},
		Test:     Test{Raw: "assert_match \"x\", shell_output(bin/\"x --help\")"},
	}
	got, err := Generate(f, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(got, `    assert_match "x", shell_output(bin/"x --help")`) {
		t.Errorf("raw test not rendered:\n%s", got)
	}
}
