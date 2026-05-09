param(
    [string]$BaseRef = "",
    [string]$HeadRef = "HEAD",
    [string]$OutputFile = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
. (Join-Path $PSScriptRoot "service-catalog.ps1")

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

function Test-GitRef {
    param([string]$Ref)

    if ([string]::IsNullOrWhiteSpace($Ref)) {
        return $false
    }

    & git rev-parse --verify --quiet "$Ref^{commit}" | Out-Null
    return ($LASTEXITCODE -eq 0)
}

function Add-SelectedService {
    param(
        [System.Collections.Generic.List[string]]$Services,
        [string]$Service
    )

    if (-not $Services.Contains($Service)) {
        $Services.Add($Service)
    }
}

if (-not (Test-GitRef -Ref $HeadRef)) {
    throw ("head ref not found: {0}" -f $HeadRef)
}

$appTag = (& git rev-parse --short=12 $HeadRef).Trim()
$selectedServices = [System.Collections.Generic.List[string]]::new()
$frontendChanged = $false
$fullBackend = $false

if (Test-GitRef -Ref $BaseRef) {
    $changedFiles = @(& git diff --name-only $BaseRef $HeadRef)
} else {
    $changedFiles = @()
    $frontendChanged = $true
    $fullBackend = $true
}

foreach ($file in $changedFiles) {
    switch -Wildcard ($file) {
        "README.md" { continue }
        "docs/*" { continue }
        "frontend/*" { $frontendChanged = $true; continue }
        "deploy/nginx/*" { $frontendChanged = $true; continue }
        "deploy/docker-pack.*" { $frontendChanged = $true; $fullBackend = $true; continue }
        "deploy/docker-load.sh" { $frontendChanged = $true; $fullBackend = $true; continue }
        "deploy/stage-release.*" { $frontendChanged = $true; $fullBackend = $true; continue }
        "deploy/project-release.sh" { $frontendChanged = $true; $fullBackend = $true; continue }
        "deploy/project-rollback.sh" { $frontendChanged = $true; $fullBackend = $true; continue }
        "deploy/impact-rules.*" { $frontendChanged = $true; $fullBackend = $true; continue }
        "deploy/service-catalog.*" { $frontendChanged = $true; $fullBackend = $true; continue }
        "docker-compose.full.yml" { $frontendChanged = $true; $fullBackend = $true; continue }
        "frontend/nginx.conf" { $frontendChanged = $true; $fullBackend = $true; continue }
        ".env.full.example" { $frontendChanged = $true; $fullBackend = $true; continue }
        "backend/Dockerfile" { $fullBackend = $true; continue }
        "backend/config.docker.yaml" { $fullBackend = $true; continue }
        "backend/shared/*" { $fullBackend = $true; continue }
        "backend/client/*" { $fullBackend = $true; continue }
        "backend/gateway/*" { Add-SelectedService -Services $selectedServices -Service "api"; continue }
        "backend/cmd/api/*" { Add-SelectedService -Services $selectedServices -Service "api"; continue }
        "backend/cmd/userauth/*" { Add-SelectedService -Services $selectedServices -Service "userauth"; continue }
        "backend/services/userauth/*" { Add-SelectedService -Services $selectedServices -Service "userauth"; continue }
        "backend/cmd/notification/*" { Add-SelectedService -Services $selectedServices -Service "notification"; continue }
        "backend/services/notification/*" { Add-SelectedService -Services $selectedServices -Service "notification"; continue }
        "backend/cmd/active-scheduler/*" { Add-SelectedService -Services $selectedServices -Service "active-scheduler"; continue }
        "backend/services/active_scheduler/*" { Add-SelectedService -Services $selectedServices -Service "active-scheduler"; continue }
        "backend/cmd/schedule/*" { Add-SelectedService -Services $selectedServices -Service "schedule"; continue }
        "backend/services/schedule/*" { Add-SelectedService -Services $selectedServices -Service "schedule"; continue }
        "backend/cmd/task/*" { Add-SelectedService -Services $selectedServices -Service "task"; continue }
        "backend/services/task/*" { Add-SelectedService -Services $selectedServices -Service "task"; continue }
        "backend/cmd/task-class/*" { Add-SelectedService -Services $selectedServices -Service "task-class"; continue }
        "backend/services/task_class/*" { Add-SelectedService -Services $selectedServices -Service "task-class"; continue }
        "backend/cmd/course/*" { Add-SelectedService -Services $selectedServices -Service "course"; continue }
        "backend/services/course/*" { Add-SelectedService -Services $selectedServices -Service "course"; continue }
        "backend/cmd/memory/*" { Add-SelectedService -Services $selectedServices -Service "memory"; continue }
        "backend/services/memory/*" { Add-SelectedService -Services $selectedServices -Service "memory"; continue }
        "backend/cmd/agent/*" { Add-SelectedService -Services $selectedServices -Service "agent"; continue }
        "backend/services/agent/*" { Add-SelectedService -Services $selectedServices -Service "agent"; continue }
        "backend/cmd/taskclassforum/*" { Add-SelectedService -Services $selectedServices -Service "taskclassforum"; continue }
        "backend/services/taskclassforum/*" { Add-SelectedService -Services $selectedServices -Service "taskclassforum"; continue }
        "backend/cmd/tokenstore/*" { Add-SelectedService -Services $selectedServices -Service "tokenstore"; continue }
        "backend/services/tokenstore/*" { Add-SelectedService -Services $selectedServices -Service "tokenstore"; continue }
        "backend/cmd/llm/*" { Add-SelectedService -Services $selectedServices -Service "llm"; continue }
        "backend/services/llm/*" { Add-SelectedService -Services $selectedServices -Service "llm"; continue }
        "backend/*" { $fullBackend = $true; continue }
    }
}

if ($fullBackend) {
    $selectedServices.Clear()
    foreach ($service in Get-SmartFlowBackendServices) {
        $selectedServices.Add($service)
    }
}

if ($frontendChanged) {
    Add-SelectedService -Services $selectedServices -Service "frontend"
}

$buildBackend = 0
$buildFrontend = 0
foreach ($service in $selectedServices) {
    if ($service -eq "frontend") {
        $buildFrontend = 1
    } else {
        $buildBackend = 1
    }
}

$noop = if ($selectedServices.Count -eq 0) { 1 } else { 0 }
$restartCsv = [string]::Join(",", $selectedServices.ToArray())

$lines = [System.Collections.Generic.List[string]]::new()
$lines.Add("SMARTFLOW_APP_TAG=$appTag")
$lines.Add("SMARTFLOW_NOOP=$noop")
$lines.Add("SMARTFLOW_BUILD_BACKEND=$buildBackend")
$lines.Add("SMARTFLOW_BUILD_FRONTEND=$buildFrontend")
$lines.Add("SMARTFLOW_RESTART_SERVICES=$restartCsv")

foreach ($service in $selectedServices) {
    $imageEnv = Get-SmartFlowImageEnvForService -Service $service
    $imageRef = Get-SmartFlowDefaultImageForService -Service $service -AppTag $appTag
    $lines.Add(("{0}={1}" -f $imageEnv, $imageRef))
}

$content = [string]::Join("`n", $lines.ToArray())
if (-not [string]::IsNullOrWhiteSpace($OutputFile)) {
    $parent = Split-Path -Parent $OutputFile
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    $outputPath = if ([System.IO.Path]::IsPathRooted($OutputFile)) { $OutputFile } else { Join-Path (Get-Location) $OutputFile }
    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($outputPath, $content, $utf8NoBom)
} else {
    Write-Output $content
}
