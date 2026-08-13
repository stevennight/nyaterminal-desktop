param(
  [ValidateSet("dev", "build")]
  [string]$Mode = "build",
  [switch]$Installer,
  [string]$Version = "0.1.0-dev",
  [string]$Commit = "",
  [string]$BuildDate = "",
  [string]$UpdateRepository = "nyaterminal/nyaterminal-desktop"
)

$ErrorActionPreference = "Stop"

Set-Location $PSScriptRoot

$iconSourcePath = Join-Path $PSScriptRoot "frontend\public\nyaterminal-icon-dark-gray.png"
$wailsIconPath = Join-Path $PSScriptRoot "build\appicon.png"
$wailsIcoPath = Join-Path $PSScriptRoot "build\windows\icon.ico"
if (-not (Test-Path -LiteralPath $iconSourcePath -PathType Leaf)) {
  throw "The application icon source is missing at $iconSourcePath."
}
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $wailsIconPath), (Split-Path -Parent $wailsIcoPath) | Out-Null
Copy-Item -LiteralPath $iconSourcePath -Destination $wailsIconPath -Force
if (Test-Path -LiteralPath $wailsIcoPath -PathType Leaf) {
  Remove-Item -LiteralPath $wailsIcoPath -Force
}

function Ensure-Command {
  param([string]$Name, [string]$InstallHint)
  if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
    throw "$Name was not found. $InstallHint"
  }
}

Ensure-Command go "Install Go and add it to PATH."
Ensure-Command npm "Install Node.js and add it to PATH."
$npm = Get-Command npm.cmd -ErrorAction SilentlyContinue
if (-not $npm) {
  $npm = Get-Command npm -ErrorAction Stop
}

$requiredWailsVersion = "v2.12.0"
$wails = Get-Command wails -ErrorAction SilentlyContinue
$installWails = -not $wails
if ($wails) {
  $installedWailsVersion = (& $wails.Source version 2>&1 | Out-String)
  $installWails = $installedWailsVersion -notmatch [regex]::Escape($requiredWailsVersion)
}
if ($installWails) {
  $gobin = Join-Path (go env GOPATH) "bin"
  $env:PATH = "$gobin;$env:PATH"
  Write-Host "Installing Wails CLI $requiredWailsVersion..." -ForegroundColor Yellow
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
  if ($LASTEXITCODE -ne 0) {
    throw "Could not install Wails CLI $requiredWailsVersion."
  }
  $gobin = Join-Path (go env GOPATH) "bin"
  $env:PATH = "$gobin;$env:PATH"
  $wails = Get-Command wails -ErrorAction SilentlyContinue
}
if (-not $wails) {
  throw "Wails CLI is still unavailable after installation."
}
if ($Installer) {
  if ($Mode -ne "build") {
    throw "-Installer can only be used with -Mode build."
  }
  if (-not $IsWindows -and $env:OS -ne "Windows_NT") {
    throw "The NSIS installer can only be built on Windows."
  }
  $makensis = Get-Command makensis -ErrorAction SilentlyContinue
  if (-not $makensis) {
    $programFilesRoots = @(
      [Environment]::GetEnvironmentVariable("ProgramFiles(x86)"),
      [Environment]::GetEnvironmentVariable("ProgramFiles")
    ) | Where-Object { $_ }

    foreach ($root in $programFilesRoots) {
      $candidate = Join-Path $root "NSIS\makensis.exe"
      if (Test-Path -LiteralPath $candidate -PathType Leaf) {
        $env:PATH = "$(Split-Path -Parent $candidate);$env:PATH"
        $makensis = Get-Command makensis -ErrorAction SilentlyContinue
        break
      }
    }
  }
  if (-not $makensis) {
    throw "makensis was not found. Install NSIS and add makensis.exe to PATH."
  }

  $installerTemplatePath = Join-Path $PSScriptRoot "installer\project.nsi"
  if (-not (Test-Path -LiteralPath $installerTemplatePath -PathType Leaf)) {
    throw "The NSIS installer template is missing at $installerTemplatePath."
  }
  $generatedInstallerPath = Join-Path $PSScriptRoot "build\windows\installer\project.nsi"
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $generatedInstallerPath) | Out-Null
  Copy-Item -LiteralPath $installerTemplatePath -Destination $generatedInstallerPath -Force
}

$productVersion = $Version.Trim()
if ($productVersion.StartsWith("v")) {
  $productVersion = $productVersion.Substring(1)
}
$stableVersion = $productVersion -match '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
if ($Installer -and -not $stableVersion) {
  throw "Installer versions must use stable MAJOR.MINOR.PATCH format."
}
if ($stableVersion) {
  $versionParts = $productVersion.Split('.') | ForEach-Object { [int64]$_ }
  if ($versionParts[0] -gt 255 -or $versionParts[1] -gt 255 -or $versionParts[2] -gt 65535) {
    throw "Version $productVersion exceeds Windows installer version limits."
  }
  $Version = $productVersion
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
    & $npm.Source install
    if ($LASTEXITCODE -ne 0) {
      throw "npm install failed with exit code $LASTEXITCODE."
    }
  }
  & $npm.Source run build
  if ($LASTEXITCODE -ne 0) {
    throw "npm run build failed with exit code $LASTEXITCODE."
  }
}
finally {
  Pop-Location
}

if ($Mode -eq "dev") {
  & $wails.Source dev
  exit $LASTEXITCODE
}

$ldflags = "-X github.com/nyaterminal/nyaterminal-desktop/internal/version.Version=$Version " +
  "-X github.com/nyaterminal/nyaterminal-desktop/internal/version.Commit=$Commit " +
  "-X github.com/nyaterminal/nyaterminal-desktop/internal/version.BuildDate=$BuildDate " +
  "-X github.com/nyaterminal/nyaterminal-desktop/internal/version.UpdateRepository=$UpdateRepository"

$configPath = Join-Path $PSScriptRoot "wails.json"
$originalConfig = $null
if ($stableVersion) {
  $originalConfig = [System.IO.File]::ReadAllText($configPath)
  $config = $originalConfig | ConvertFrom-Json
  $config.info.productVersion = $productVersion
  $updatedConfig = $config | ConvertTo-Json -Depth 20
  [System.IO.File]::WriteAllText($configPath, $updatedConfig, [System.Text.UTF8Encoding]::new($false))
}

try {
  $buildArguments = @("build", "-clean", "-trimpath", "-ldflags", $ldflags)
  if ($Installer) {
    $buildArguments += @("-platform", "windows/amd64", "-nsis")
  }
  & $wails.Source @buildArguments
  if ($LASTEXITCODE -ne 0) {
    throw "Wails build failed with exit code $LASTEXITCODE."
  }
  if ($Installer) {
    $installerPath = Join-Path $PSScriptRoot "build\bin\NyaTerminal-amd64-installer.exe"
    if (-not (Test-Path -LiteralPath $installerPath -PathType Leaf)) {
      throw "Wails did not produce the expected NSIS installer at $installerPath."
    }
  }
}
finally {
  if ($null -ne $originalConfig) {
    [System.IO.File]::WriteAllText($configPath, $originalConfig, [System.Text.UTF8Encoding]::new($false))
  }
}
