# End-to-end pipeline on Windows: build -> unit tests -> benchmark -> charts.
# Pass extra benchmark flags through, e.g.:
#   powershell -File scripts/all.ps1 -BenchArgs '-concurrency 1,10,50,100,200'

param(
    [string]$BenchArgs = ''
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

& "$PSScriptRoot/kill-servers.ps1"
& "$PSScriptRoot/build.ps1"

Write-Host "`n== go test ==" -ForegroundColor Cyan
& go test ./...
if ($LASTEXITCODE -ne 0) { throw 'unit tests failed' }

Write-Host "`n== benchmark ==" -ForegroundColor Cyan
$argList = @('-bin', 'bin', '-out', 'results')
if ($BenchArgs) { $argList += $BenchArgs.Split(' ') }
& (Join-Path $root 'bin/benchmark.exe') @argList
if ($LASTEXITCODE -ne 0) { throw 'benchmark failed' }

Write-Host "`n== charts ==" -ForegroundColor Cyan
& python (Join-Path $PSScriptRoot 'plot.py') --csv results/summary.csv --outdir results
if ($LASTEXITCODE -ne 0) { throw 'plot.py failed' }

Write-Host "`nDone. See results/summary.csv and results/*.png" -ForegroundColor Green
