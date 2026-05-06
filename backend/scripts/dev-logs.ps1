[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Service,

    [ValidateSet("stdout", "stderr", "both")]
    [string]$Stream = "stdout",

    [int]$Tail = 80,

    [switch]$Follow
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot\dev-common.ps1"

Initialize-DevState

if ($Tail -le 0) {
    throw "Tail must be greater than 0"
}

$serviceDef = Get-BackendServiceDefinition -Name $Service
$streams = @(
    if ($Stream -eq "both") {
        "stdout"
        "stderr"
    }
    else {
        $Stream
    }
)

if ($Follow -and $streams.Count -gt 1) {
    throw "Follow mode only supports a single stream. Use -Stream stdout or -Stream stderr."
}

$paths = @()
foreach ($selectedStream in $streams) {
    $path = Get-LatestServiceLogPath -Service $serviceDef -Stream $selectedStream
    if ([string]::IsNullOrWhiteSpace($path)) {
        throw "No log file found for service $Service stream $selectedStream"
    }

    $paths += [pscustomobject]@{
        Stream = $selectedStream
        Path   = $path
    }
}
$paths = @($paths)

for ($index = 0; $index -lt $paths.Count; $index++) {
    $entry = $paths[$index]
    Write-Host "==> $Service [$($entry.Stream)]"
    Write-Host "Path: $($entry.Path)"
    if ($Follow) {
        Get-Content -LiteralPath $entry.Path -Encoding UTF8 -Tail $Tail -Wait
        return
    }

    Get-Content -LiteralPath $entry.Path -Encoding UTF8 -Tail $Tail
    if ($paths.Count -gt 1 -and $index -lt ($paths.Count - 1)) {
        Write-Host ""
    }
}
