package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// packageCache holds cached package lists with in-memory and disk persistence.
type packageCache struct {
	mu          sync.Mutex
	installed   []Package
	installedAt time.Time
	upgradeable []Package
	upgradeAt   time.Time
	ttl         time.Duration
	diskTTL     time.Duration
}

var cache = &packageCache{
	ttl:     2 * time.Minute,
	diskTTL: 24 * time.Hour,
}

// diskCacheData is the on-disk JSON format.
type diskCacheData struct {
	Installed   []Package `json:"installed"`
	Upgradeable []Package `json:"upgradeable"`
	SavedAt     time.Time `json:"saved_at"`
}

func (c *packageCache) getInstalled() ([]Package, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.installed != nil && time.Since(c.installedAt) < c.ttl {
		cp := make([]Package, len(c.installed))
		copy(cp, c.installed)
		return cp, true
	}
	return nil, false
}

func (c *packageCache) setInstalled(pkgs []Package) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.installed = make([]Package, len(pkgs))
	copy(c.installed, pkgs)
	c.installedAt = time.Now()
	c.saveToDiskLocked()
}

func (c *packageCache) getUpgradeable() ([]Package, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.upgradeable != nil && time.Since(c.upgradeAt) < c.ttl {
		cp := make([]Package, len(c.upgradeable))
		copy(cp, c.upgradeable)
		return cp, true
	}
	return nil, false
}

func (c *packageCache) setUpgradeable(pkgs []Package) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.upgradeable = make([]Package, len(pkgs))
	copy(c.upgradeable, pkgs)
	c.upgradeAt = time.Now()
	c.saveToDiskLocked()
}

func (c *packageCache) getInstalledRaw() []Package {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.installed == nil {
		return nil
	}
	cp := make([]Package, len(c.installed))
	copy(cp, c.installed)
	return cp
}

func (c *packageCache) getUpgradeableRaw() []Package {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.upgradeable == nil {
		return nil
	}
	cp := make([]Package, len(c.upgradeable))
	copy(cp, c.upgradeable)
	return cp
}

func (c *packageCache) prime(installed, upgradeable []Package, savedAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.installed = make([]Package, len(installed))
	copy(c.installed, installed)
	c.upgradeable = make([]Package, len(upgradeable))
	copy(c.upgradeable, upgradeable)
	c.installedAt = savedAt
	c.upgradeAt = savedAt
}

// invalidate clears the in-memory cache (call after manual refresh).
func (c *packageCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.installed = nil
	c.upgradeable = nil
}

// ── Disk persistence ─────────────────────────────────────────────

func diskCachePath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	dir := filepath.Join(configDir, "wintui")
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "cache.json")
}

// saveToDiskLocked writes both lists via temp-file + rename for atomicity.
// Must be called under c.mu. Only writes when both lists are populated.
func (c *packageCache) saveToDiskLocked() {
	if c.installed == nil || c.upgradeable == nil {
		return
	}
	data := diskCacheData{
		Installed:   c.installed,
		Upgradeable: c.upgradeable,
		SavedAt:     time.Now(),
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	target := diskCachePath()
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, target)
}

// loadFromDisk reads the disk cache. Returns ok=false if missing, corrupt, or expired.
func (c *packageCache) loadFromDisk() (installed []Package, upgradeable []Package, savedAt time.Time, ok bool) {
	b, err := os.ReadFile(diskCachePath())
	if err != nil {
		return nil, nil, time.Time{}, false
	}
	var data diskCacheData
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, nil, time.Time{}, false
	}
	if time.Since(data.SavedAt) > c.diskTTL {
		return nil, nil, time.Time{}, false
	}
	if data.Installed == nil || data.Upgradeable == nil {
		return nil, nil, time.Time{}, false
	}
	return data.Installed, data.Upgradeable, data.SavedAt, true
}

// deleteDiskCache removes the cache file (for manual refresh).
func (c *packageCache) deleteDiskCache() {
	_ = os.Remove(diskCachePath())
}
