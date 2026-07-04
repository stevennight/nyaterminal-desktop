param(
  [ValidateSet("dev", "build")]
  [string]$Mode = "build",
  [string]$Version = "0.1.0-dev",
  [string]$Commit = "",
  [string]$BuildDate = "",
  [string]$UpdateRepository = "nyaterminal/nyaterminal-desktop"
)

$ErrorActionPreference = "Stop"

Set-Location $PSScriptRoot

function Ensure-Command {
  param([string]$Name, [string]$InstallHint)
  if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
    throw "$Name was not found. $InstallHint"
  }
}

Ensure-Command go "Install Go and add it to PATH."
Ensure-Command npm "Install Node.js and add it to PATH."

$wails = Get-Command wails -ErrorAction SilentlyContinue
if (-not $wails) {
  $gobin = Join-Path (go env GOPATH) "bin"
  $env:PATH = "$gobin;$env:PATH"
  $wails = Get-Command wails -ErrorAction SilentlyContinue
}
if (-not $wails) {
  Write-Host "Wails CLI not found. Installing it with go install..." -ForegroundColor Yellow
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
  $gobin = Join-Path (go env GOPATH) "bin"
  $env:PATH = "$gobin;$env:PATH"
  $wails = Get-Command wails -ErrorAction SilentlyContinue
}
if (-not $wails) {
  throw "Wails CLI is still unavailable after installation."
}

Push-Location frontend
try {
  if (-not $BuildDate) {
    $BuildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
  }
  $env:NYATERMINAL_DESKTOP_VERSION = $Version
  $env:NYATERMINAL_DESKTOP_COMMIT = $Commit
  $env:NYATERMINAL_DESKTOP_BUILD_DATE = $BuildDate
  $env:NYATERMINAL_UPDATE_REPOSITORY = $UpdateRepository
  if (-not (Test-Path node_modules)) {
    npm install
  }
  npm run build
}
finally {
  Pop-Location
}

if ($Mode -eq "dev") {
  & wails dev
  exit $LASTEXITCODE
}

$ldflags = "-X github.com/nyaterminal/nyaterminal-desktop/internal/version.Version=$Version " +
  "-X github.com/nyaterminal/nyaterminal-desktop/internal/version.Commit=$Commit " +
  "-X github.com/nyaterminal/nyaterminal-desktop/internal/version.BuildDate=$BuildDate " +
  "-X github.com/nyaterminal/nyaterminal-desktop/internal/version.UpdateRepository=$UpdateRepository"
& wails build -ldflags $ldflags
exit $LASTEXITCODE
