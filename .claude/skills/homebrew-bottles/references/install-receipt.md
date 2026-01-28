# INSTALL_RECEIPT.json Reference

Complete schema documentation for the Homebrew Tab (installation receipt) file.

## Overview

`INSTALL_RECEIPT.json` (also called "Tab") stores metadata about how a formula was installed. It lives in `.brew/INSTALL_RECEIPT.json` inside the bottle tarball.

## Complete Schema

```json
{
  "homebrew_version": "4.4.0",
  "used_options": [],
  "unused_options": [],
  "built_as_bottle": true,
  "poured_from_bottle": false,
  "loaded_from_api": false,
  "installed_as_dependency": false,
  "installed_on_request": true,
  "changed_files": ["INSTALL_RECEIPT.json"],
  "time": 1706000000,
  "source_modified_time": 1706000000,
  "stdlib": null,
  "compiler": "clang",
  "aliases": [],
  "runtime_dependencies": [],
  "source": {
    "tap": "homebrew/core",
    "tap_git_head": "abc123def456789...",
    "spec": "stable",
    "path": "/opt/homebrew/Library/Taps/homebrew/homebrew-core/Formula/g/gobottle.rb",
    "versions": {
      "stable": "1.0.0",
      "head": null,
      "version_scheme": 0
    }
  },
  "arch": "arm64",
  "built_on": {
    "os": "Macintosh",
    "os_version": "macOS 14.0",
    "cpu_family": "apple_m1",
    "xcode": "15.0",
    "clt": "15.0"
  }
}
```

## Field Reference

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `homebrew_version` | string | Yes | Homebrew version used to build |
| `used_options` | array | Yes | Build options that were used (usually empty) |
| `unused_options` | array | Yes | Available but unused options (usually empty) |
| `built_as_bottle` | boolean | Yes | **Must be `true`** for bottles |
| `poured_from_bottle` | boolean | Yes | **Must be `false`** for newly created bottles |
| `loaded_from_api` | boolean | No | Whether formula loaded from API |
| `installed_as_dependency` | boolean | No | Whether installed as dependency |
| `installed_on_request` | boolean | No | Whether explicitly requested |
| `changed_files` | array | No | Files changed after installation |
| `time` | integer | Yes | Unix timestamp of build time |
| `source_modified_time` | integer | No | Unix timestamp of source modification |
| `stdlib` | string/null | No | C++ stdlib used (null for Go) |
| `compiler` | string | Yes | Compiler used |
| `aliases` | array | No | Formula aliases |
| `runtime_dependencies` | array | Yes | Runtime dependency list |
| `source` | object | Yes | Source information |
| `arch` | string | Yes | CPU architecture |
| `built_on` | object | No | Build environment info |

### Critical Fields for Bottles

These fields MUST be set correctly:

```json
{
  "built_as_bottle": true,      // MUST be true
  "poured_from_bottle": false,  // MUST be false for new bottles
  "time": 1706000000,           // Build timestamp (Unix epoch)
  "arch": "arm64"               // Must match platform tag
}
```

### The `source` Object

