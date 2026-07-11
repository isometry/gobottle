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
		Completions:  true,
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
		Completions: true,
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
