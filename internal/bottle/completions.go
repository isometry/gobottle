package bottle

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// completionTimeout bounds each completion-script invocation; the binary is
// untrusted input and must not hang the build.
const completionTimeout = 30 * time.Second

// completionShells maps each shell to the keg-relative path brew installs its
// completion script from, mirroring Formula#bash_completion, #zsh_completion
// and #fish_completion plus the file naming used by
// generate_completions_from_executable.
var completionShells = []struct {
	shell       string
	archivePath func(name string) string
}{
	{"bash", func(name string) string { return path.Join("etc", "bash_completion.d", name) }},
	{"zsh", func(name string) string { return path.Join("share", "zsh", "site-functions", "_"+name) }},
	{"fish", func(name string) string { return path.Join("share", "fish", "vendor_completions.d", name+".fish") }},
}

// GenerateCompletions runs `<binaryPath> <command...> <shell>` for bash, zsh
// and fish, writes each captured script under dir, and returns keg-relative
// archive path -> local path entries suitable for BuildOptions.ExtraFiles.
// Any shell failing to generate is an error: shipping partial completions
// silently is worse than failing the build.
func GenerateCompletions(ctx context.Context, binaryPath string, command []string, dir string) (map[string]string, error) {
	name := filepath.Base(binaryPath)
	files := make(map[string]string, len(completionShells))

	for _, s := range completionShells {
		args := append(append([]string{}, command...), s.shell)

		cctx, cancel := context.WithTimeout(ctx, completionTimeout)
		cmd := exec.CommandContext(cctx, binaryPath, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("failed to generate %s completions (%s %s): %w: %s",
				s.shell, name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}

		localPath := filepath.Join(dir, name+"."+s.shell)
		if err := os.WriteFile(localPath, stdout.Bytes(), 0644); err != nil {
			return nil, fmt.Errorf("failed to write %s completions: %w", s.shell, err)
		}
		files[s.archivePath(name)] = localPath
	}

	return files, nil
}
