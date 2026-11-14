# Construye LCS.exe: compila la SPA, la embebe y arma el binario sin consola.
# (Solo ASCII: Windows PowerShell lee los .ps1 sin BOM como ANSI.)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $root
try {
  Write-Host '[1/3] frontend' -ForegroundColor Cyan
  Push-Location web
  if (-not (Test-Path node_modules)) {
    npm install
  }
  if (-not (Test-Path 'public\workers\libredwg-web.wasm')) {
    node scripts\assets.mjs
  }
  npm run build
  Pop-Location

  Write-Host '[2/3] go build' -ForegroundColor Cyan
  $env:CGO_ENABLED = '0'
  go build -trimpath -ldflags="-s -w -H windowsgui" -o LCS.exe ./cmd/lcs
  if ($LASTEXITCODE -ne 0) { throw "go build fallo con codigo $LASTEXITCODE" }

  $size = [math]::Round((Get-Item LCS.exe).Length / 1MB, 1)
  Write-Host "[3/3] OK - LCS.exe listo ($size MB)" -ForegroundColor Green
} finally {
  Pop-Location
}
