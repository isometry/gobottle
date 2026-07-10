package config

import (
	"os"
	"testing"

	"github.com/isometry/gobottle/internal/git"
)

func TestSetDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    *Config
		check    func(*Config) bool
		expected string
	}{
		{
			name:  "empty config gets local source type",
			input: &Config{},
			check: func(c *Config) bool {
				return c.Source.Type == "local"
			},
			expected: "Source.Type should be 'local'",
		},
		{
			name:  "empty config gets dist path",
			input: &Config{},
			check: func(c *Config) bool {
				return c.Source.DistPath == "dist"
			},
			expected: "Source.DistPath should be 'dist'",
		},
		{
			name:  "empty config gets default cellar",
			input: &Config{},
			check: func(c *Config) bool {
				return c.Bottle.Cellar == ":any_skip_relocation"
			},
			expected: "Bottle.Cellar should be ':any_skip_relocation'",
		},
		{
			name:  "empty config gets ghcr.io registry",
			input: &Config{},
			check: func(c *Config) bool {
				return c.Registry.Host == "ghcr.io"
			},
			expected: "Registry.Host should be 'ghcr.io'",
		},
		{
			name:  "empty config gets homebrew-tap",
			input: &Config{},
			check: func(c *Config) bool {
				return c.Tap.Repo == "homebrew-tap"
			},
			expected: "Tap.Repo should be 'homebrew-tap'",
		},
		{
			name:  "empty config gets main branch",
			input: &Config{},
			check: func(c *Config) bool {
				return c.Tap.Branch == "main"
			},
			expected: "Tap.Branch should be 'main'",
		},
		{
			name:  "root path derived from owner and tap repo",
			input: &Config{Formula: FormulaConfig{Name: "myformula"}, Registry: RegistryConfig{Owner: "myorg"}},
			check: func(c *Config) bool {
				return c.Registry.RootPath == "myorg/tap"
			},
			expected: "Registry.RootPath should derive from owner + tap repo minus homebrew- prefix",
		},
		{
			name:  "binary defaults to formula name",
			input: &Config{Formula: FormulaConfig{Name: "myformula"}},
			check: func(c *Config) bool {
				return len(c.Binaries) == 1 && c.Binaries[0].Name == "myformula"
			},
			expected: "Binaries should default to formula name",
		},
		{
			name: "tap token defaults to registry token",
			input: &Config{
				Registry: RegistryConfig{Token: "secret-token"},
			},
			check: func(c *Config) bool {
				return c.Tap.Token == "secret-token"
			},
			expected: "Tap.Token should default to Registry.Token",
		},
		{
			name: "existing values are preserved",
			input: &Config{
				Source: SourceConfig{
					Type:     "github",
					DistPath: "output",
				},
				Bottle: BottleConfig{
					Cellar: ":any",
				},
				Registry: RegistryConfig{
					Host: "custom.registry.io",
				},
			},
			check: func(c *Config) bool {
				return c.Source.Type == "github" &&
					c.Source.DistPath == "output" &&
					c.Bottle.Cellar == ":any" &&
					c.Registry.Host == "custom.registry.io"
			},
			expected: "Existing values should be preserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.SetDefaults()
			if !tt.check(tt.input) {
				t.Error(tt.expected)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	// Create a temp directory for dist path validation
	tmpDir, err := os.MkdirTemp("", "gobottle-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tests := []struct {
		name        string
		config      *Config
		setupEnv    map[string]string
		expectError bool
		errorField  string
	}{
		{
			name: "valid local config",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Version: "1.0.0",
				Source: SourceConfig{
					Type:     "local",
					DistPath: tmpDir,
				},
				Registry: RegistryConfig{
					Owner: "myorg",
					Token: "token",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			expectError: false,
		},
		{
			name: "valid github config",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Source: SourceConfig{
					Type:  "github",
					Owner: "myorg",
					Repo:  "myrepo",
					Tag:   "v1.0.0",
				},
				Registry: RegistryConfig{
					Owner: "myorg",
					Token: "token",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			expectError: false,
		},
		{
			name: "missing formula name",
			config: &Config{
				Version: "1.0.0",
				Source: SourceConfig{
					Type:     "local",
					DistPath: tmpDir,
				},
				Registry: RegistryConfig{
					Owner: "myorg",
					Token: "token",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			expectError: true,
			errorField:  "formula.name",
		},
		{
			name: "missing owner for github source",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Source: SourceConfig{
					Type: "github",
					Repo: "myrepo",
					Tag:  "v1.0.0",
				},
				Registry: RegistryConfig{
					Owner: "myorg",
					Token: "token",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			expectError: true,
			errorField:  "source.owner",
		},
		{
			name: "missing tag for github source",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Source: SourceConfig{
					Type:  "github",
					Owner: "myorg",
					Repo:  "myrepo",
				},
				Registry: RegistryConfig{
					Owner: "myorg",
					Token: "token",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			expectError: true,
			errorField:  "source.tag",
		},
		{
			name: "missing version for local source",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Source: SourceConfig{
					Type:     "local",
					DistPath: tmpDir,
				},
				Registry: RegistryConfig{
					Owner: "myorg",
					Token: "token",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			expectError: true,
			errorField:  "version",
		},
		{
			name: "invalid source type",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Version: "1.0.0",
				Source: SourceConfig{
					Type: "invalid",
				},
				Registry: RegistryConfig{
					Owner: "myorg",
					Token: "token",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			expectError: true,
			errorField:  "source.type",
		},
		{
			name: "missing registry owner",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Version: "1.0.0",
				Source: SourceConfig{
					Type:     "local",
					DistPath: tmpDir,
				},
				Registry: RegistryConfig{
					Token: "token",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			expectError: true,
			errorField:  "registry.root_path",
		},
		{
			name: "missing token",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Version: "1.0.0",
				Source: SourceConfig{
					Type:     "local",
					DistPath: tmpDir,
				},
				Registry: RegistryConfig{
					Owner: "myorg",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			expectError: true,
			errorField:  "registry.token",
		},
		{
			name: "token from GITHUB_TOKEN env",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Version: "1.0.0",
				Source: SourceConfig{
					Type:     "local",
					DistPath: tmpDir,
				},
				Registry: RegistryConfig{
					Owner: "myorg",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			setupEnv:    map[string]string{"GITHUB_TOKEN": "env-token"},
			expectError: false,
		},
		{
			name: "token from GH_TOKEN env",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Version: "1.0.0",
				Source: SourceConfig{
					Type:     "local",
					DistPath: tmpDir,
				},
				Registry: RegistryConfig{
					Owner: "myorg",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			setupEnv:    map[string]string{"GH_TOKEN": "gh-token"},
			expectError: false,
		},
		{
			name: "dist path does not exist",
			config: &Config{
				Formula: FormulaConfig{Name: "myformula"},
				Version: "1.0.0",
				Source: SourceConfig{
					Type:     "local",
					DistPath: "/nonexistent/path",
				},
				Registry: RegistryConfig{
					Owner: "myorg",
					Token: "token",
				},
				Tap: TapConfig{
					Owner: "myorg",
				},
				Binaries: []BinaryConfig{{Name: "mybin"}},
			},
			expectError: true,
			errorField:  "source.dist_path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear relevant env vars
			_ = os.Unsetenv("GITHUB_TOKEN")
			_ = os.Unsetenv("GH_TOKEN")

			// Set up test env vars
			for k, v := range tt.setupEnv {
				_ = os.Setenv(k, v)
				defer func(key string) { _ = os.Unsetenv(key) }(k)
			}

			// Validate() runs after SetDefaults() in real usage (root path
			// derivation happens there).
			tt.config.SetDefaults()
			err := tt.config.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errorField != "" {
					errStr := err.Error()
					if me, ok := err.(*MultiError); ok {
						found := false
						for _, e := range me.Errors {
							if ve, ok := e.(*ValidationError); ok {
								if ve.Field == tt.errorField {
									found = true
									break
								}
							}
						}
						if !found {
							t.Errorf("expected error for field %q, got: %s", tt.errorField, errStr)
						}
					} else if ve, ok := err.(*ValidationError); ok {
						if ve.Field != tt.errorField {
							t.Errorf("expected error for field %q, got: %s", tt.errorField, ve.Field)
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestBinaryNames(t *testing.T) {
	cfg := &Config{
		Binaries: []BinaryConfig{
			{Name: "bin1"},
			{Name: "bin2"},
			{Name: "bin3"},
		},
	}

	names := cfg.BinaryNames()

	if len(names) != 3 {
		t.Errorf("expected 3 names, got %d", len(names))
	}

	expected := []string{"bin1", "bin2", "bin3"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("expected %q at index %d, got %q", expected[i], i, name)
		}
	}
}

func TestParseGitURL(t *testing.T) {
	tests := []struct {
		url           string
		expectedOwner string
		expectedRepo  string
	}{
		{
			url:           "git@github.com:myorg/myrepo.git",
			expectedOwner: "myorg",
			expectedRepo:  "myrepo",
		},
		{
			url:           "https://github.com/myorg/myrepo.git",
			expectedOwner: "myorg",
			expectedRepo:  "myrepo",
		},
		{
			url:           "https://github.com/myorg/myrepo",
			expectedOwner: "myorg",
			expectedRepo:  "myrepo",
		},
		{
			url:           "http://github.com/myorg/myrepo.git",
			expectedOwner: "myorg",
			expectedRepo:  "myrepo",
		},
		{
			url:           "git@gitlab.com:group/project.git",
			expectedOwner: "group",
			expectedRepo:  "project",
		},
		{
			url:           "invalid-url",
			expectedOwner: "",
			expectedRepo:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			owner, repo := git.ParseRemoteURL(tt.url)
			if owner != tt.expectedOwner {
				t.Errorf("expected owner %q, got %q", tt.expectedOwner, owner)
			}
			if repo != tt.expectedRepo {
				t.Errorf("expected repo %q, got %q", tt.expectedRepo, repo)
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	t.Run("with value", func(t *testing.T) {
		err := &ValidationError{
			Field:   "test.field",
			Value:   "bad-value",
			Message: "is invalid",
		}
		expected := "validation error: test.field: is invalid (got: bad-value)"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("without value", func(t *testing.T) {
		err := &ValidationError{
			Field:   "test.field",
			Message: "is required",
		}
		expected := "validation error: test.field: is required"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})
}

func TestMultiError(t *testing.T) {
	t.Run("single error", func(t *testing.T) {
		err := &MultiError{
			Errors: []error{
				&ValidationError{Field: "field1", Message: "error1"},
			},
		}
		expected := "validation error: field1: error1"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("multiple errors", func(t *testing.T) {
		err := &MultiError{
			Errors: []error{
				&ValidationError{Field: "field1", Message: "error1"},
				&ValidationError{Field: "field2", Message: "error2"},
			},
		}
		result := err.Error()
		if result[:2] != "2 " {
			t.Errorf("expected to start with '2 ', got %q", result[:10])
		}
	})
}
