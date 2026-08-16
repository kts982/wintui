# WinTUI v2.11.1 Release Notes

**Clipboard paste works now.** Pasting into the `/` filter and the `s` search
overlay — Ctrl+V or right-click in Windows Terminal — never worked in any
prior WinTUI version, and this patch fixes it. Also ships a security fix for
a render-path dependency (GO-2026-5970), the Go 1.26.6 security toolchain,
the first dependabot-mediated dependency refreshes, and the first release
with **build-provenance attestations** on every artifact.

## Fixes

- **Paste into text inputs.** The terminal delivers a paste as a bracketed
  paste event, which bubbletea v2 surfaces as its own message type rather
  than a stream of key presses. WinTUI only ever routed key presses to its
  two text inputs, so pastes were silently discarded — the input widgets
  themselves supported pasting all along. Paste is now routed to whichever
  input owns the foreground (search overlay, then the filter), with the same
  precedence as typing: overlays and modals without text inputs swallow the
  paste instead of leaking it to a background input. Multi-line clipboard
  content is sanitized by the input widget, so pasting a stray newline can't
  smuggle in a "press Enter".

## Security

- **golang.org/x/text bumped to v0.40.0**, fixing
  [GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970) (infinite loop on
  invalid input in Unicode normalization). govulncheck flagged the vulnerable
  code as reachable through WinTUI's text-rendering path, which draws strings
  sourced from the public winget catalog — a defense-in-depth fix; no known
  exploitation. The tree scans clean after the bump.
- **Built with Go 1.26.6** (2026-08-13 security release). None of its CVEs
  are reachable in WinTUI per govulncheck — the toolchain rides along as
  hygiene.

## Dependencies

- **charm.land/bubbles v2.1.1** (patch: internal dependency refresh),
  **golang.org/x/sys v0.47.0** (routine), and
  **mattn/go-runewidth v0.0.27** — width measurement now matches how modern
  terminals actually render multi-rune graphemes (ZWJ/flag emoji count 2
  cells, spacing combining marks get their own cell), improving column
  alignment for non-Latin package names. All proposed by the repo's monthly
  dependabot config and merged after full CI validation
  (golang.org/x/sync v0.22.0 rides along).
- Zero new Win32 / COM / syscall surface: the `go tool nm`
  `syscall.`/`windows.` symbol set is **identical** to v2.11.0 — fourth
  consecutive release with an unchanged static surface.

## Supply-chain hardening (repo-side)

Since v2.11.0 the repository gained CodeQL analysis (clean first scan),
an OpenSSF Scorecard workflow with published results, a lint gate
(golangci-lint, zero issues), a `SECURITY.md` with private vulnerability
reporting, SHA-pinned workflow actions, and monthly dependabot. None of this
changes the shipped binary — but starting with this release, artifacts carry
**GitHub build-provenance attestations**:

- a `wintui_provenance.intoto.jsonl` bundle is attached to the release, and
- any artifact can be verified with
  `gh attestation verify <file> --repo kts982/wintui`.

## Compatibility

- No breaking changes; `settings.json`, `cache.json`, and `history.json` are
  untouched. No new features, no keybinding changes.

## Verification

VirusTotal scans of the published artifacts for v2.11.1 (run 2026-08-16):

| Asset | SHA256 | Detections | Report |
|---|---|---|---|
| `wintui_2.11.1_windows_amd64.exe` (7.6 MB) | `cef405a07fef…` | 3/71 | [VT report](https://www.virustotal.com/gui/file/cef405a07fefc0aed0abdc8b08c638cb2f45cf48ad03d6834f627c241b7f79ef) |
| `wintui_2.11.1_windows_amd64.zip` (2.8 MB) | `beccfa86d49e…` | 1/68 | [VT report](https://www.virustotal.com/gui/file/beccfa86d49ea8ff7a5746b3e50cbccd25cec1678c63443fae2cf46f02e0632e) |
| `wintui_2.11.1_windows_arm64.exe` (7 MB) | `6f510b244de8…` | 1/69 | [VT report](https://www.virustotal.com/gui/file/6f510b244de8110e1cd0012c59484cab2955a6e169e9a81ca8bc960525f95c17) |
| `wintui_2.11.1_windows_arm64.zip` (2.5 MB) | `eb213ce8578d…` | 0/67 | [VT report](https://www.virustotal.com/gui/file/eb213ce8578db19d7733d3cee9763c75257d5430ee0515134bace8f56760f243) |

VirusTotal's Microsoft engine flagged both `.exe` artifacts (`Wacatac.B!ml`) at
scan time. Cross-check per the established playbook: **live Windows Defender
(`MpCmdRun -Scan -ScanType 3`, signatures 1.457.187.0) scanned the identical
published bytes and found no threats on either architecture** — the VT hit is a
known configuration artifact of VT's non-production engine settings (same
pattern as v2.9.1, which passed winget-pkgs validation against real Defender).

Build provenance: all four artifacts are attested;
`gh attestation verify <file> --repo kts982/wintui` succeeds for both `.exe`
files and both `.zip` archives.

Full SHA256 hashes:

- `wintui_2.11.1_windows_amd64.exe`: `cef405a07fefc0aed0abdc8b08c638cb2f45cf48ad03d6834f627c241b7f79ef`
- `wintui_2.11.1_windows_amd64.zip`: `beccfa86d49ea8ff7a5746b3e50cbccd25cec1678c63443fae2cf46f02e0632e`
- `wintui_2.11.1_windows_arm64.exe`: `6f510b244de8110e1cd0012c59484cab2955a6e169e9a81ca8bc960525f95c17`
- `wintui_2.11.1_windows_arm64.zip`: `eb213ce8578db19d7733d3cee9763c75257d5430ee0515134bace8f56760f243`

