package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// CacheFileName is the name of the platform cache file
	CacheFileName = "platforms.json"

	// CacheTTL is how long cached platform info is considered valid
	CacheTTL = 24 * time.Hour
)

// CachedPlatformInfo wraps PlatformInfo with cache metadata
type CachedPlatformInfo struct {
	PlatformInfo
	CachedAt time.Time `json:"cached_at"`
}

// Cache manages platform information caching
type Cache struct {
	cacheDir  string
	cachePath string
}

// NewCache creates a new platform cache
func NewCache(cacheDir string) (*Cache, error) {
	if cacheDir == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user cache directory: %w", err)
		}
		cacheDir = filepath.Join(userCache, "gobottle")
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &Cache{
		cacheDir:  cacheDir,
		cachePath: filepath.Join(cacheDir, CacheFileName),
	}, nil
}

// Load loads platform info from cache if valid and not expired
func (c *Cache) Load() (*PlatformInfo, error) {
	cached, err := c.loadRaw()
	if err != nil {
		return nil, err
	}

	// Check if expired
	if time.Since(cached.CachedAt) > CacheTTL {
		return nil, fmt.Errorf("cache expired")
	}

	return &cached.PlatformInfo, nil
}

// LoadExpired loads platform info from cache even if expired
func (c *Cache) LoadExpired() (*PlatformInfo, error) {
	cached, err := c.loadRaw()
	if err != nil {
		return nil, err
	}

	return &cached.PlatformInfo, nil
}

// loadRaw loads the raw cached data
func (c *Cache) loadRaw() (*CachedPlatformInfo, error) {
	data, err := os.ReadFile(c.cachePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache: %w", err)
	}

	var cached CachedPlatformInfo
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("failed to parse cache: %w", err)
	}

	return &cached, nil
}

// Save saves platform info to cache
func (c *Cache) Save(info *PlatformInfo) error {
	cached := CachedPlatformInfo{
		PlatformInfo: *info,
		CachedAt:     time.Now(),
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(c.cachePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache: %w", err)
	}

	return nil
}

// Clear removes the cache file
func (c *Cache) Clear() error {
	if err := os.Remove(c.cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear cache: %w", err)
	}
	return nil
}

// Path returns the cache file path
func (c *Cache) Path() string {
	return c.cachePath
}

// Age returns how old the cache is, or -1 if no cache exists
func (c *Cache) Age() time.Duration {
	cached, err := c.loadRaw()
	if err != nil {
		return -1
	}
	return time.Since(cached.CachedAt)
}
