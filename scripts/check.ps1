param(
    [ValidateSet("pre-commit", "full")]
    [string]$Mode = "full"
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

function Invoke-Check {
    param(
        [string]$Name,
        [scriptblock]$Action
    )

    Write-Host "==> $Name" -ForegroundColor Cyan
    $global:LASTEXITCODE = 0
    & $Action
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE"
    }
}

function Get-GoFiles {
    param([bool]$StagedOnly)

    if ($StagedOnly) {
        $files = git diff --cached --name-only --diff-filter=ACMR -- '*.go'
    } else {
        $files = git ls-files -- '*.go'
    }

    return @($files | Where-Object { $_ -and (Test-Path $_) })
}

function Test-GoFmt {
    param([string[]]$Files)

    if (-not $Files -or $Files.Count -eq 0) {
        return
    }

    $unformatted = @(gofmt -l $Files)
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
    if ($unformatted.Count -gt 0) {
        Write-Host "Unformatted Go files:" -ForegroundColor Red
        $unformatted | ForEach-Object { Write-Host "  $_" -ForegroundColor Red }
        throw "gofmt check failed"
    }
}

$stagedOnly = $Mode -eq "pre-commit"
$goFiles = Get-GoFiles -StagedOnly:$stagedOnly

Invoke-Check "gofmt" { Test-GoFmt -Files $goFiles }

if ($Mode -eq "pre-commit") {
    exit 0
}

Invoke-Check "go test" { go test ./... }
Invoke-Check "go vet" { go vet ./... }

$staticcheck = Get-Command staticcheck -ErrorAction SilentlyContinue
if (-not $staticcheck) {
    throw "staticcheck not found in PATH. Install it with: go install honnef.co/go/tools/cmd/staticcheck@latest"
}
Invoke-Check "staticcheck" { staticcheck ./... }

$buildOut = Join-Path $env:TEMP "wintui-build-check.exe"
if (Test-Path $buildOut) {
    Remove-Item $buildOut -Force
}
try {
    Invoke-Check "go build" { go build -o $buildOut . }
} finally {
    if (Test-Path $buildOut) {
        Remove-Item $buildOut -Force
    }
}
