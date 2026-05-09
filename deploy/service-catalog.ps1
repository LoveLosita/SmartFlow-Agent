$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

if ([string]::IsNullOrWhiteSpace($env:SMARTFLOW_SERVICE_CATALOG_FILE)) {
    $script:SmartFlowCatalogFile = Join-Path $PSScriptRoot "service-catalog.txt"
}
if (-not [string]::IsNullOrWhiteSpace($env:SMARTFLOW_SERVICE_CATALOG_FILE)) {
    $script:SmartFlowCatalogFile = $env:SMARTFLOW_SERVICE_CATALOG_FILE
}

function Get-SmartFlowServiceCatalog {
    if (-not (Test-Path -LiteralPath $script:SmartFlowCatalogFile)) {
        throw ("service catalog not found: {0}" -f $script:SmartFlowCatalogFile)
    }

    $items = @()
    foreach ($line in Get-Content -LiteralPath $script:SmartFlowCatalogFile -Encoding UTF8) {
        if ([string]::IsNullOrWhiteSpace($line) -or $line.TrimStart().StartsWith("#")) {
            continue
        }

        $parts = $line.Split("|")
        if ($parts.Count -ne 4) {
            throw ("invalid service catalog line: {0}" -f $line)
        }

        $items += [pscustomobject]@{
            Service = $parts[0].Trim()
            ImageEnv = $parts[1].Trim()
            ImageRepo = $parts[2].Trim()
            Kind = $parts[3].Trim()
        }
    }

    return $items
}

function Get-SmartFlowBackendServices {
    return @(Get-SmartFlowServiceCatalog | Where-Object { $_.Kind -eq "backend" } | ForEach-Object { $_.Service })
}

function Get-SmartFlowServiceCatalogItem {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Service
    )

    $item = Get-SmartFlowServiceCatalog | Where-Object { $_.Service -eq $Service } | Select-Object -First 1
    if ($null -eq $item) {
        throw ("unknown release service: {0}" -f $Service)
    }

    return $item
}

function Test-SmartFlowBackendService {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Service
    )

    return ((Get-SmartFlowServiceCatalogItem -Service $Service).Kind -eq "backend")
}

function Get-SmartFlowImageEnvForService {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Service
    )

    return (Get-SmartFlowServiceCatalogItem -Service $Service).ImageEnv
}

function Get-SmartFlowDefaultImageForService {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Service,

        [Parameter(Mandatory = $true)]
        [string]$AppTag
    )

    $item = Get-SmartFlowServiceCatalogItem -Service $Service
    return ("{0}:{1}" -f $item.ImageRepo, $AppTag)
}
