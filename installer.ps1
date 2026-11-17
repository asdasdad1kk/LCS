# Genera dist\LCS-Setup.exe: instalador de un solo archivo (LCS.exe va dentro, comprimido).
#   .\installer.ps1              compila LCS.exe (build.ps1) y luego el instalador
#   .\installer.ps1 -SkipBuild   reutiliza el LCS.exe que ya existe
# (Solo ASCII: Windows PowerShell lee los .ps1 sin BOM como ANSI.)
param([switch]$SkipBuild)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $root
try {
  $envFile = Join-Path $root '.env'
  if (Test-Path $envFile) {
    foreach ($line in Get-Content $envFile) {
      $e = $line.Trim()
      if ($e -match '^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$' -and -not $e.StartsWith('#')) {
        if (-not (Test-Path "Env:$($Matches[1])")) { Set-Item "Env:$($Matches[1])" $Matches[2].Trim().Trim('"', "'") }
      }
    }
  }
  $go = if ($env:GO_BIN) { $env:GO_BIN } else { 'go' }

  if (-not $SkipBuild) {
    & (Join-Path $root 'build.ps1')
    if ($LASTEXITCODE -ne 0) { throw 'build.ps1 fallo' }
  }
  if (-not (Test-Path 'LCS.exe')) { throw 'No existe LCS.exe: ejecuta .\build.ps1 primero' }

  Write-Host '[1/3] comprimiendo LCS.exe' -ForegroundColor Cyan
  $gz = Join-Path $root 'cmd\installer\payload\lcs.exe.gz'
  $src = [IO.File]::OpenRead((Join-Path $root 'LCS.exe'))
  $dst = [IO.File]::Create($gz)
  $zip = New-Object IO.Compression.GZipStream($dst, [IO.Compression.CompressionLevel]::Optimal)
  $src.CopyTo($zip)
  $zip.Dispose(); $dst.Dispose(); $src.Dispose()

  Write-Host '[2/3] go build installer' -ForegroundColor Cyan
  # Mismo icono y manifiesto que LCS.exe.
  Copy-Item 'cmd\lcs\rsrc_windows_amd64.syso' 'cmd\installer\rsrc_windows_amd64.syso' -Force
  New-Item -ItemType Directory -Force dist | Out-Null
  $env:CGO_ENABLED = '1'
  & $go build -tags "desktop,production" -trimpath -ldflags="-s -w -H windowsgui" -o dist\LCS-Setup.exe ./cmd/installer
  if ($LASTEXITCODE -ne 0) { throw "go build fallo con codigo $LASTEXITCODE" }

  $size = [math]::Round((Get-Item dist\LCS-Setup.exe).Length / 1MB, 1)
  Write-Host "[3/3] OK - dist\LCS-Setup.exe listo ($size MB)" -ForegroundColor Green
} finally {
  Pop-Location
}
