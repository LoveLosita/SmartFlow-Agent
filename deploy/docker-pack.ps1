param(
    [string]$AppTag = "latest",
    [string]$BackendImage = "smartflow/backend-suite",
    [string]$FrontendImage = "smartflow/frontend",
    [string]$OutputDir = ".docker-bundles",
    [switch]$IncludeInfra
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Get-ImageRef {
    param(
        [string]$EnvName,
        [string]$DefaultValue
    )

    $value = [Environment]::GetEnvironmentVariable($EnvName)
    if ([string]::IsNullOrWhiteSpace($value)) {
        return $DefaultValue
    }

    return $value.Trim()
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$bundleDir = Join-Path $repoRoot $OutputDir
$backendRef = "{0}:{1}" -f $BackendImage, $AppTag
$frontendRef = "{0}:{1}" -f $FrontendImage, $AppTag
$appBundlePath = Join-Path $bundleDir ("smartflow-app-{0}.tar" -f $AppTag)
$infraBundlePath = Join-Path $bundleDir ("smartflow-infra-{0}.tar" -f $AppTag)

New-Item -ItemType Directory -Force -Path $bundleDir | Out-Null

Write-Host "==> Build backend image $backendRef"
docker build --platform linux/amd64 -f (Join-Path $repoRoot "backend\Dockerfile") -t $backendRef (Join-Path $repoRoot "backend")
if ($LASTEXITCODE -ne 0) {
    throw "Backend image build failed."
}

Write-Host "==> Build frontend image $frontendRef"
docker build --platform linux/amd64 -f (Join-Path $repoRoot "frontend\Dockerfile") -t $frontendRef (Join-Path $repoRoot "frontend")
if ($LASTEXITCODE -ne 0) {
    throw "Frontend image build failed."
}

if (Test-Path $appBundlePath) {
    Remove-Item -LiteralPath $appBundlePath -Force
}

Write-Host "==> Export app bundle to $appBundlePath"
docker save -o $appBundlePath $backendRef $frontendRef
if ($LASTEXITCODE -ne 0) {
    throw "App bundle export failed."
}

if (-not $IncludeInfra) {
    Write-Host "==> Done. App bundle exported."
    return
}

$infraImages = @(
    (Get-ImageRef -EnvName "SMARTFLOW_MYSQL_IMAGE" -DefaultValue "mysql:8.0"),
    (Get-ImageRef -EnvName "SMARTFLOW_REDIS_IMAGE" -DefaultValue "redis:7"),
    (Get-ImageRef -EnvName "SMARTFLOW_KAFKA_IMAGE" -DefaultValue "apache/kafka:3.7.2"),
    (Get-ImageRef -EnvName "SMARTFLOW_ETCD_IMAGE" -DefaultValue "quay.io/coreos/etcd:v3.5.5"),
    (Get-ImageRef -EnvName "SMARTFLOW_MINIO_IMAGE" -DefaultValue "minio/minio:RELEASE.2023-03-20T20-16-18Z"),
    (Get-ImageRef -EnvName "SMARTFLOW_MILVUS_IMAGE" -DefaultValue "milvusdb/milvus:v2.4.4"),
    (Get-ImageRef -EnvName "SMARTFLOW_ATTU_IMAGE" -DefaultValue "zilliz/attu:v2.4.3")
)

foreach ($imageRef in $infraImages) {
    Write-Host "==> Pull infra image $imageRef"
    docker pull $imageRef
    if ($LASTEXITCODE -ne 0) {
        throw ("Infra image pull failed: {0}" -f $imageRef)
    }
}

if (Test-Path $infraBundlePath) {
    Remove-Item -LiteralPath $infraBundlePath -Force
}

Write-Host "==> Export infra bundle to $infraBundlePath"
docker save -o $infraBundlePath @infraImages
if ($LASTEXITCODE -ne 0) {
    throw "Infra bundle export failed."
}

Write-Host "==> Done. App bundle and infra bundle exported."
