<#
.SYNOPSIS
  Check a binary (local file, GitHub release asset, or bare SHA256) on
  VirusTotal and report vendor detection results.

.DESCRIPTION
  Three modes:

    -Path <file>            Scan a local file. Use for pre-tag pre-flight
                            on a goreleaser-built binary.

    -ReleaseTag <vX.Y.Z>    Fetch all .exe/.zip assets plus checksums.txt
                            from a published GitHub release via `gh` CLI,
                            verify every asset against checksums.txt, and
                            scan each. Use for post-publish verification
                            before winget submission.

    -Sha256 <hash>[,<hash>] Look up hashes VirusTotal already knows. Never
                            uploads anything.

  Every file is looked up by SHA256 first (GET /files/{sha256}). When VT
  already holds a completed analysis, that cached report is used and its
  analysis date is printed; otherwise the file is uploaded and the
  analysis is polled. Two switches change that:

    -Rescan     Get a fresh analysis even when a cached report exists:
                POST /files/{sha256}/analyse when VT knows the file (no
                upload), a normal upload when it does not. On the public
                API a rescan can be accepted and then sit unprocessed (on
                2026-11-01 one was still missing after 15 min), which is
                reported as "timed out".

    -HashOnly   With -Path / -ReleaseTag: look the hashes up but never
                upload. A hash VT does not know is reported as "not on
                VirusTotal". -Sha256 is always hash-only. -Rescan still
                works with both, because /analyse uploads nothing.

  VT behaviour the script tolerates (both first hit 2026-11-01):

    - GET /analyses/{id} can answer 404 NotFoundError for an analysis that
      was created seconds earlier and later reads back as "completed". A
      404 is retried with backoff; after each one the file report is
      checked, since VT may already have filed the result there. If the
      analysis object stays missing, polling moves to the file report.
    - POST /files can answer AlreadySubmittedError ("Already being
      submitted for scanning") while an earlier upload of the same bytes
      is still in flight. The script then waits on the file report.
    - 429 quota errors, 5xx and network errors are retried with backoff,
      and a client-side throttle keeps calls under -RequestsPerMinute.

  Authentication is via the VT_API_KEY environment variable (User scope).
  Set once with:
      [Environment]::SetEnvironmentVariable("VT_API_KEY","<key>","User")
  A shell started before the key was set has no copy of it, so the script
  falls back to reading the User-scope value directly.

  Exit codes:
    0  Every scan completed and no vendor flagged anything, or flags were
       allowed with -AllowFlagged.
    1  A vendor flagged a file (without -AllowFlagged); a scan failed,
       timed out or found no VirusTotal report; a -ReleaseTag asset did
       not match checksums.txt; or any other runtime error.
    2  Configuration error: VT_API_KEY is not set, or the -AppendTo target
       file does not exist. Both are checked before any API call.

  The script prints SHA256 hashes so you can re-check or share results via
  the VT web UI directly.

.EXAMPLE
  .\scripts\vt-scan.ps1 -Path .\dist\wintui_2.7.0_windows_amd64\wintui.exe

.EXAMPLE
  .\scripts\vt-scan.ps1 -ReleaseTag v2.7.0

.EXAMPLE
  .\scripts\vt-scan.ps1 -ReleaseTag v2.7.0 -HashOnly -AppendTo .\docs\release-v2.7.0.md

.EXAMPLE
  .\scripts\vt-scan.ps1 -Sha256 d1e3985756bfba184eb914db23a49b73fe65997f9f56eae04e7bf9ccca688f75

.EXAMPLE
  .\scripts\vt-scan.ps1 -Path .\dist\wintui_2.7.0_windows_amd64\wintui.exe -Rescan

.NOTES
  Free-tier API limits are 4 req/min, 500/day. A file VT already has a
  report for costs 1 lookup. Otherwise: 1 lookup + 1 upload (or /analyse)
  + one poll every -PollSeconds, typically 2-6 polls.
#>

# PowerShell 7: Invoke-RestMethod -Form (the upload) does not exist in 5.1.
#Requires -Version 7.0

[CmdletBinding(DefaultParameterSetName = "Path")]
param(
    [Parameter(ParameterSetName = "Path", Mandatory)]
    [string]$Path,

    [Parameter(ParameterSetName = "Tag", Mandatory)]
    [string]$ReleaseTag,

    # One or more SHA256 values to look up. Never uploads.
    [Parameter(ParameterSetName = "Hash", Mandatory)]
    [ValidatePattern('^[0-9a-fA-F]{64}$')]
    [string[]]$Sha256,

    # Ask VT for a fresh analysis instead of reusing its cached report.
    [switch]$Rescan,

    # Look the -Path / -ReleaseTag hashes up, but never upload.
    [Parameter(ParameterSetName = "Path")]
    [Parameter(ParameterSetName = "Tag")]
    [switch]$HashOnly,

    [ValidateRange(1, 600)]
    [int]$PollSeconds = 15,

    # 40 polls × 15s = 10-minute ceiling per asset. Free-tier queue
    # times spike under load and we'd rather wait a bit than throw on
    # a still-queued analysis.
    [ValidateRange(1, 1000)]
    [int]$PollMaxTries = 40,

    # Client-side throttle, rolling 60-second window. 4 matches the
    # public API; raise it for a premium key, 0 disables it.
    [ValidateRange(0, 10000)]
    [int]$RequestsPerMinute = 4,

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

# Configuration errors exit 2. -ErrorAction Continue matters: under
# $ErrorActionPreference = "Stop" a bare Write-Error terminates the script
# with exit code 1 before the `exit 2` line runs.
$apiKey = $env:VT_API_KEY
if (-not $apiKey) {
    $apiKey = [Environment]::GetEnvironmentVariable("VT_API_KEY", "User")
}
if (-not $apiKey) {
    Write-Error "VT_API_KEY env var is not set. Set with [Environment]::SetEnvironmentVariable('VT_API_KEY','<key>','User') and restart the shell." -ErrorAction Continue
    exit 2
}
if ($AppendTo -and -not (Test-Path -LiteralPath $AppendTo)) {
    Write-Error "AppendTo target file not found: $AppendTo" -ErrorAction Continue
    exit 2
}

$apiBase = "https://www.virustotal.com/api/v3"
$headers = @{ "x-apikey" = $apiKey }

# POST /files takes up to 32 MB; bigger files (up to 650 MB) go to a
# one-time URL from GET /files/upload_url.
$directUploadMax = 32MB
$uploadMax = 650MB

# Consecutive analysis 404s after which the analysis object is written
# off and polling continues on the file report alone.
$notFoundLimit = 4

$requestLog = [System.Collections.Generic.Queue[datetime]]::new()

# StrictMode-safe nested property read ("data.attributes.status"):
# returns $null when a segment is missing instead of throwing. VT omits
# attributes it has no value for, e.g. last_analysis_date on a file whose
# first analysis is still running.
function Get-Prop {
    param($Object, [string]$Name)

    $current = $Object
    foreach ($part in $Name.Split(".")) {
        if ($null -eq $current) { return $null }
        $prop = $current.PSObject.Properties[$part]
        if (-not $prop) { return $null }
        $current = $prop.Value
    }
    return $current
}

function Get-FileHashSha256 {
    param([string]$FilePath)
    (Get-FileHash -Algorithm SHA256 -LiteralPath $FilePath).Hash.ToLower()
}

function ConvertFrom-UnixTime {
    param([long]$Seconds)
    [DateTimeOffset]::FromUnixTimeSeconds($Seconds).UtcDateTime
}

# Block until one more request fits in the rolling 60 s window.
function Wait-RateLimit {
    if ($RequestsPerMinute -le 0) { return }
    while ($requestLog.Count -ge $RequestsPerMinute) {
        $age = ((Get-Date) - $requestLog.Peek()).TotalSeconds
        if ($age -ge 60) {
            [void]$requestLog.Dequeue()
            continue
        }
        $wait = [math]::Ceiling(60 - $age)
        Write-Host "  (rate limit: waiting ${wait}s)" -ForegroundColor DarkGray
        Start-Sleep -Seconds $wait
    }
    $requestLog.Enqueue((Get-Date))
}

# Every VT call goes through here: throttle, retry, error parsing. HTTP
# errors come back as a result object instead of an exception, so callers
# can branch on VT's error code (NotFoundError, AlreadySubmittedError, …).
# Retried here is only what is transient on every endpoint: 429 quota
# errors, 5xx / TransientError / DeadlineExceededError, network failures.
function Invoke-Vt {
    param(
        [string]$Uri,
        [string]$Method = "Get",
        [System.IO.FileInfo]$UploadFile,
        [int]$MaxAttempts = 4
    )

    if ($Uri -notmatch '^https?://') { $Uri = "$apiBase$Uri" }
    $quotaCodes = @("QuotaExceededError", "TooManyRequestsError")
    $serverCodes = @("TransientError", "DeadlineExceededError")

    for ($attempt = 1; ; $attempt++) {
        Wait-RateLimit
        $status = 0
        $code = $null
        $network = $false
        try {
            $params = @{ Uri = $Uri; Method = $Method; Headers = $headers; TimeoutSec = 300 }
            if ($UploadFile) {
                # VT v3 file upload is multipart/form-data with the field
                # name `file`; -Form handles this when given a FileInfo.
                $params.Form = @{ file = $UploadFile }
            }
            $body = Invoke-RestMethod @params
            return [PSCustomObject]@{ Ok = $true; Status = 200; Code = $null; Message = $null; Body = $body }
        } catch {
            $err = $_
            $message = $err.Exception.Message
            $response = $err.Exception.PSObject.Properties["Response"]
            if ($response -and $response.Value) {
                $status = [int]$response.Value.StatusCode
            } else {
                $network = $err.Exception -is [System.Net.Http.HttpRequestException] -or
                    $err.Exception -is [System.Threading.Tasks.TaskCanceledException] -or
                    $err.Exception -is [System.TimeoutException]
            }
            # VT error bodies: {"error":{"code":"…","message":"…"}}
            $detail = if ($err.ErrorDetails) { $err.ErrorDetails.Message } else { $null }
            if ($detail) {
                try {
                    $json = $detail | ConvertFrom-Json -ErrorAction Stop
                    $code = Get-Prop $json "error.code"
                    $vtMessage = Get-Prop $json "error.message"
                    if ($vtMessage) { $message = $vtMessage }
                } catch {
                    if ($status) { $message = $detail }
                }
            }
        }

        $quota = $status -eq 429 -or $code -in $quotaCodes
        $transient = $quota -or $network -or $status -ge 500 -or $code -in $serverCodes
        if (-not $transient -or $attempt -ge $MaxAttempts) {
            return [PSCustomObject]@{ Ok = $false; Status = $status; Code = $code; Message = $message; Body = $null }
        }
        $delay = if ($quota) { 60 } else { [int][math]::Min(60, 15 * [math]::Pow(2, $attempt - 1)) }
        $what = if ($code) { $code } elseif ($status) { "HTTP $status" } else { "network error" }
        Write-Host "  $what — retrying in ${delay}s (attempt $attempt/$MaxAttempts)" -ForegroundColor DarkGray
        Start-Sleep -Seconds $delay
    }
}

function Format-VtError {
    param($Response)

    $parts = @()
    if ($Response.Status) { $parts += "HTTP $($Response.Status)" }
    if ($Response.Code) { $parts += $Response.Code }
    $text = $parts -join " "
    if ($Response.Message) {
        $text = if ($text) { "$text — $($Response.Message)" } else { $Response.Message }
    }
    if (-not $text) { $text = "unexpected response" }
    return $text
}

# AlreadySubmittedError is missing from VT's documented error table
# (checked 2026-11-01) but is what POST /files returns while the same
# bytes are still in flight. Match the message as well, in case VT files
# it under the documented AlreadyExistsError instead.
function Test-AlreadySubmitted {
    param($Response)
    ($Response.Code -in @("AlreadySubmittedError", "AlreadyExistsError")) -or
        ("$($Response.Message)" -match "already being (submitted|scanned)")
}

# Normalise either report shape into one object:
#   analysis object  /analyses/{id}   attributes.stats / results / date
#   file object      /files/{sha256}  attributes.last_analysis_stats / _results / _date
# Returns $null unless it holds a completed analysis (a date and at least
# one engine result).
function New-Report {
    param($Stats, $Results, $Date, $Size, [string]$Source)

    if (-not $Date -or -not $Stats -or -not $Results) { return $null }
    if (@($Results.PSObject.Properties).Count -eq 0) { return $null }
    return [PSCustomObject]@{
        Stats   = $Stats
        Results = $Results
        Date    = [long]$Date
        Size    = if ($Size) { [long]$Size } else { 0 }
        Source  = $Source
    }
}

# GET /files/{sha256}. Known = VT has the file at all; Report = its last
# completed analysis, $null while the first analysis is still running.
function Get-VtFile {
    param([string]$Sha)

    $r = Invoke-Vt -Uri "/files/$Sha"
    if (-not $r.Ok) {
        if ($r.Code -eq "NotFoundError") {
            return [PSCustomObject]@{ Known = $false; Report = $null }
        }
        throw "File lookup failed: $(Format-VtError $r)"
    }
    $attr = Get-Prop $r.Body "data.attributes"
    $report = New-Report -Stats (Get-Prop $attr "last_analysis_stats") `
        -Results (Get-Prop $attr "last_analysis_results") `
        -Date (Get-Prop $attr "last_analysis_date") `
        -Size (Get-Prop $attr "size") `
        -Source "cached report"
    return [PSCustomObject]@{ Known = $true; Report = $report }
}

# The file report, if it carries an analysis dated $Since or later.
function Get-FreshFileReport {
    param([string]$Sha, [long]$Since, [string]$Source)

    $file = Get-VtFile -Sha $Sha
    if ($file.Report -and $file.Report.Date -ge $Since) {
        $file.Report.Source = $Source
        return $file.Report
    }
    return $null
}

# VT analysis IDs are base64("<md5>:<unix-ts>"), and that timestamp is the
# analysis' date, which becomes the file's last_analysis_date (verified
# 2026-11-01 on two analyses). The format is undocumented, so fall back to
# the local clock with a skew margin when the ID does not decode.
function Get-AnalysisTimestamp {
    param([string]$AnalysisId)

    try {
        $plain = [Text.Encoding]::ASCII.GetString([Convert]::FromBase64String($AnalysisId))
        if ($plain -match ':(\d{9,11})$') { return [long]$Matches[1] }
    } catch {
        Write-Verbose "Analysis ID $AnalysisId does not decode; using the local clock"
    }
    return [DateTimeOffset]::UtcNow.ToUnixTimeSeconds() - 300
}

# Poll GET /analyses/{id} until completed. A 404 NotFoundError here is a
# VT-side transient, not a verdict: on 2026-11-01 two analyses 404'd
# mid-poll (one right after "queued") and both read back as "completed"
# later, with the results already on the file report. So after each 404
# the file report is checked for an analysis at least as new as this one,
# the next /analyses poll backs off (15, 30, 60, 60 s at the default
# -PollSeconds), and after $notFoundLimit consecutive 404s polling moves
# to the file report for the rest of the budget.
function Wait-Analysis {
    param([string]$AnalysisId, [string]$Sha, [string]$Source)

    $submittedAt = Get-AnalysisTimestamp -AnalysisId $AnalysisId
    $notFound = 0
    $delay = $PollSeconds
    for ($i = 1; $i -le $PollMaxTries; $i++) {
        Start-Sleep -Seconds $delay
        $delay = $PollSeconds

        if ($notFound -ge $notFoundLimit) {
            $report = Get-FreshFileReport -Sha $Sha -Since $submittedAt -Source "$Source, read from the file report (analysis object missing)"
            if ($report) { return $report }
            Write-Host "  poll $i/$PollMaxTries — file report not updated yet" -ForegroundColor DarkGray
            continue
        }

        $r = Invoke-Vt -Uri "/analyses/$AnalysisId"
        if ($r.Ok) {
            $notFound = 0
            $status = Get-Prop $r.Body "data.attributes.status"
            Write-Host "  poll $i/$PollMaxTries — status: $status" -ForegroundColor DarkGray
            if ($status -eq "completed") {
                $attr = Get-Prop $r.Body "data.attributes"
                $report = New-Report -Stats (Get-Prop $attr "stats") `
                    -Results (Get-Prop $attr "results") `
                    -Date (Get-Prop $attr "date") `
                    -Size (Get-Prop $r.Body "meta.file_info.size") `
                    -Source $Source
                if ($report) { return $report }
                # Completed but empty: read the file report instead.
                $report = Get-FreshFileReport -Sha $Sha -Since $submittedAt -Source "$Source, read from the file report"
                if ($report) { return $report }
                throw "Analysis $AnalysisId completed without engine results"
            }
            continue
        }

        if ($r.Code -ne "NotFoundError") {
            throw "Analysis poll failed: $(Format-VtError $r)"
        }
        $notFound++
        Write-Host "  poll $i/$PollMaxTries — analysis not found (NotFoundError $notFound/$notFoundLimit); checking the file report" -ForegroundColor DarkGray
        $report = Get-FreshFileReport -Sha $Sha -Since $submittedAt -Source "$Source, read from the file report (analysis object missing)"
        if ($report) { return $report }
        $delay = [int][math]::Min(60, $PollSeconds * [math]::Pow(2, $notFound - 1))
    }
    throw [System.TimeoutException]::new("Analysis $AnalysisId was still not completed after $PollMaxTries polls; VT may finish it later, so re-check with -Sha256 $Sha (one lookup, no upload)")
}

# Poll GET /files/{sha256} until it carries a completed analysis dated
# $Since or later. Used when there is no analysis ID to poll: on
# AlreadySubmittedError, or when VT knows the file but its first analysis
# has not finished.
function Wait-FileReport {
    param([string]$Sha, [long]$Since, [string]$Source)

    for ($i = 1; $i -le $PollMaxTries; $i++) {
        Start-Sleep -Seconds $PollSeconds
        $report = Get-FreshFileReport -Sha $Sha -Since $Since -Source $Source
        if ($report) { return $report }
        Write-Host "  poll $i/$PollMaxTries — file report not updated yet" -ForegroundColor DarkGray
    }
    throw [System.TimeoutException]::new("The file report was still not updated after $PollMaxTries polls; VT may finish it later, so re-check with -Sha256 $Sha (one lookup, no upload)")
}

function Submit-File {
    param([string]$FilePath)

    $item = Get-Item -LiteralPath $FilePath
    Write-Host "==> Uploading $($item.Name) ($([math]::Round($item.Length/1MB,1)) MB)" -ForegroundColor Cyan
    if ($item.Length -gt $uploadMax) {
        throw "$($item.Name) is larger than VT's 650 MB upload limit"
    }
    if ($item.Length -le $directUploadMax) {
        return Invoke-Vt -Uri "/files" -Method Post -UploadFile $item
    }

    # Upload URLs are single-use, so that POST gets one attempt only.
    $u = Invoke-Vt -Uri "/files/upload_url"
    $uploadUrl = if ($u.Ok) { Get-Prop $u.Body "data" } else { $null }
    if (-not $uploadUrl) {
        throw "Could not get a large-file upload URL: $(Format-VtError $u)"
    }
    return Invoke-Vt -Uri $uploadUrl -Method Post -UploadFile $item -MaxAttempts 1
}

# Decide how to get a report for one hash, then get it. $FilePath is $null
# in hash-only mode, which rules out uploading.
function Get-ScanReport {
    param([string]$Sha, [string]$FilePath)

    $file = Get-VtFile -Sha $Sha

    if (-not $Rescan) {
        if ($file.Report) {
            return $file.Report
        }
        if ($file.Known) {
            Write-Host "  VT knows this file, but its first analysis has not finished; waiting on the file report" -ForegroundColor DarkGray
            return Wait-FileReport -Sha $Sha -Since 0 -Source "file report (first analysis was in progress)"
        }
    }

    $previous = if ($file.Report) { $file.Report.Date } else { 0 }
    if ($file.Known) {
        Write-Host "==> Requesting a fresh analysis of $Sha" -ForegroundColor Cyan
        $r = Invoke-Vt -Uri "/files/$Sha/analyse" -Method Post
        $source = "rescan"
        $action = "Rescan request"
    } elseif (-not $FilePath) {
        throw [System.Collections.Generic.KeyNotFoundException]::new("VirusTotal has no file with this SHA256 (hash-only lookup, nothing uploaded)")
    } else {
        $r = Submit-File -FilePath $FilePath
        $source = "fresh upload"
        $action = "Upload"
    }

    if (-not $r.Ok) {
        if (Test-AlreadySubmitted $r) {
            Write-Host "  VT: $($r.Message) — waiting on the file report instead" -ForegroundColor DarkGray
            return Wait-FileReport -Sha $Sha -Since ($previous + 1) -Source "file report (already being submitted)"
        }
        throw "$action failed: $(Format-VtError $r)"
    }

    $analysisId = Get-Prop $r.Body "data.id"
    if (-not $analysisId) {
        throw "$action was accepted but VT returned no analysis ID"
    }
    return Wait-Analysis -AnalysisId $analysisId -Sha $Sha -Source $source
}

function Format-Results {
    param(
        [string]$Label,
        [string]$AssetName,
        [string]$Sha256,
        [long]$SizeBytes,
        [PSCustomObject]$Report
    )

    $count = @{}
    foreach ($name in @("harmless", "malicious", "suspicious", "undetected", "timeout", "failure")) {
        $value = Get-Prop $Report.Stats $name
        $count[$name] = if ($value) { [int]$value } else { 0 }
    }
    $total = $count.harmless + $count.malicious + $count.suspicious + $count.undetected + $count.timeout + $count.failure
    $flagged = $count.malicious + $count.suspicious
    $analysedAt = ConvertFrom-UnixTime $Report.Date

    $microsoftFlagged = $false
    $flaggedEngines = @()
    if ($flagged -gt 0) {
        $Report.Results.PSObject.Properties | ForEach-Object {
            $engine = $_.Name
            $category = Get-Prop $_.Value "category"
            if ($category -in @("malicious", "suspicious")) {
                $result = Get-Prop $_.Value "result"
                $detection = if ($result) { $result } else { "(no detection name)" }
                $flaggedEngines += [PSCustomObject]@{
                    Engine    = $engine
                    Category  = $category
                    Detection = $detection
                }
                if ($engine -eq "Microsoft") { $microsoftFlagged = $true }
            }
        }
    }

    $reportNote = "$($Report.Source), analysed $($analysedAt.ToString('yyyy-MM-dd HH:mm:ss')) UTC"
    if ($Report.Source -eq "cached report") { $reportNote += " (-Rescan for a fresh analysis)" }

    Write-Host ""
    Write-Host "Result: $Label" -ForegroundColor Cyan
    Write-Host "  SHA256:    $Sha256"
    Write-Host "  Report:    $reportNote"
    Write-Host "  Detected:  $flagged / $total ($($count.malicious) malicious + $($count.suspicious) suspicious)" `
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
        AssetName        = $AssetName
        Sha256           = $Sha256
        SizeBytes        = $SizeBytes
        Total            = $total
        Flagged          = $flagged
        MicrosoftFlagged = $microsoftFlagged
        FlaggedEngines   = $flaggedEngines
        Status           = "completed"
        Source           = $Report.Source
        AnalysisDate     = $analysedAt
        Error            = $null
    }
}

# One target to one record. Never throws: a failed target becomes a record
# with Flagged = -1 and a Status saying what actually happened ("timed
# out" only for a still-running analysis; "failed" for API errors; "not on
# VirusTotal" for an unknown hash in hash-only mode), so one bad asset
# cannot lose the others' results.
function Invoke-Scan {
    param([PSCustomObject]$Target)

    try {
        $report = Get-ScanReport -Sha $Target.Sha256 -FilePath $Target.FilePath
        $size = if ($Target.SizeBytes -gt 0) { $Target.SizeBytes } else { $report.Size }
        return Format-Results -Label $Target.Label -AssetName $Target.AssetName `
            -Sha256 $Target.Sha256 -SizeBytes $size -Report $report
    } catch {
        $status = if ($_.Exception -is [System.TimeoutException]) { "timed out" }
            elseif ($_.Exception -is [System.Collections.Generic.KeyNotFoundException]) { "not on VirusTotal" }
            else { "failed" }
        $what = if ($status -eq "not on VirusTotal") { $status } else { "scan $status" }
        Write-Host ""
        Write-Host "!! $($Target.AssetName): $what — $($_.Exception.Message)" -ForegroundColor Red
        return [PSCustomObject]@{
            Label            = $Target.Label
            AssetName        = $Target.AssetName
            Sha256           = $Target.Sha256
            SizeBytes        = $Target.SizeBytes
            Total            = 0
            Flagged          = -1
            MicrosoftFlagged = $false
            FlaggedEngines   = @()
            Status           = $status
            Source           = $null
            AnalysisDate     = $null
            Error            = $_.Exception.Message
        }
    }
}

function New-Target {
    param([string]$Label, [string]$AssetName, [string]$Sha, [string]$FilePath, [long]$SizeBytes)

    # FilePath = $null means "never upload".
    [PSCustomObject]@{
        Label     = $Label
        AssetName = $AssetName
        Sha256    = $Sha.ToLower()
        FilePath  = if ($HashOnly) { $null } else { $FilePath }
        SizeBytes = $SizeBytes
    }
}

function Get-LocalTarget {
    param([string]$FilePath)

    if (-not (Test-Path -LiteralPath $FilePath -PathType Leaf)) {
        throw "File not found: $FilePath"
    }
    $item = Get-Item -LiteralPath $FilePath
    return New-Target -Label $FilePath -AssetName $item.Name -Sha (Get-FileHashSha256 -FilePath $item.FullName) `
        -FilePath $item.FullName -SizeBytes $item.Length
}

# Download the release's .exe/.zip assets and checksums.txt, and verify
# every asset against checksums.txt before anything reaches VT: the report
# must describe the bytes the release actually publishes. A missing
# checksums.txt, an unlisted asset or a mismatch stops the run (exit 1).
function Get-ReleaseTargets {
    param([string]$Tag)

    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
        throw "GitHub CLI ('gh') is not on PATH. Install via 'winget install GitHub.cli' or pass -Path instead."
    }

    $tmp = Join-Path $env:TEMP "wintui-vt-$([guid]::NewGuid().ToString('N'))"
    New-Item -ItemType Directory -Path $tmp | Out-Null
    Write-Host "==> Downloading $Tag assets to $tmp" -ForegroundColor Cyan

    & gh release download $Tag --dir $tmp --pattern "*.exe" --pattern "*.zip" --pattern "checksums.txt" 2>&1 | Out-Host
    if ($LASTEXITCODE -ne 0) {
        throw "gh release download failed for $Tag"
    }

    $assets = @(Get-ChildItem -LiteralPath $tmp -File | Where-Object {
        $_.Extension -in @(".exe", ".zip")
    })
    if (-not $assets) {
        throw "No .exe or .zip assets found in release $Tag"
    }

    $sumsFile = Join-Path $tmp "checksums.txt"
    if (-not (Test-Path -LiteralPath $sumsFile)) {
        throw "Release $Tag has no checksums.txt; cannot verify the downloaded assets"
    }
    # goreleaser format: "<sha256>  <file name>"
    $expected = @{}
    foreach ($line in Get-Content -LiteralPath $sumsFile) {
        if ($line -match '^\s*([0-9a-fA-F]{64})\s+\*?(\S.*?)\s*$') {
            $expected[$Matches[2]] = $Matches[1].ToLower()
        }
    }

    Write-Host "==> Verifying assets against checksums.txt" -ForegroundColor Cyan
    $targets = @()
    foreach ($asset in $assets) {
        $sha = Get-FileHashSha256 -FilePath $asset.FullName
        if (-not $expected.ContainsKey($asset.Name)) {
            throw "$($asset.Name) is not listed in $Tag's checksums.txt"
        }
        if ($expected[$asset.Name] -ne $sha) {
            throw "Checksum mismatch for $($asset.Name): checksums.txt says $($expected[$asset.Name]), downloaded file is $sha"
        }
        Write-Host "  OK  $($asset.Name)  $sha" -ForegroundColor DarkGray
        $targets += New-Target -Label $asset.FullName -AssetName $asset.Name -Sha $sha `
            -FilePath $asset.FullName -SizeBytes $asset.Length
    }
    return $targets
}

