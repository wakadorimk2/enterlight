$ErrorActionPreference = "Stop"

$repo = "wakadorimk2/enterlight"
$installDir = Join-Path $env:LOCALAPPDATA "Programs\Enterlight"
$exePath = Join-Path $installDir "enterlight.exe"
$url = "https://github.com/$repo/releases/latest/download/enterlight-windows-amd64.exe"

New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $exePath

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
$parts = @($userPath -split ";" | Where-Object { $_ })
if ($parts -notcontains $installDir) {
  [Environment]::SetEnvironmentVariable("Path", (($parts + $installDir) -join ";"), "User")
}

Write-Host "Installed Enterlight to $exePath"
Write-Host "Open a new terminal, then run: enterlight doctor"
