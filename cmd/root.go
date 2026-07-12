package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Options holds global command options
type Options struct {
	ConfigFile string
	LogLevel   string
	DryRun     bool
}

// NewRootCommand creates the root cobra command
func NewRootCommand() *cobra.Command {
	opts := &Options{}

	cmd := &cobra.Command{
		Use:   "gobottle",
		Short: "Transform GoReleaser artifacts into Homebrew bottles",
		Long: `Gobottle creates Homebrew bottles from GoReleaser release artifacts
and pushes them to GitHub Container Registry (GHCR).

It supports fetching artifacts from GitHub Releases or a local dist/ directory,
generating bottles for all supported macOS and Linux platforms (auto-discovered),
and updating your Homebrew tap formula with the bottle block.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initConfig(opts)
		},
		SilenceUsage: true,
	}

	// Global flags
	cmd.PersistentFlags().StringVar(&opts.ConfigFile, "config", "",
		"config file (default is .gobottle.yaml)")
	cmd.PersistentFlags().StringVar(&opts.LogLevel, "log-level", "info",
		"log level (debug, info, warn, error)")
	cmd.PersistentFlags().BoolVar(&opts.DryRun, "dry-run", false,
		"perform a dry run without pushing to registry or updating tap")

	_ = viper.BindPFlag("log_level", cmd.PersistentFlags().Lookup("log-level"))
	_ = viper.BindPFlag("dry_run", cmd.PersistentFlags().Lookup("dry-run"))

	// Add subcommands
	cmd.AddCommand(NewBuildCommand(opts))
	cmd.AddCommand(NewPushCommand(opts))
	cmd.AddCommand(NewReleaseCommand(opts))
	cmd.AddCommand(NewPlatformsCommand(opts))
	cmd.AddCommand(NewVersionCommand())
	cmd.AddCommand(NewCompletionCommand())
	cmd.AddCommand(NewInitCommand())

	return cmd
}

// Execute runs the root command
func Execute() error {
	return NewRootCommand().Execute()
}

// initConfig reads in config file and ENV variables if set.
func initConfig(opts *Options) error {
	if opts.ConfigFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(opts.ConfigFile)
	} else {
		// Find current directory.
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Search config in current directory with name ".gobottle" (without extension).
		viper.AddConfigPath(cwd)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".gobottle")
	}

	// Read in environment variables that match: GOBOTTLE_REGISTRY_TOKEN ->
	// registry.token, etc. GOBOTTLE_REPO is a ko-style shorthand for the
	// registry root path.
	viper.SetEnvPrefix("GOBOTTLE")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	_ = viper.BindEnv("registry.root_path", "GOBOTTLE_REPO", "GOBOTTLE_REGISTRY_ROOT_PATH")
	// GOBOTTLE_TAP_TOKEN optionally dedicates a token to tap commits; when
	// unset (or empty) the tap falls back to the standard token
	// (GITHUB_TOKEN et al.) via the defaults below and the config cascade.
	_ = viper.BindEnv("tap.token", "GOBOTTLE_TAP_TOKEN")

	// Also check for GITHUB_TOKEN
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		viper.SetDefault("registry.token", token)
		viper.SetDefault("tap.token", token)
	}

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		if opts.LogLevel == "debug" {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
	}

	return nil
}

// GetCacheDir returns the cache directory for gobottle
func GetCacheDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user cache directory: %w", err)
	}

	gobottleCache := filepath.Join(cacheDir, "gobottle")
	if err := os.MkdirAll(gobottleCache, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	return gobottleCache, nil
}
