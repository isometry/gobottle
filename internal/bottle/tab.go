package bottle

import (
	"encoding/json"
	"os"
	"strconv"
	"time"
)

// Tab represents the INSTALL_RECEIPT.json structure used by Homebrew
// to track bottle metadata and installation information.
type Tab struct {
	HomebrewVersion       string              `json:"homebrew_version"`
	UsedOptions           []string            `json:"used_options"`
	UnusedOptions         []string            `json:"unused_options"`
	BuiltAsBottle         bool                `json:"built_as_bottle"`
	PouredFromBottle      bool                `json:"poured_from_bottle"`
	InstalledAsDependency bool                `json:"installed_as_dependency"`
	InstalledOnRequest    bool                `json:"installed_on_request"`
	ChangedFiles          []string            `json:"changed_files"`
	Time                  int64               `json:"time"`
	SourceModifiedTime    int64               `json:"source_modified_time"`
	Compiler              string              `json:"compiler"`
	Aliases               []string            `json:"aliases"`
	RuntimeDependencies   []RuntimeDependency `json:"runtime_dependencies"`
	Source                TabSource           `json:"source"`
	Arch                  string              `json:"arch"`
	BuiltOn               TabBuiltOn          `json:"built_on"`
}

// RuntimeDependency represents a runtime dependency in the tab
type RuntimeDependency struct {
	FullName         string `json:"full_name"`
	Version          string `json:"version"`
	Revision         int    `json:"revision,omitempty"`
	PkgVersion       string `json:"pkg_version"`
	DeclaredDirectly bool   `json:"declared_directly"`
}

// TabSource contains source information for the formula
type TabSource struct {
	Spec       string      `json:"spec"`
	Versions   TabVersions `json:"versions"`
	Path       string      `json:"path,omitempty"`
	Tap        string      `json:"tap,omitempty"`
	TapGitHead string      `json:"tap_git_head,omitempty"`
}

// TabVersions contains version information
type TabVersions struct {
	Stable        string `json:"stable"`
	Head          string `json:"head"`
	VersionScheme int    `json:"version_scheme"`
}

// TabBuiltOn contains build environment information
type TabBuiltOn struct {
	OS            string `json:"os"`
	OSVersion     string `json:"os_version"`
	CPUFamily     string `json:"cpu_family"`
	Xcode         string `json:"xcode,omitempty"`
	CLT           string `json:"clt,omitempty"`
	PreferredPerl string `json:"preferred_perl,omitempty"`
}

// TabOptions contains options for creating a new Tab
type TabOptions struct {
	// Formula name
	Formula string

	// Version string
	Version string

	// Architecture (arm64 or x86_64)
	Arch string

	// OS (darwin or linux)
	OS string

	// OS version (e.g., "15.0" for macOS Sequoia)
	OSVersion string

	// Tap name (e.g., "user/homebrew-tap")
	Tap string

	// Compiler used (default: "go" for Go-built bottles)
	Compiler string
}

// NewTab creates a new Tab with the given options
func NewTab(opts TabOptions) *Tab {
	// Set defaults
	if opts.Compiler == "" {
		opts.Compiler = "go"
	}

	// Get build time
	buildTime := time.Now().Unix()

	// Respect SOURCE_DATE_EPOCH for reproducibility
	if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
		if secs, err := strconv.ParseInt(epoch, 10, 64); err == nil {
			buildTime = secs
		}
	}

	// Convert Go arch to Homebrew arch format
	homebrewArch := opts.Arch
	if opts.Arch == "amd64" {
		homebrewArch = "x86_64"
	} else if opts.Arch == "arm64" {
		homebrewArch = "arm64"
	}

	// Build CPU family string
	cpuFamily := homebrewArch
	if opts.OS == "darwin" && opts.Arch == "arm64" {
		cpuFamily = "arm_firestorm_icestorm"
	} else if opts.Arch == "amd64" {
		cpuFamily = "dunno" // Homebrew often uses this for Intel
	}

	return &Tab{
		HomebrewVersion:       "4.4.0",
		UsedOptions:           []string{},
		UnusedOptions:         []string{},
		BuiltAsBottle:         true,
		PouredFromBottle:      false,
		InstalledAsDependency: false,
		InstalledOnRequest:    true,
		ChangedFiles:          []string{},
		Time:                  buildTime,
		SourceModifiedTime:    buildTime,
		Compiler:              opts.Compiler,
		Aliases:               []string{},
		RuntimeDependencies:   []RuntimeDependency{},
		Source: TabSource{
			Spec: "stable",
			Versions: TabVersions{
				Stable:        opts.Version,
				Head:          "",
				VersionScheme: 0,
			},
			Tap: opts.Tap,
		},
		Arch: homebrewArch,
		BuiltOn: TabBuiltOn{
			OS:        opts.OS,
			OSVersion: opts.OSVersion,
			CPUFamily: cpuFamily,
		},
	}
}

// Marshal returns the JSON representation of the Tab
func (t *Tab) Marshal() ([]byte, error) {
	return json.MarshalIndent(t, "", "  ")
}