```json
{
  "source": {
    "tap": "user/homebrew-tap",
    "tap_git_head": "abc123...",
    "spec": "stable",
    "path": "/path/to/formula.rb",
    "versions": {
      "stable": "1.0.0",
      "head": null,
      "version_scheme": 0
    }
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `tap` | string | Tap name (e.g., "homebrew/core", "user/tap") |
| `tap_git_head` | string | Git commit SHA of tap (optional) |
| `spec` | string | Formula spec: "stable" or "head" |
| `path` | string | Path to formula file (optional) |
| `versions.stable` | string | Stable version number |
| `versions.head` | string/null | Head version (usually null) |
| `versions.version_scheme` | integer | Version scheme (usually 0) |

### The `runtime_dependencies` Array

For bottles with runtime dependencies:

```json
{
  "runtime_dependencies": [
    {
      "full_name": "openssl@3",
      "version": "3.1.0",
      "revision": 0,
      "pkg_version": "3.1.0",
      "declared_directly": true
    },
    {
      "full_name": "readline",
      "version": "8.2",
      "revision": 0,
      "pkg_version": "8.2",
      "declared_directly": false
    }
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `full_name` | string | Dependency formula name |
| `version` | string | Dependency version |
| `revision` | integer | Formula revision (usually 0) |
| `pkg_version` | string | Package version string |
| `declared_directly` | boolean | Whether declared in formula `depends_on` |

**For Go binaries**: Usually `[]` (empty array) since static binaries have no runtime dependencies.

### The `built_on` Object

Build environment information (optional but recommended):

```json
{
  "built_on": {
    "os": "Macintosh",
    "os_version": "macOS 14.0",
    "cpu_family": "apple_m1",
    "xcode": "15.0",
    "clt": "15.0"
  }
}
```

| Field | Type | Platform | Description |
|-------|------|----------|-------------|
| `os` | string | All | OS name ("Macintosh", "Linux") |
| `os_version` | string | All | OS version string |
| `cpu_family` | string | All | CPU family identifier |
| `xcode` | string | macOS | Xcode version |
| `clt` | string | macOS | Command Line Tools version |

### The `compiler` Field

| Value | When to Use |
|-------|-------------|
| `"clang"` | macOS builds using system Clang |
| `"gcc"` | Linux builds using GCC |
| `"go"` | Pure Go builds (recommended for gobottle) |
| `"gcc-11"` | Specific GCC version |

For gobottle, use `"go"` to indicate Go compiler.

### The `arch` Field

| Value | Platform |
|-------|----------|
| `"arm64"` | Apple Silicon, Linux ARM64 |
| `"x86_64"` | Intel Mac, Linux x86_64 |

## Minimal Example for Go Binary

The absolute minimum for a Go binary bottle:

```json
{
  "homebrew_version": "4.4.0",
  "used_options": [],
  "unused_options": [],
  "built_as_bottle": true,
  "poured_from_bottle": false,
  "time": 1706000000,
  "compiler": "go",
  "runtime_dependencies": [],
  "source": {
    "tap": "user/homebrew-tap",
    "spec": "stable",
    "versions": {
      "stable": "1.0.0",
      "head": null,
      "version_scheme": 0
    }
  },
  "arch": "arm64"
}
```

## Complete Example for Go Binary

A fully-populated receipt for a Go binary:

```json
{
  "homebrew_version": "4.4.0",
  "used_options": [],
  "unused_options": [],
  "built_as_bottle": true,
  "poured_from_bottle": false,
  "loaded_from_api": false,
  "installed_as_dependency": false,
  "installed_on_request": true,
  "changed_files": [
    "INSTALL_RECEIPT.json"
  ],
  "time": 1706000000,
  "source_modified_time": 1706000000,
  "stdlib": null,
  "compiler": "go",
  "aliases": [],
  "runtime_dependencies": [],
  "source": {
    "tap": "user/homebrew-tap",
    "tap_git_head": "a1b2c3d4e5f6g7h8i9j0",
    "spec": "stable",
    "path": "/opt/homebrew/Library/Taps/user/homebrew-tap/Formula/gobottle.rb",
    "versions": {
      "stable": "1.0.0",
      "head": null,
      "version_scheme": 0
    }
  },
  "arch": "arm64",
  "built_on": {
    "os": "Macintosh",
    "os_version": "macOS 14.0",
    "cpu_family": "apple_m1",
    "xcode": "15.0",
    "clt": "15.0"
  }
}
```

## Go Implementation

```go
package bottle

import (
    "encoding/json"
    "os"
    "runtime"
    "time"
)

type Tab struct {
    HomebrewVersion       string              `json:"homebrew_version"`
    UsedOptions           []string            `json:"used_options"`
    UnusedOptions         []string            `json:"unused_options"`
    BuiltAsBottle         bool                `json:"built_as_bottle"`
    PouredFromBottle      bool                `json:"poured_from_bottle"`
    LoadedFromAPI         bool                `json:"loaded_from_api,omitempty"`
    InstalledAsDependency bool                `json:"installed_as_dependency,omitempty"`
    InstalledOnRequest    bool                `json:"installed_on_request,omitempty"`
    ChangedFiles          []string            `json:"changed_files,omitempty"`
    Time                  int64               `json:"time"`
    SourceModifiedTime    int64               `json:"source_modified_time,omitempty"`
    Stdlib                *string             `json:"stdlib"`
    Compiler              string              `json:"compiler"`
    Aliases               []string            `json:"aliases,omitempty"`
    RuntimeDependencies   []RuntimeDependency `json:"runtime_dependencies"`
    Source                Source              `json:"source"`
    Arch                  string              `json:"arch"`
    BuiltOn               *BuiltOn            `json:"built_on,omitempty"`
}

type RuntimeDependency struct {
    FullName         string `json:"full_name"`
    Version          string `json:"version"`
    Revision         int    `json:"revision"`
    PkgVersion       string `json:"pkg_version"`
    DeclaredDirectly bool   `json:"declared_directly"`
}

type Source struct {
    Tap        string   `json:"tap"`
    TapGitHead string   `json:"tap_git_head,omitempty"`
    Spec       string   `json:"spec"`
    Path       string   `json:"path,omitempty"`
    Versions   Versions `json:"versions"`
}

type Versions struct {
    Stable        string  `json:"stable"`
    Head          *string `json:"head"`
    VersionScheme int     `json:"version_scheme"`
}

type BuiltOn struct {
    OS        string `json:"os"`
    OSVersion string `json:"os_version"`
    CPUFamily string `json:"cpu_family"`
    Xcode     string `json:"xcode,omitempty"`
    CLT       string `json:"clt,omitempty"`
}

// NewTab creates a minimal Tab for a Go binary bottle
func NewTab(formula, version, tap string) *Tab {
    now := time.Now().Unix()

    // Use SOURCE_DATE_EPOCH if set for reproducibility
    if epoch := os.Getenv("SOURCE_DATE_EPOCH"); epoch != "" {
        if ts, err := strconv.ParseInt(epoch, 10, 64); err == nil {
            now = ts
        }
    }

    return &Tab{
        HomebrewVersion:     "4.4.0",
        UsedOptions:         []string{},
        UnusedOptions:       []string{},
        BuiltAsBottle:       true,
        PouredFromBottle:    false,
        Time:                now,
        SourceModifiedTime:  now,
        Stdlib:              nil,
        Compiler:            "go",
        RuntimeDependencies: []RuntimeDependency{},
        Source: Source{
            Tap:  tap,
            Spec: "stable",
            Versions: Versions{
                Stable:        version,
                Head:          nil,
                VersionScheme: 0,
            },
        },
        Arch: runtime.GOARCH,
    }
}

// JSON returns the Tab as formatted JSON
func (t *Tab) JSON() ([]byte, error) {
    return json.MarshalIndent(t, "", "  ")
}
```

## Validation

### Required Fields Check

```go
func (t *Tab) Validate() error {
    if t.HomebrewVersion == "" {
        return errors.New("homebrew_version is required")
    }
    if !t.BuiltAsBottle {
        return errors.New("built_as_bottle must be true for bottles")
    }
    if t.PouredFromBottle {
        return errors.New("poured_from_bottle must be false for new bottles")
    }
    if t.Time == 0 {
        return errors.New("time is required")
    }
    if t.Compiler == "" {
        return errors.New("compiler is required")
    }
    if t.Source.Tap == "" {
        return errors.New("source.tap is required")
    }
    if t.Source.Versions.Stable == "" {
        return errors.New("source.versions.stable is required")
    }
    if t.Arch == "" {
        return errors.New("arch is required")
    }
    return nil
}
```

### JSON Schema (for external validation)

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": [
    "homebrew_version",
    "used_options",
    "unused_options",
    "built_as_bottle",
    "poured_from_bottle",
    "time",
    "compiler",
    "runtime_dependencies",
    "source",
    "arch"
  ],
  "properties": {
    "homebrew_version": { "type": "string" },
    "built_as_bottle": { "const": true },
    "poured_from_bottle": { "const": false },
    "time": { "type": "integer", "minimum": 0 },
    "arch": { "enum": ["arm64", "x86_64"] },
    "source": {
      "type": "object",
      "required": ["tap", "spec", "versions"],
      "properties": {
        "versions": {
          "type": "object",
          "required": ["stable", "version_scheme"]
        }
      }
    }
  }
}
```
