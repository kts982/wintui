## WinGet Manifest

This folder contains the local WinGet manifest set for the current GitHub release.

Layout mirrors the `winget-pkgs` repository so the files can be validated locally and
submitted with minimal reshaping.

### Validate

```powershell
winget validate .\packaging\winget\manifests\k\kts982\WinTUI\<version>
```

### Test with a local manifest

Local manifest installs must be enabled in WinGet settings before testing:

```powershell
winget settings --enable LocalManifestFiles
winget install --manifest .\packaging\winget\manifests\k\kts982\WinTUI\<version>
```

### Submit

1. Copy the manifest directory into a fork of `microsoft/winget-pkgs`.
2. Run `winget validate` against the copied package-version directory.
3. Open a PR to `microsoft/winget-pkgs`.
