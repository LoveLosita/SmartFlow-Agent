param(
    [Parameter(Mandatory = $true)]
    [string]$ReleaseDir,

    [Parameter(Mandatory = $true)]
    [string]$PlanFile,

    [string]$BundleDir = ".docker-bundles"
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Convert-FileToUtf8Lf {
    param([string]$Path)

    $content = [System.IO.File]::ReadAllText($Path)
    $normalized = $content.Replace("`r`n", "`n")
    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($Path, $normalized, $utf8NoBom)
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$releaseAbs = Join-Path $repoRoot $ReleaseDir
$bundleAbs = Join-Path $repoRoot $BundleDir

if (-not (Test-Path -LiteralPath $PlanFile)) {
    throw ("release plan not found: {0}" -f $PlanFile)
}

if (Test-Path -LiteralPath $releaseAbs) {
    Remove-Item -LiteralPath $releaseAbs -Recurse -Force
}

New-Item -ItemType Directory -Force -Path (Join-Path $releaseAbs "deploy\nginx") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $releaseAbs "deploy\certs") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $releaseAbs ".docker-bundles") | Out-Null

Copy-Item -LiteralPath (Join-Path $repoRoot "docker-compose.full.yml") -Destination (Join-Path $releaseAbs "docker-compose.full.yml")
Copy-Item -LiteralPath (Join-Path $repoRoot "deploy\docker-load.sh") -Destination (Join-Path $releaseAbs "deploy\docker-load.sh")
Copy-Item -LiteralPath (Join-Path $repoRoot "deploy\project-release.sh") -Destination (Join-Path $releaseAbs "deploy\project-release.sh")
Copy-Item -LiteralPath (Join-Path $repoRoot "deploy\project-rollback.sh") -Destination (Join-Path $releaseAbs "deploy\project-rollback.sh")
Copy-Item -LiteralPath (Join-Path $repoRoot "deploy\impact-rules.sh") -Destination (Join-Path $releaseAbs "deploy\impact-rules.sh")
Copy-Item -LiteralPath (Join-Path $repoRoot "deploy\service-catalog.sh") -Destination (Join-Path $releaseAbs "deploy\service-catalog.sh")
Copy-Item -LiteralPath (Join-Path $repoRoot "deploy\service-catalog.txt") -Destination (Join-Path $releaseAbs "deploy\service-catalog.txt")
Copy-Item -LiteralPath (Join-Path $repoRoot "deploy\nginx\default.conf") -Destination (Join-Path $releaseAbs "deploy\nginx\default.conf")
Copy-Item -LiteralPath (Join-Path $repoRoot "deploy\certs\README.md") -Destination (Join-Path $releaseAbs "deploy\certs\README.md")
Copy-Item -LiteralPath $PlanFile -Destination (Join-Path $releaseAbs "deploy\release-plan.env")

$shellScripts = @(
    (Join-Path $releaseAbs "deploy\docker-load.sh"),
    (Join-Path $releaseAbs "deploy\project-release.sh"),
    (Join-Path $releaseAbs "deploy\project-rollback.sh"),
    (Join-Path $releaseAbs "deploy\impact-rules.sh"),
    (Join-Path $releaseAbs "deploy\service-catalog.sh")
)
foreach ($scriptPath in $shellScripts) {
    Convert-FileToUtf8Lf -Path $scriptPath
}

if (Test-Path -LiteralPath $bundleAbs) {
    $bundles = Get-ChildItem -LiteralPath $bundleAbs -Filter "*.tar" -File
    foreach ($bundle in $bundles) {
        Copy-Item -LiteralPath $bundle.FullName -Destination (Join-Path $releaseAbs ".docker-bundles")
    }
}
