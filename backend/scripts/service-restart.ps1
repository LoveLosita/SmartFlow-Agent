[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Service
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot\dev-common.ps1"

Initialize-DevState
Assert-ToolExists -Name "go"

$serviceDef = Get-BackendServiceDefinition -Name $Service

Write-Host "==> Restart service: $($serviceDef.Name)"
$result = Restart-BackendService -Service $serviceDef

Write-Host ""
Write-Host "Service restart summary:"
@($result) | Format-Table -AutoSize
