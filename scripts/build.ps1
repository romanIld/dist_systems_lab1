# Builds every command into bin/. Each binary is built individually (building
# several main packages with `go build ./...` races on a shared temp file on
# Windows) and retried a few times, because on-access antivirus scanning can
# briefly lock a freshly written .exe.

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$binDir = Join-Path $root 'bin'
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

$cmds = 'server-threading', 'server-async', 'server-grpc', 'client-tcp', 'client-grpc', 'benchmark'

foreach ($cmd in $cmds) {
    $out = Join-Path $binDir "$cmd.exe"
    $ok = $false
    for ($try = 1; $try -le 5 -and -not $ok; $try++) {
        & go build -o $out "./cmd/$cmd"
        if ($LASTEXITCODE -eq 0 -and (Test-Path $out)) {
            $ok = $true
            Write-Host ("built {0}" -f $cmd)
        }
        else {
            Write-Host ("retry {0} ({1}/5)" -f $cmd, $try)
            Start-Sleep -Seconds 2
        }
    }
    if (-not $ok) { throw "failed to build $cmd" }
}

Write-Host 'all binaries present in bin/'
