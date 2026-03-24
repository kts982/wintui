package main

import (
	"sync"
	"time"
)

// packageCache holds cached package lists to avoid re-fetching on tab switches.
type packageCache struct {
	mu          sync.Mutex
	installed   []Package
	installedAt time.Time
	upgradeable []Package
	upgradeAt   time.Time
	ttl         time.Duration
}

var cache = &packageCache{ttl: 2 * time.Minute}

func (c *packageCache) getInstalled() ([]Package, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.installed != nil && time.Since(c.installedAt) < c.ttl {
		// Return a copy
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
}

// invalidate clears all caches (call after install/upgrade/uninstall).
func (c *packageCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.installed = nil
	c.upgradeable = nil
}
