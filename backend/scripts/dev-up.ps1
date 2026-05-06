[CmdletBinding()]
param(
    [switch]$SkipInfra
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot\dev-common.ps1"

Initialize-DevState
Assert-ToolExists -Name "go"

if (-not $SkipInfra) {
    Write-Host "==> Start infrastructure and wait for health checks"
    Start-BackendInfrastructure
}

$results = @()
:serviceLoop foreach ($service in (Get-BackendServiceDefinitions)) {
    Write-Host "==> Process service: $($service.Name)"
    $results += Ensure-ServiceStarted -Service $service
}

Write-Host ""
Write-Host "Backend start summary:"
$results | Format-Table -AutoSize
