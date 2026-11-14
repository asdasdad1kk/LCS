# Construye LCS.exe: compila la SPA, la embebe y arma el binario sin consola.
# (Solo ASCII: Windows PowerShell lee los .ps1 sin BOM como ANSI.)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $root
try {
  $envFile = Join-Path $root '.env'
  if (Test-Path $envFile) {
    foreach ($line in Get-Content $envFile) {
      $entry = $line.Trim()
      if (-not $entry -or $entry.StartsWith('#')) { continue }
      if ($entry -match '^export\s+') { $entry = $entry -replace '^export\s+', '' }
      if ($entry -match '^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') {
        $name = $Matches[1]
        $value = $Matches[2].Trim()
        if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
          $value = $value.Substring(1, $value.Length - 2)
        }
        if (-not (Test-Path "Env:$name") -or -not (Get-Item "Env:$name").Value) {
          Set-Item "Env:$name" $value
        }
      }
    }
  }
  $npmCommand = if ($env:NPM_BIN) { $env:NPM_BIN } else { 'npm' }
  $goCommand = if ($env:GO_BIN) { $env:GO_BIN } else { 'go' }
  $nodeCommand = if ($env:NODE_BIN) { $env:NODE_BIN } else { 'node' }
  if ($env:NODE_BIN) {
    $env:PATH = "$(Split-Path -Parent $env:NODE_BIN);$env:PATH"
  }

  Write-Host '[1/3] frontend' -ForegroundColor Cyan
  Push-Location web
  if (-not (Test-Path node_modules)) {
    & $npmCommand install
    if ($LASTEXITCODE -ne 0) { throw "npm install fallo con codigo $LASTEXITCODE" }
  }
  if (-not (Test-Path 'public\workers\libredwg-web.wasm')) {
    & $nodeCommand scripts\assets.mjs
    if ($LASTEXITCODE -ne 0) { throw "node scripts/assets.mjs fallo con codigo $LASTEXITCODE" }
  }
  & $npmCommand run build
  if ($LASTEXITCODE -ne 0) { throw "npm run build fallo con codigo $LASTEXITCODE" }
  Pop-Location

  Write-Host '[2/3] go build' -ForegroundColor Cyan
  $env:CGO_ENABLED = '1'
  & $goCommand build -tags "desktop,production" -trimpath -ldflags="-s -w -H windowsgui" -o LCS.exe ./cmd/lcs
  if ($LASTEXITCODE -ne 0) { throw "go build fallo con codigo $LASTEXITCODE" }

  $size = [math]::Round((Get-Item LCS.exe).Length / 1MB, 1)
  Write-Host "[3/3] OK - LCS.exe listo ($size MB)" -ForegroundColor Green
} finally {
  Pop-Location
}
