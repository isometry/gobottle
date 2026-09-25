package bottle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeCompletionBinary writes an executable shell script standing in for a
// binary with a cobra-style `completion <shell>` subcommand.
func makeCompletionBinary(t *testing.T, name, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGenerateCompletions(t *testing.T) {
	bin := makeCompletionBinary(t, "mytool", "#!/bin/sh\necho \"completion-for-$2\"\n")
	dir := t.TempDir()

	files, err := GenerateCompletions(context.Background(), bin, "mytool", []string{"completion"}, dir)
	if err != nil {
		t.Fatalf("GenerateCompletions: %v", err)
	}

	want := map[string]string{
		"etc/bash_completion.d/mytool":                "completion-for-bash\n",
		"share/zsh/site-functions/_mytool":            "completion-for-zsh\n",
		"share/fish/vendor_completions.d/mytool.fish": "completion-for-fish\n",
	}
	if len(files) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(files), len(want), files)
	}
	for archivePath, content := range want {
		localPath, ok := files[archivePath]
		if !ok {
			t.Errorf("missing archive path %s (have %v)", archivePath, files)
			continue
		}
		data, err := os.ReadFile(localPath)
		if err != nil {
			t.Errorf("reading %s: %v", localPath, err)
			continue
		}
		if string(data) != content {
			t.Errorf("%s content = %q, want %q", archivePath, data, content)
		}
	}
}

func TestGenerateCompletionsFailure(t *testing.T) {
	bin := makeCompletionBinary(t, "mytool", "#!/bin/sh\nexit 1\n")

	if _, err := GenerateCompletions(context.Background(), bin, "mytool", []string{"completion"}, t.TempDir()); err == nil {
		t.Fatal("expected error when the completion command fails")
	}
}

// buildArgv0EchoBinary compiles a small real executable (not a shebang
// script) that prints os.Args[0]'s basename followed by its last argument.
// A shell script's $0 always reflects the invoked script's own path: for
// interpreter execution the kernel rewrites argv[0] to the script path
// before the interpreter ever sees it, regardless of what the caller passed
// as argv[0]. Only a genuine executable can prove GenerateCompletions sets
// cmd.Args[0] to name.
func buildArgv0EchoBinary(t *testing.T, dir, name string) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "main.go")
	source := `package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	fmt.Printf("%s-completion-for-%s\n", filepath.Base(os.Args[0]), os.Args[len(os.Args)-1])
}
`
	if err := os.WriteFile(src, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building test helper binary: %v\n%s", err, out)
	}
	return bin
}

// TestGenerateCompletionsSymlinkedBinary covers a binary configured under an
// alias, e.g. a release archive that ships a dereferenced copy under an
// unpredictable versioned name (mytool -> mytool_1.2.3), which is what
// callers pass as binaryPath after resolving through the symlink (see
// bottle.ResolveBinary). The archive paths, local file names and the
// process's own argv[0] must all use the configured name ("mytool"), not
// binaryPath's basename ("mytool_1.2.3").
func TestGenerateCompletionsSymlinkedBinary(t *testing.T) {
	binDir := t.TempDir()
	target := buildArgv0EchoBinary(t, binDir, "mytool_1.2.3")
	link := filepath.Join(binDir, "mytool")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	files, err := GenerateCompletions(context.Background(), resolved, "mytool", []string{"completion"}, dir)
	if err != nil {
		t.Fatalf("GenerateCompletions: %v", err)
	}

	want := map[string]struct {
		localName string
		content   string
	}{
		"etc/bash_completion.d/mytool":                {"mytool.bash", "mytool-completion-for-bash\n"},
		"share/zsh/site-functions/_mytool":            {"mytool.zsh", "mytool-completion-for-zsh\n"},
		"share/fish/vendor_completions.d/mytool.fish": {"mytool.fish", "mytool-completion-for-fish\n"},
	}
	if len(files) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(files), len(want), files)
	}
	for archivePath, w := range want {
		localPath, ok := files[archivePath]
		if !ok {
			t.Errorf("missing archive path %s (have %v)", archivePath, files)
			continue
		}
		if got := filepath.Base(localPath); got != w.localName {
			t.Errorf("local file for %s = %q, want %q", archivePath, got, w.localName)
		}
		data, err := os.ReadFile(localPath)
		if err != nil {
			t.Errorf("reading %s: %v", localPath, err)
			continue
		}
		if string(data) != w.content {
			t.Errorf("%s content = %q, want %q (argv[0] basename must be the installed name, not the resolved symlink target)", archivePath, data, w.content)
		}
	}
}
