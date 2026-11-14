# Modo desarrollo: Vite (hot reload del front) + air (recompila el backend Go).
# La ventana de Wails carga el front desde Vite; la API la sirve el binario Go.
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $root
try {
  $envFile = Join-Path $root '.env'
  if (Test-Path $envFile) {
    foreach ($line in Get-Content $envFile) {
      if ($line -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$' -and -not $line.TrimStart().StartsWith('#')) {
        if (-not (Test-Path "Env:$($Matches[1])")) { Set-Item "Env:$($Matches[1])" $Matches[2].Trim('"', "'") }
      }
    }
  }
  $go = if ($env:GO_BIN) { $env:GO_BIN } else { 'go' }
  $npm = if ($env:NPM_BIN) { $env:NPM_BIN } else { 'npm' }
  if ($env:NODE_BIN) { $env:PATH = "$(Split-Path -Parent $env:NODE_BIN);$env:PATH" }

  $air = Join-Path (& $go env GOPATH) 'bin\air.exe'
  if (-not (Test-Path $air)) {
    Write-Host 'Instalando air (recarga de Go)...' -ForegroundColor Cyan
    & $go install github.com/air-verse/air@latest
    if ($LASTEXITCODE -ne 0) { throw 'no pude instalar air' }
  }
  if (-not (Test-Path 'web\node_modules')) { Push-Location web; & $npm install; Pop-Location }
  if (-not (Test-Path 'web\public\workers\libredwg-web.wasm')) { Push-Location web; $node = if ($env:NODE_BIN) { $env:NODE_BIN } else { 'node' }; & $node scripts\assets.mjs; Pop-Location }

  Write-Host 'Vite en http://localhost:5173  |  backend con air' -ForegroundColor Cyan
  $vite = Start-Process -PassThru -NoNewWindow -WorkingDirectory (Join-Path $root 'web') -FilePath $npm -ArgumentList 'run', 'dev', '--', '--strictPort'
  Start-Sleep -Seconds 3
  $env:LCS_DEV_URL = 'http://localhost:5173'
  & $air -c .air.toml
} finally {
  if ($vite -and -not $vite.HasExited) { & taskkill /PID $vite.Id /T /F | Out-Null }
  Pop-Location
}
