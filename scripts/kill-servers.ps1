# Stops any echo server/client/benchmark process left running from this project.
# Useful on Windows where an interrupted run can keep a listener bound to a port.

$ErrorActionPreference = 'SilentlyContinue'

$root = Split-Path -Parent $PSScriptRoot
$names = 'server-threading', 'server-async', 'server-grpc', 'client-tcp', 'client-grpc', 'benchmark'

$procs = Get-CimInstance Win32_Process | Where-Object {
    $_.ExecutablePath -and $_.ExecutablePath.StartsWith($root, [System.StringComparison]::OrdinalIgnoreCase) -and
    ($names -contains [System.IO.Path]::GetFileNameWithoutExtension($_.Name))
}

if (-not $procs) {
    Write-Host 'No project server/client processes running.'
    return
}

foreach ($p in $procs) {
    Write-Host ("Stopping {0} (PID {1})" -f $p.Name, $p.ProcessId)
    Stop-Process -Id $p.ProcessId -Force
}
