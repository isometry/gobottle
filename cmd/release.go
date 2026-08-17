package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"text/template"

	"github.com/isometry/gobottle/internal/config"
	"github.com/isometry/gobottle/internal/formula"
	"github.com/isometry/gobottle/internal/git"
	"github.com/isometry/gobottle/internal/tap"
	"github.com/isometry/gobottle/internal/util"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewReleaseCommand creates the `release` verb: generate the formula and
// commit it to the tap. Given a manifest (stdin or --input) it releases
// already-pushed bottles; otherwise it runs the full build+push pipeline
// first.
func NewReleaseCommand(rootOpts *Options) *cobra.Command {
	var input, output, outputDir, tapPath string

	cmd := &cobra.Command{
		Use:   "release [formula]",
		Short: "Generate the tap formula (building and pushing first if needed)",
		Long: `Generate the Homebrew formula from configuration and bottle data, and
commit it to the tap repository.

With a bottles.json manifest on stdin or --input, only the formula step runs
(the bottles must already be pushed). Without one, the full pipeline runs:
build -> push -> release.

The formula is fully generated on every release: description, license,
dependencies, caveats, test block etc. are driven by the formula: section of
.gobottle.yaml (see also formula.template for a full template override).`,
		Example: `  # full pipeline: build, push, update tap
  gobottle release --source go --packages .

  # staged: release previously pushed bottles
  gobottle push -o json < bottles.json | gobottle release -i -

  # iterate on formula content without touching anything
  gobottle release --dry-run`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := bindFlags(cmd, buildFlagBindings); err != nil {
				return err
			}
			cfg, err := loadConfig(args)
			if err != nil {
				return err
			}
			if err := cfg.ValidateFor(!rootOpts.DryRun); err != nil {
				return err
			}
			if cfg.Formula.Description == "" {
				warn("formula.description is empty - brew audit will complain")
			}
			if !rootOpts.DryRun {
				if repo, err := git.Open("."); err == nil {
					if dirty, err := repo.HasUncommittedChanges(); err == nil && dirty {
						warn("working tree has uncommitted changes - published bottles will not match the %s tag", cfg.Version)
					}
				}
			}

			// Obtain a manifest: from --input, or by running the pipeline.
			var manifest *Manifest
			if input != "" {
				if manifest, err = readManifest(input); err != nil {
					return err
				}
			} else {
				if manifest, err = runBuild(cmd.Context(), cfg, outputDir); err != nil {
					return err
				}
				if err = runPush(cmd.Context(), manifest, cfg.Registry.Token, rootOpts.DryRun); err != nil {
					return err
				}
			}

			if err := runRelease(cmd.Context(), cfg, manifest, tapPath, rootOpts.DryRun); err != nil {
				return err
			}

			if output == "json" {
				return emitManifest(manifest)
			}
			return nil
		},
	}

	addBuildFlags(cmd)
	cmd.Flags().StringVarP(&input, "input", "i", "", "bottles.json manifest of already-pushed bottles ('-' reads stdin)")
	cmd.Flags().StringVar(&outputDir, "output-dir", "bottles", "directory for bottles and manifest (one-shot mode)")
	cmd.Flags().StringVar(&tapPath, "tap-path", "", "write the formula into this local tap checkout instead of committing via the GitHub API")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: 'text' or 'json'")

	return cmd
}

