package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/isometry/gobottle/internal/oci"
	"github.com/isometry/gobottle/internal/util"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/isometry/gobottle/internal/bottle"
)

// NewPushCommand creates the `push` verb: publish built bottles to the
// registry from a bottles.json manifest.
func NewPushCommand(rootOpts *Options) *cobra.Command {
	var input, output string

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push built bottles to the OCI registry",
		Long: `Push bottles described by a bottles.json manifest (from 'gobottle build')
to the registry as one OCI index per version, using the exact conventions
brew expects when pouring from GHCR.

Re-pushing is idempotent: existing other-platform manifests at the same
version tag are preserved.`,
		Example: `  gobottle build -o json | gobottle push
  gobottle push --input bottles/bottles.json -o json > pushed.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest, err := readManifest(input)
			if err != nil {
				return err
			}

			token := viper.GetString("registry.token")
			if token == "" {
				token = os.Getenv("GITHUB_TOKEN")
			}
			if token == "" {
				token = os.Getenv("GH_TOKEN")
			}
			if token == "" && !rootOpts.DryRun {
				return fmt.Errorf("registry token required: set GITHUB_TOKEN (write:packages scope)")
			}

			if err := runPush(cmd.Context(), manifest, token, rootOpts.DryRun); err != nil {
				return err
			}

			if output == "json" {
				return emitManifest(manifest)
			}
			for _, e := range manifest.Bottles {
				fmt.Printf("%s\t%s\n", e.Platform, e.Ref)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&input, "input", "i", "", "bottles.json manifest (default: stdin)")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: 'text' or 'json'")

	return cmd
}

// runPush publishes the manifest's bottles and annotates the manifest with
// the pushed references. Shared by `push` and one-shot `release`.
func runPush(ctx context.Context, manifest *Manifest, token string, dryRun bool) error {
	bottles := make([]*bottle.Bottle, 0, len(manifest.Bottles))
	for i := range manifest.Bottles {
		e := &manifest.Bottles[i]
		b, err := e.toBottle(manifest)
		if err != nil {
			return err
		}

		// Guard against manifest/file drift: the file must still hash to the
		// digest brew will look up.
		actual, err := util.ComputeSHA256(b.Path)
		if err != nil {
			return fmt.Errorf("bottle file missing for %s: %w (re-run 'gobottle build')", e.Platform, err)
		}
		if actual != e.SHA256 {
			return fmt.Errorf("bottle %s no longer matches its manifest SHA256 (re-run 'gobottle build')", b.Path)
		}
		bottles = append(bottles, b)
	}

	imageRef := func(b *bottle.Bottle) string {
		return fmt.Sprintf("%s/%s/%s:%s",
			manifest.Registry.Host, manifest.Registry.RootPath,
			oci.ImageFormulaName(manifest.Formula), b.VersionRebuild())
	}

	if dryRun {
		progress("[DRY RUN] would push %d bottles", len(bottles))
		for i, b := range bottles {
			manifest.Bottles[i].Ref = imageRef(b)
			progress("  %s -> %s", b.BottleName(), manifest.Bottles[i].Ref)
		}
		return nil
	}

	publisher, err := oci.NewPublisher(
		manifest.Registry.Host,
		manifest.Registry.RootPath,
		manifest.Formula,
		token,
		oci.PublishOptions{
			SourceURL:   fmt.Sprintf("https://github.com/%s/%s", manifest.Tap.Owner, manifest.Tap.Repo),
			Homepage:    manifest.Homepage,
			License:     manifest.License,
			Description: manifest.Description,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create OCI publisher: %w", err)
	}

	results, err := publisher.Push(ctx, bottles, func(msg string) { progress("  %s", msg) })
	if err != nil {
		return fmt.Errorf("failed to push bottles: %w", err)
	}

	byRefName := map[string]*oci.Result{}
	for i := range results {
		for _, b := range results[i].Bottles {
			byRefName[b.RefName()] = &results[i]
		}
	}
	for i := range manifest.Bottles {
		b := bottles[i]
		if res := byRefName[b.RefName()]; res != nil {
			manifest.Bottles[i].Ref = res.Ref
			manifest.Bottles[i].Pushed = true
		}
	}

	for _, res := range results {
		if public, err := publisher.AnonymouslyAccessible(ctx, res.Tag); err == nil && !public {
			warn("%s is not anonymously accessible - brew cannot pour from it.\n"+
				"  Make the GHCR package public (Package settings -> Change visibility).", res.Ref)
		}
	}

	return nil
}
