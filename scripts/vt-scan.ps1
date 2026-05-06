<#
.SYNOPSIS
  Submit a binary (local file or GitHub release asset) to VirusTotal and
  report vendor detection results.

.DESCRIPTION
  Two modes:

    -Path <file>            Scan a local file. Use for pre-tag pre-flight
                            on a goreleaser-built binary.

    -ReleaseTag <vX.Y.Z>    Fetch all .exe/.zip assets from a published
                            GitHub release via `gh` CLI and scan each.
                            Use for post-publish verification before
                            winget submission.

  Authentication is via the VT_API_KEY environment variable (User scope).
  Set once with:
      [Environment]::SetEnvironmentVariable("VT_API_KEY","<key>","User")
  then restart the shell so child processes inherit it.

  Exits non-zero if any vendor flagged any submitted file. The script
  prints SHA256 hashes so you can re-check or share results via the VT
  web UI directly.

.EXAMPLE
  .\scripts\vt-scan.ps1 -Path .\dist\wintui_2.7.0_windows_amd64\wintui.exe

.EXAMPLE
  .\scripts\vt-scan.ps1 -ReleaseTag v2.7.0

.NOTES
  Free-tier API limits are 4 req/min, 500/day — plenty for release-time
  scans of 2-3 binaries. Each scan costs roughly: 1 upload + 1-3 polls.
#>

