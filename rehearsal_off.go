//go:build !rehearsal

package main

// rehearsalMode gates capabilities that only canary-rehearsal builds may use
// (currently the self-update manifest override). Release builds compile with
// this false so the gated code paths are eliminated entirely — a shipped
// binary must never read an external file that re-targets its own update.
const rehearsalMode = false
