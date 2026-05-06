[CmdletBinding()]
param(
    [switch]$StopInfra
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot\dev-common.ps1"

Initialize-DevState

$services = @(Get-BackendServiceDefinitions)
[array]::Reverse($services)

$results = @()
foreach ($service in $services) {
    Write-Host "==> Stop service: $($service.Name)"
    $results += Stop-ServiceProcess -Service $service
}

if ($StopInfra) {
    Write-Host "==> Stop infrastructure containers"
    Stop-BackendInfrastructure
}

Write-Host ""
Write-Host "Backend stop summary:"
$results | Format-Table -AutoSize
