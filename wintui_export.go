package main

import (
	"fmt"
	"runtime"
	"time"
)

// exportEnvelopeVersion is the only version writers emit today. The reader
// rejects anything else with a clear error so a downgrade can't silently
// drop fields it doesn't recognize.
const exportEnvelopeVersion = 1

// exportEnvelope is the v2.7.0 wire format for `wintui export`. Older
// flat-array exports (legacy `[]importPkg`) are still readable; only the new
// writer emits envelopes. The flat-array format is preserved for backward
// compat with any files users hand-rolled before this command shipped.
type exportEnvelope struct {
	Version   int         `json:"version"`
	Generator string      `json:"generator"`
	Created   time.Time   `json:"created"`
	Host      exportHost  `json:"host"`
	Packages  []exportPkg `json:"packages"`
}

// exportHost captures the source machine so a future "diff against my old
// PC" feature can attribute entries. None of these are required for import
// and any future field can be optional.
type exportHost struct {
	Arch string `json:"arch,omitempty"` // GOARCH at export time
	OS   string `json:"os,omitempty"`   // GOOS at export time
}

// exportPkg is the per-package shape — same field set as importPkg minus the
// runtime annotations (Installed / NonCanonical / Collisions). Versions are
// optional; the `wintui export` command omits them by default per the locked
// design (restoring exact versions is a footgun on a fresh machine).
type exportPkg struct {
	Name    string `json:"name"`
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

// buildExportEnvelope normalizes installed packages into an envelope. With
// withVersions=false (the default), Version fields are dropped — the
// resulting file installs whatever winget considers latest at import time.
func buildExportEnvelope(installed []Package, withVersions bool, now time.Time) exportEnvelope {
	pkgs := make([]exportPkg, 0, len(installed))
	for _, p := range installed {
		ep := exportPkg{
			Name:   p.Name,
			ID:     p.ID,
			Source: p.Source,
		}
		if withVersions {
			ep.Version = p.Version
		}
		pkgs = append(pkgs, ep)
	}
	return exportEnvelope{
		Version:   exportEnvelopeVersion,
		Generator: fmt.Sprintf("wintui %s", version),
		Created:   now.UTC(),
		Host: exportHost{
			Arch: runtime.GOARCH,
			OS:   runtime.GOOS,
		},
		Packages: pkgs,
	}
}
