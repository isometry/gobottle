package config

import (
	"testing"
)

func TestSourceURL(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantURL string
	}{
		{
			name: "explicit URL",
			cfg: &Config{
				Source: SourceConfig{
					URL: "https://custom.url/archive.tar.gz",
				},
			},
			wantURL: "https://custom.url/archive.tar.gz",
		},
		{
			name: "derived from GitHub with tag",
			cfg: &Config{
				Source: SourceConfig{
					Owner: "isometry",
					Repo:  "gobottle",
					Tag:   "v0.0.1",
				},
			},
			wantURL: "https://github.com/isometry/gobottle/archive/refs/tags/v0.0.1.tar.gz",
		},
		{
			name: "derived from GitHub with version",
			cfg: &Config{
				Version: "1.2.3",
				Source: SourceConfig{
					Owner: "isometry",
					Repo:  "gobottle",
				},
			},
			wantURL: "https://github.com/isometry/gobottle/archive/refs/tags/v1.2.3.tar.gz",
		},
		{
			name: "missing owner returns empty",
			cfg: &Config{
				Source: SourceConfig{
					Repo: "gobottle",
					Tag:  "v0.0.1",
				},
			},
			wantURL: "",
		},
		{
			name: "missing repo returns empty",
			cfg: &Config{
				Source: SourceConfig{
					Owner: "isometry",
					Tag:   "v0.0.1",
				},
			},
			wantURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.SourceURL()
			if got != tt.wantURL {
				t.Errorf("SourceURL() = %q, want %q", got, tt.wantURL)
			}
		})
	}
}
