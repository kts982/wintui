# Security Policy

## Supported versions

Only the latest release receives fixes. WinTUI has no LTS branches; upgrading
is a single `winget upgrade kts982.WinTUI` (or `wintui upgrade --self`).

## Reporting a vulnerability

Please use GitHub's **private vulnerability reporting**: [Security tab →
Report a vulnerability](https://github.com/kts982/wintui/security/advisories/new).
Do not open a public issue for anything you believe is exploitable.

WinTUI is maintained by one person; reports are handled on a best-effort
basis, normally acknowledged within a few days. Security-relevant areas worth
scrutiny: the elevated helper (`helper.go`, `elevation.go` — action allowlist,
named-pipe protocol), the self-update handoff (`self_update_windows.go`), and
the cleanup path-validation allowlist (`cleanup_targets.go`).

## Verifying release artifacts

- Every GitHub release ships a `checksums.txt` (SHA256 for all assets), and
  the release notes carry a **Verification** section with per-asset hashes
  and VirusTotal report links.
- Releases after v2.11.0 also carry GitHub build-provenance attestations;
  verify with `gh attestation verify <file> --repo kts982/wintui`.
- The winget manifests in `microsoft/winget-pkgs` pin the same SHA256s, so a
  `winget install` will refuse a tampered binary.

## A note on antivirus false positives

WinTUI binaries are currently unsigned, and fresh unsigned Go binaries are a
well-known false-positive magnet for reputation-based AV engines (each release
is a brand-new SHA256 with zero download history). Every release is scanned
with VirusTotal and live Microsoft Defender before submission to winget — see
the release notes. If your AV flags a WinTUI binary whose SHA256 matches
`checksums.txt`, it is almost certainly a reputation FP; please report it to
your AV vendor (for Defender: https://www.microsoft.com/wdsi/filesubmission)
and open a regular issue so it can be tracked.
