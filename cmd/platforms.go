package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/isometry/gobottle/internal/platform"
	"github.com/spf13/cobra"
)

// PlatformsOptions holds options for the platforms command
type PlatformsOptions struct {
	Refresh bool
	JSON    bool
}

// NewPlatformsCommand creates the platforms command
func NewPlatformsCommand(rootOpts *Options) *cobra.Command {
	opts := &PlatformsOptions{}

	cmd := &cobra.Command{
		Use:   "platforms",
		Short: "List supported Homebrew bottle platforms",
		Long: `List all supported Homebrew bottle platforms discovered from Homebrew's source.

Platforms are auto-discovered from Homebrew's version.rb and cached locally.
Use --refresh to force refresh the cache.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlatforms(cmd, rootOpts, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Refresh, "refresh", false, "force refresh the platform cache")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "output in JSON format")

	return cmd
}

func runPlatforms(cmd *cobra.Command, rootOpts *Options, opts *PlatformsOptions) error {
	cacheDir, err := GetCacheDir()
	if err != nil {
		return err
	}

	discoverer, err := platform.NewDiscoverer(cacheDir)
	if err != nil {
		return fmt.Errorf("failed to create platform discoverer: %w", err)
	}

	info, err := discoverer.Discover(cmd.Context(), opts.Refresh)
	if err != nil {
		return fmt.Errorf("failed to discover platforms: %w", err)
	}

	if opts.JSON {
		output := struct {
			MacOS []struct {
				Symbol  string   `json:"symbol"`
				Version int      `json:"version"`
				Tags    []string `json:"tags"`
			} `json:"macos"`
			Linux struct {
				Architectures []string `json:"architectures"`
				Tags          []string `json:"tags"`
			} `json:"linux"`
		}{
			MacOS: make([]struct {
				Symbol  string   `json:"symbol"`
				Version int      `json:"version"`
				Tags    []string `json:"tags"`
			}, len(info.MacOSVersions)),
			Linux: struct {
				Architectures []string `json:"architectures"`
				Tags          []string `json:"tags"`
			}{
				Architectures: info.LinuxArches,
				Tags:          make([]string, len(info.LinuxArches)),
			},
		}

		// Populate macOS entries
		for i, v := range info.MacOSVersions {
			output.MacOS[i].Symbol = v.Symbol
			output.MacOS[i].Version = v.Major
			output.MacOS[i].Tags = []string{
				fmt.Sprintf("arm64_%s", v.Symbol),
				v.Symbol, // Intel is just the symbol
			}
		}

		// Populate Linux tags
		for i, arch := range info.LinuxArches {
			output.Linux.Tags[i] = fmt.Sprintf("%s_linux", arch)
		}

		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
	}

	fmt.Println("Discovered Homebrew bottle platforms:")
	fmt.Println()
	fmt.Print(platform.FormatPlatformList(info))

	return nil
}
