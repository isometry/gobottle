package bottle

import (
	"context"
	"os"
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

	files, err := GenerateCompletions(context.Background(), bin, []string{"completion"}, dir)
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

	if _, err := GenerateCompletions(context.Background(), bin, []string{"completion"}, t.TempDir()); err == nil {
		t.Fatal("expected error when the completion command fails")
	}
}