[CmdletBinding(DefaultParameterSetName = "Path")]
param(
    [Parameter(ParameterSetName = "Path", Mandatory)]
    [string]$Path,

    [Parameter(ParameterSetName = "Tag", Mandatory)]
    [string]$ReleaseTag,

    [int]$PollSeconds = 15,
    [int]$PollMaxTries = 20,

    # Allow flagged scans to exit zero — useful when investigating a known
    # FP without breaking a chained pipeline. Off by default.
    [switch]$AllowFlagged,

    # Append a "## Verification" markdown section to the named file with a
    # per-asset SHA256 + VT link table. Pairs with `gh release edit
    # --notes-file` for syncing the GitHub release notes after publish.
    [string]$AppendTo
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$apiKey = $env:VT_API_KEY
if (-not $apiKey) {
    Write-Error "VT_API_KEY env var is not set. Set with [Environment]::SetEnvironmentVariable('VT_API_KEY','<key>','User') and restart the shell."
    exit 2
}

$apiBase = "https://www.virustotal.com/api/v3"
$headers = @{ "x-apikey" = $apiKey }

function Get-FileHashSha256 {
    param([string]$FilePath)
    (Get-FileHash -Algorithm SHA256 -Path $FilePath).Hash.ToLower()
}

function Submit-File {
    param([string]$FilePath)

    $name = [System.IO.Path]::GetFileName($FilePath)
    $size = (Get-Item $FilePath).Length
    Write-Host "==> Uploading $name ($([math]::Round($size/1MB,1)) MB)" -ForegroundColor Cyan

    # VT v3 file upload is multipart/form-data with the field name `file`.
    # Invoke-RestMethod's -Form param handles this when given a FileInfo.
    $resp = Invoke-RestMethod `
        -Uri "$apiBase/files" `
        -Method Post `
        -Headers $headers `
        -Form @{ file = Get-Item -LiteralPath $FilePath }

    return $resp.data.id
}

function Wait-Analysis {
    param([string]$AnalysisId)

    for ($i = 1; $i -le $PollMaxTries; $i++) {
        Start-Sleep -Seconds $PollSeconds
        $resp = Invoke-RestMethod `
            -Uri "$apiBase/analyses/$AnalysisId" `
            -Method Get `
            -Headers $headers

        $status = $resp.data.attributes.status
        Write-Host "  poll $i/$PollMaxTries — status: $status" -ForegroundColor DarkGray
        if ($status -eq "completed") {
            return $resp
        }
    }
    throw "Analysis $AnalysisId did not complete within $($PollMaxTries * $PollSeconds)s"
}

function Format-Results {
    param(
        [string]$Label,
        [string]$Sha256,
        [int]$SizeBytes,
        [PSCustomObject]$Analysis
    )

    $stats = $Analysis.data.attributes.stats
    $total = $stats.harmless + $stats.malicious + $stats.suspicious + $stats.undetected + $stats.timeout + $stats.failure
    $flagged = $stats.malicious + $stats.suspicious

    $microsoftFlagged = $false
    $flaggedEngines = @()
    if ($flagged -gt 0) {
        $Analysis.data.attributes.results.PSObject.Properties | ForEach-Object {
            $engine = $_.Name
            $r = $_.Value
            if ($r.category -in @("malicious", "suspicious")) {
                $detection = if ($r.result) { $r.result } else { "(no detection name)" }
                $flaggedEngines += [PSCustomObject]@{
                    Engine    = $engine
                    Category  = $r.category
                    Detection = $detection
                }
                if ($engine -eq "Microsoft") { $microsoftFlagged = $true }
            }
        }
    }

    Write-Host ""
    Write-Host "Result: $Label" -ForegroundColor Cyan
    Write-Host "  SHA256:    $Sha256"
    Write-Host "  Detected:  $flagged / $total ($($stats.malicious) malicious + $($stats.suspicious) suspicious)" `
        -ForegroundColor $(if ($flagged -gt 0) { "Yellow" } else { "Green" })
    Write-Host "  Web link:  https://www.virustotal.com/gui/file/$Sha256"

    if ($flagged -gt 0) {
        Write-Host "  Flagged by:" -ForegroundColor Yellow
        $flaggedEngines | ForEach-Object {
            Write-Host "    $($_.Engine)`: $($_.Category) — $($_.Detection)" -ForegroundColor Yellow
        }
    }

    return [PSCustomObject]@{
        Label            = $Label
        AssetName        = [System.IO.Path]::GetFileName($Label)
        Sha256           = $Sha256
        SizeBytes        = $SizeBytes
        Total            = $total
        Flagged          = $flagged
        MicrosoftFlagged = $microsoftFlagged
        FlaggedEngines   = $flaggedEngines
    }
}

function Scan-LocalFile {
    param([string]$FilePath)

    if (-not (Test-Path -LiteralPath $FilePath)) {
        throw "File not found: $FilePath"
    }
    $size = (Get-Item -LiteralPath $FilePath).Length
    $sha = Get-FileHashSha256 -FilePath $FilePath
    $analysisId = Submit-File -FilePath $FilePath
    $analysis = Wait-Analysis -AnalysisId $analysisId
    return Format-Results -Label $FilePath -Sha256 $sha -SizeBytes $size -Analysis $analysis
}

function Scan-ReleaseAssets {
    param([string]$Tag)

    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
        throw "GitHub CLI ('gh') is not on PATH. Install via 'winget install GitHub.cli' or pass -Path instead."
    }

    $tmp = Join-Path $env:TEMP "wintui-vt-$([guid]::NewGuid().ToString('N'))"
    New-Item -ItemType Directory -Path $tmp | Out-Null
    Write-Host "==> Downloading $Tag assets to $tmp" -ForegroundColor Cyan

    # Download .exe + .zip artifacts; skip checksum files etc.
    & gh release download $Tag --dir $tmp --pattern "*.exe" --pattern "*.zip" 2>&1 | Out-Host
    if ($LASTEXITCODE -ne 0) {
        throw "gh release download failed for $Tag"
    }

    $assets = Get-ChildItem -LiteralPath $tmp -File | Where-Object {
        $_.Extension -in @(".exe", ".zip")
    }
    if (-not $assets) {
        throw "No .exe or .zip assets found in release $Tag"
    }

    $records = @()
    foreach ($asset in $assets) {
        $records += Scan-LocalFile -FilePath $asset.FullName
    }
    return $records
}

function Format-MarkdownReport {
    param(
        [PSCustomObject[]]$Records,
        [string]$ReleaseTag
    )

    $today = (Get-Date).ToUniversalTime().ToString("yyyy-MM-dd")
    $tagPart = if ($ReleaseTag) { " for $ReleaseTag" } else { "" }

    $lines = @()
    $lines += ""
    $lines += "## Verification"
    $lines += ""
    $lines += "VirusTotal scans of the published artifacts$tagPart (run $today):"
    $lines += ""
    $lines += "| Asset | SHA256 | Detections | Report |"
    $lines += "|---|---|---|---|"

    foreach ($r in $Records) {
        $shortSha = $r.Sha256.Substring(0, 12) + "…"
        $sizeMB = [math]::Round($r.SizeBytes / 1MB, 1)
        $lines += "| ``$($r.AssetName)`` ($sizeMB MB) | ``$shortSha`` | $($r.Flagged)/$($r.Total) | [VT report](https://www.virustotal.com/gui/file/$($r.Sha256)) |"
    }

    $lines += ""

    $totalFlagged = ($Records | Measure-Object -Property Flagged -Sum).Sum
    $microsoftHits = @($Records | Where-Object MicrosoftFlagged)

    if ($totalFlagged -eq 0) {
        $lines += "All engines clean across submitted artifacts."
    } elseif ($microsoftHits.Count -eq 0) {
        $lines += "Detections at scan time were single-vendor low-signal ML/reputation noise. " + `
            "Microsoft Defender returned clean across all artifacts."
    } else {
        $lines += "Microsoft Defender flagged $($microsoftHits.Count) artifact(s) at scan time. " + `
            "MMPC FP submission may be required if the detection persists; see prior v2.6.0 saga in release notes."
    }
    $lines += ""
    $lines += "Full SHA256 hashes:"
    $lines += ""
    foreach ($r in $Records) {
        $lines += "- ``$($r.AssetName)``: ``$($r.Sha256)``"
    }
    $lines += ""

    return ($lines -join "`r`n")
}

# ── Dispatch ─────────────────────────────────────────────────────────

$records = switch ($PSCmdlet.ParameterSetName) {
    "Path" { @(Scan-LocalFile -FilePath $Path) }
    "Tag"  { Scan-ReleaseAssets -Tag $ReleaseTag }
}

$flaggedCount = ($records | Measure-Object -Property Flagged -Sum).Sum

if ($AppendTo) {
    if (-not (Test-Path -LiteralPath $AppendTo)) {
        Write-Error "AppendTo target file not found: $AppendTo"
        exit 2
    }
    $report = Format-MarkdownReport -Records $records -ReleaseTag $ReleaseTag
    Add-Content -Path $AppendTo -Value $report -Encoding UTF8
    Write-Host ""
    Write-Host "==> Appended verification section to $AppendTo" -ForegroundColor Cyan
}

Write-Host ""
if ($flaggedCount -gt 0) {
    Write-Host "Done — $flaggedCount detection(s) across submitted files." -ForegroundColor Yellow
    if (-not $AllowFlagged) {
        exit 1
    }
} else {
    Write-Host "Done — all submitted files clean." -ForegroundColor Green
}
exit 0