# Detections across completed records; incomplete ones (Flagged = -1)
# don't count. Not Measure-Object: it emits nothing for empty input, and
# StrictMode turns the .Sum read into an error when every scan failed.
function Get-DetectionSum {
    param([PSCustomObject[]]$Records)

    $sum = 0
    foreach ($r in $Records) {
        if ($r.Flagged -gt 0) { $sum += $r.Flagged }
    }
    return $sum
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
        $sizePart = if ($r.SizeBytes -gt 0) { " ($([math]::Round($r.SizeBytes / 1MB, 1)) MB)" } else { "" }
        $detections = switch ($r.Status) {
            "completed"         { "$($r.Flagged)/$($r.Total)" }
            "timed out"         { "(scan timed out — re-check)" }
            "not on VirusTotal" { "(not on VirusTotal — re-check)" }
            default             { "(scan failed — re-check)" }
        }
        $lines += "| ``$($r.AssetName)``$sizePart | ``$shortSha`` | $detections | [VT report](https://www.virustotal.com/gui/file/$($r.Sha256)) |"
    }

    $lines += ""

    # Say so when a row reflects VT's existing report rather than a fresh
    # upload: its analysis date can predate this run.
    $cached = @($Records | Where-Object { $_.Status -eq "completed" -and $_.Source -eq "cached report" })
    if ($cached.Count -gt 0) {
        $list = ($cached | ForEach-Object {
            "``$($_.AssetName)`` (analysed $($_.AnalysisDate.ToString('yyyy-MM-dd HH:mm')) UTC)"
        }) -join ", "
        $lines += "Existing VirusTotal reports were reused, without a new upload, for $list."
        $lines += ""
    }

    # Incomplete scans (Flagged = -1) are excluded from the detection sum so
    # they don't poison the editorial summary line.
    $completedRecords = @($Records | Where-Object { $_.Flagged -ge 0 })
    $failedRecords = @($Records | Where-Object { $_.Flagged -lt 0 })
    $totalFlagged = Get-DetectionSum -Records $completedRecords
    $microsoftHits = @($Records | Where-Object MicrosoftFlagged)

    if ($completedRecords.Count -eq 0) {
        $lines += "No scan completed; every artifact needs a manual re-check."
    } elseif ($totalFlagged -eq 0) {
        $lines += "All engines clean across submitted artifacts."
    } elseif ($microsoftHits.Count -eq 0) {
        $lines += "Detections at scan time were single-vendor low-signal ML/reputation noise. " + `
            "Microsoft Defender returned clean across all artifacts."
    } else {
        $lines += "Microsoft Defender flagged $($microsoftHits.Count) artifact(s) at scan time. " + `
            "MMPC FP submission may be required if the detection persists; see prior v2.6.0 saga in release notes."
    }
    if ($failedRecords.Count -gt 0 -and $completedRecords.Count -gt 0) {
        $lines += ""
        $lines += "$($failedRecords.Count) artifact(s) could not be checked at scan time (see the table) and need a manual re-check."
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

$targets = switch ($PSCmdlet.ParameterSetName) {
    "Path" { @(Get-LocalTarget -FilePath $Path) }
    "Tag"  { Get-ReleaseTargets -Tag $ReleaseTag }
    "Hash" { $Sha256 | ForEach-Object { New-Target -Label $_ -AssetName $_.ToLower() -Sha $_ -FilePath $null -SizeBytes 0 } }
}

$records = @()
foreach ($target in @($targets)) {
    $records += Invoke-Scan -Target $target
}

# Incomplete scans (Flagged = -1) shouldn't add to the detection count;
# they show up with their status in the report and are surfaced
# separately at exit.
$failed = @($records | Where-Object { $_.Flagged -lt 0 })
$flaggedCount = Get-DetectionSum -Records $records

if ($AppendTo) {
    $report = Format-MarkdownReport -Records $records -ReleaseTag $ReleaseTag
    Add-Content -LiteralPath $AppendTo -Value $report -Encoding UTF8
    Write-Host ""
    Write-Host "==> Appended verification section to $AppendTo" -ForegroundColor Cyan
}

Write-Host ""
if ($failed.Count -gt 0) {
    $breakdown = ($failed | Group-Object -Property Status | ForEach-Object { "$($_.Count) $($_.Name)" }) -join ", "
    Write-Host "Done — $flaggedCount detection(s); $($failed.Count) scan(s) did not complete ($breakdown) and need manual re-check." -ForegroundColor Yellow
    exit 1
}
if ($flaggedCount -gt 0) {
    Write-Host "Done — $flaggedCount detection(s) across submitted files." -ForegroundColor Yellow
    if (-not $AllowFlagged) {
        exit 1
    }
} else {
    Write-Host "Done — all submitted files clean." -ForegroundColor Green
}
exit 0