// runRelease renders the final formula (with bottle block) and delivers it
// to the tap.
func runRelease(ctx context.Context, cfg *config.Config, manifest *Manifest, tapPath string, dryRun bool) error {
	// The manifest carries the commit the bottles were built from, so a
	// `build | push | release` pipeline split across machines still bakes the
	// right commit into the formula's source-build ldflags.
	commit := manifest.Source.Commit
	if commit == "" {
		commit = resolveReleaseCommit(cfg)
	}

	model, tmpl, err := formulaModel(cfg, commit)
	if err != nil {
		return err
	}

	// The manifest is authoritative for what was actually bottled.
	model.RootURL = manifest.RootURL
	if model.RootURL == "" {
		model.RootURL = cfg.RootURL()
	}
	model.Rebuild = manifest.Rebuild
	// Source URL/SHA precedence: explicit flag/config > manifest > derived.
	if cfg.Source.URL == "" && manifest.Source.URL != "" {
		model.URL = manifest.Source.URL
	}
	if cfg.Source.SHA256 == "" && manifest.Source.SHA256 != "" {
		model.SHA256 = manifest.Source.SHA256
	}
	for _, e := range manifest.Bottles {
		model.Bottles = append(model.Bottles, formula.BottleSpec{
			Platform: e.Platform,
			SHA256:   e.SHA256,
			Cellar:   e.Cellar,
		})
	}

	// The stable url/sha256 are required in the published formula.
	if model.URL == "" {
		return fmt.Errorf("cannot generate formula: no source URL\n  Hint: pass --url, or set source.owner/repo so it can be derived")
	}
	if model.SHA256 == "" {
		progress("Fetching source tarball to compute SHA256...")
		sha, err := util.ComputeSHA256FromURL(ctx, model.URL)
		if err != nil {
			if !dryRun {
				return fmt.Errorf("failed to compute source SHA256: %w\n  Hint: pass --sha256, or ensure the release tag exists at %s", err, model.URL)
			}
			// Keep the dry-run formula-iteration loop usable before the
			// release tag exists.
			warn("could not fetch source tarball (%v); sha256 omitted from preview", err)
		} else {
			model.SHA256 = sha
			manifest.Source.SHA256 = sha
		}
	}

	content, err := formula.Generate(model, tmpl)
	if err != nil {
		return fmt.Errorf("failed to generate formula: %w", err)
	}

	formulaPath := path.Join(manifest.Tap.FormulaPath, manifest.Formula+".rb")

	if dryRun {
		progress("[DRY RUN] formula for %s/%s/%s:", manifest.Tap.Owner, manifest.Tap.Repo, formulaPath)
		fmt.Print(content)
		return nil
	}

	if tapPath != "" {
		dest := filepath.Join(tapPath, filepath.FromSlash(formulaPath))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return fmt.Errorf("failed to create formula directory: %w", err)
		}
		if err := os.WriteFile(dest, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write formula: %w", err)
		}
		progress("Wrote %s (commit it to publish)", dest)
		return nil
	}

	commitMsg, err := renderCommitMessage(cfg.Tap.CommitMessage, manifest)
	if err != nil {
		return err
	}

	tapToken := cfg.Tap.Token
	if tapToken == "" {
		tapToken = viper.GetString("tap.token")
	}
	updater, err := tap.NewUpdater(tapToken, manifest.Tap.Owner, manifest.Tap.Repo, manifest.Tap.Branch)
	if err != nil {
		return err
	}
	sha, err := updater.UpdateFormula(ctx, formulaPath, []byte(content), commitMsg)
	if err != nil {
		return fmt.Errorf("failed to commit formula: %w", err)
	}
	manifest.Tap.Branch = updater.Branch()
	manifest.Tap.CommitSHA = sha
	progress("Committed %s to %s/%s@%s (%s)", formulaPath, manifest.Tap.Owner, manifest.Tap.Repo, manifest.Tap.Branch, sha)

	return nil
}

// renderCommitMessage renders the tap.commit_message template
// ({{ .Formula }}, {{ .Version }}, {{ .Rebuild }}).
func renderCommitMessage(tmpl string, manifest *Manifest) (string, error) {
	t, err := template.New("commit").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("invalid tap.commit_message template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, map[string]any{
		"Formula": manifest.Formula,
		"Version": manifest.Version,
		"Rebuild": manifest.Rebuild,
	}); err != nil {
		return "", fmt.Errorf("failed to render commit message: %w", err)
	}
	return buf.String(), nil
}
