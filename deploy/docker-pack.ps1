param(
    [string]$AppTag = "latest",
    [string]$OutputDir = ".docker-bundles",
    [string]$PlanFile = "",
    [string]$Services = "",
    [switch]$IncludeInfra,
    [switch]$SkipBackend,
    [switch]$SkipFrontend,
    [string]$BackendImage = "",
    [string]$FrontendImage = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot "service-catalog.ps1")

function Read-ReleasePlan {
    param([string]$Path)

    $values = @{}
    if ([string]::IsNullOrWhiteSpace($Path)) {
        return $values
    }
    if (-not (Test-Path -LiteralPath $Path)) {
        throw ("release plan not found: {0}" -f $Path)
    }

    foreach ($line in Get-Content -LiteralPath $Path -Encoding UTF8) {
        if ([string]::IsNullOrWhiteSpace($line) -or $line.TrimStart().StartsWith("#")) {
            continue
        }

        $parts = $line -split "=", 2
        if ($parts.Count -ne 2) {
            throw ("invalid release plan line: {0}" -f $line)
        }
        $values[$parts[0].Trim()] = $parts[1].Trim()
    }

    return $values
}

function Get-ImageRefForService {
    param(
        [string]$Service,
        [hashtable]$Plan,
        [string]$Tag
    )

    $imageEnv = Get-SmartFlowImageEnvForService -Service $Service
    if ($Plan.ContainsKey($imageEnv) -and -not [string]::IsNullOrWhiteSpace($Plan[$imageEnv])) {
        return $Plan[$imageEnv]
    }

    return Get-SmartFlowDefaultImageForService -Service $Service -AppTag $Tag
}

function Invoke-Docker {
    param([string[]]$Arguments)

    & docker @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw ("docker command failed: docker {0}" -f ($Arguments -join " "))
    }
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$bundleDir = Join-Path $repoRoot $OutputDir
$plan = Read-ReleasePlan -Path $PlanFile

if ($plan.ContainsKey("SMARTFLOW_APP_TAG") -and -not [string]::IsNullOrWhiteSpace($plan["SMARTFLOW_APP_TAG"])) {
    $AppTag = $plan["SMARTFLOW_APP_TAG"]
}

if ([string]::IsNullOrWhiteSpace($Services) -and $plan.ContainsKey("SMARTFLOW_RESTART_SERVICES")) {
    $Services = $plan["SMARTFLOW_RESTART_SERVICES"]
}

$selectedServices = @()
if (-not [string]::IsNullOrWhiteSpace($Services)) {
    $selectedServices = @($Services.Split(",") | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | ForEach-Object { $_.Trim() })
} elseif ([string]::IsNullOrWhiteSpace($PlanFile)) {
    if (-not $SkipBackend) {
        $selectedServices += @(Get-SmartFlowBackendServices)
    }
    if (-not $SkipFrontend) {
        $selectedServices += "frontend"
    }
}

New-Item -ItemType Directory -Force -Path $bundleDir | Out-Null
$appBundlePath = Join-Path $bundleDir ("smartflow-app-{0}.tar" -f $AppTag)
$infraBundlePath = Join-Path $bundleDir ("smartflow-infra-{0}.tar" -f $AppTag)

if (Test-Path -LiteralPath $appBundlePath) {
    Remove-Item -LiteralPath $appBundlePath -Force
}

$appImages = @()
foreach ($service in $selectedServices) {
    if ($service -eq "frontend") {
        if ($SkipFrontend) {
            continue
        }

        $imageRef = Get-ImageRefForService -Service $service -Plan $plan -Tag $AppTag
        Write-Host "==> Build frontend image $imageRef"
        Invoke-Docker -Arguments @(
            "build", "--platform", "linux/amd64",
            "-f", (Join-Path $repoRoot "frontend\Dockerfile"),
            "-t", $imageRef,
            (Join-Path $repoRoot "frontend")
        )
        $appImages += $imageRef
        continue
    }

    if ($SkipBackend) {
        continue
    }
    if (-not (Test-SmartFlowBackendService -Service $service)) {
        throw ("unknown backend release service: {0}" -f $service)
    }

    $imageRef = Get-ImageRefForService -Service $service -Plan $plan -Tag $AppTag
    Write-Host "==> Build backend service image $imageRef"
    Invoke-Docker -Arguments @(
        "build", "--platform", "linux/amd64",
        "--target", "runtime-service",
        "--build-arg", ("SERVICE={0}" -f $service),
        "-f", (Join-Path $repoRoot "backend\Dockerfile"),
        "-t", $imageRef,
        (Join-Path $repoRoot "backend")
    )
    $appImages += $imageRef
}

if ($appImages.Count -gt 0) {
    Write-Host "==> Export app bundle to $appBundlePath"
    Invoke-Docker -Arguments (@("save", "-o", $appBundlePath) + $appImages)
} else {
    Write-Host "==> Skip app bundle export because no application image is selected"
}

if (-not $IncludeInfra) {
    Write-Host "==> Done."
    return
}

$infraImages = @()
if ([string]::IsNullOrWhiteSpace($env:SMARTFLOW_MYSQL_IMAGE)) { $infraImages += "mysql:8.0" } else { $infraImages += $env:SMARTFLOW_MYSQL_IMAGE }
if ([string]::IsNullOrWhiteSpace($env:SMARTFLOW_REDIS_IMAGE)) { $infraImages += "redis:7" } else { $infraImages += $env:SMARTFLOW_REDIS_IMAGE }
if ([string]::IsNullOrWhiteSpace($env:SMARTFLOW_KAFKA_IMAGE)) { $infraImages += "apache/kafka:3.7.2" } else { $infraImages += $env:SMARTFLOW_KAFKA_IMAGE }
if ([string]::IsNullOrWhiteSpace($env:SMARTFLOW_ETCD_IMAGE)) { $infraImages += "quay.io/coreos/etcd:v3.5.5" } else { $infraImages += $env:SMARTFLOW_ETCD_IMAGE }
if ([string]::IsNullOrWhiteSpace($env:SMARTFLOW_MINIO_IMAGE)) { $infraImages += "minio/minio:RELEASE.2023-03-20T20-16-18Z" } else { $infraImages += $env:SMARTFLOW_MINIO_IMAGE }
if ([string]::IsNullOrWhiteSpace($env:SMARTFLOW_MILVUS_IMAGE)) { $infraImages += "milvusdb/milvus:v2.4.4" } else { $infraImages += $env:SMARTFLOW_MILVUS_IMAGE }
if ([string]::IsNullOrWhiteSpace($env:SMARTFLOW_ATTU_IMAGE)) { $infraImages += "zilliz/attu:v2.4.3" } else { $infraImages += $env:SMARTFLOW_ATTU_IMAGE }

foreach ($imageRef in $infraImages) {
    & docker image inspect $imageRef | Out-Null
    if ($LASTEXITCODE -eq 0) {
        Write-Host "==> Reuse local infra image $imageRef"
        continue
    }

    Write-Host "==> Pull infra image $imageRef"
    Invoke-Docker -Arguments @("pull", $imageRef)
}

if (Test-Path -LiteralPath $infraBundlePath) {
    Remove-Item -LiteralPath $infraBundlePath -Force
}

Write-Host "==> Export infra bundle to $infraBundlePath"
Invoke-Docker -Arguments (@("save", "-o", $infraBundlePath) + $infraImages)

Write-Host "==> Done."
