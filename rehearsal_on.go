//go:build rehearsal

package main

// rehearsalMode is true only in canary-rehearsal builds
// (`go build -tags rehearsal`), mirroring the ldflags override pattern used
// for selfPackageID. See rehearsal_off.go for the release-build default.
const rehearsalMode = true
