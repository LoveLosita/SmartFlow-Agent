[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot\dev-common.ps1"

Initialize-DevState

$serviceRows = foreach ($service in (Get-BackendServiceDefinitions)) {
    $status = Get-ServiceStatus -Service $service
    [pscustomobject]@{
        Name   = $service.Name
        Port   = $service.Port
        Status = $status.Summary
        PID    = $(if ($null -ne $status.Pid) { $status.Pid } else { "-" })
    }
}

Write-Host "Backend service status:"
$serviceRows | Format-Table -AutoSize

if (Get-Command docker -ErrorAction SilentlyContinue) {
    Write-Host ""
    Write-Host "Infrastructure status:"
    Get-InfrastructureStatus | Format-Table -AutoSize
}
else {
    Write-Host ""
    Write-Host "Infrastructure status: docker command not found"
}
