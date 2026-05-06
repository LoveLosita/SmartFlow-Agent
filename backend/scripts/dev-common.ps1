$BackendRoot = Split-Path -Parent $PSScriptRoot
$RepoRoot = Split-Path -Parent $BackendRoot
$ComposeFile = Join-Path $RepoRoot "docker-compose.yml"
$StateRoot = Join-Path $BackendRoot ".dev"
$PidRoot = Join-Path $StateRoot "pids"
$LogRoot = Join-Path $StateRoot "logs"
$BinRoot = Join-Path $StateRoot "bin"
$DockerConfigRoot = Join-Path $StateRoot "docker-config"
$GoCacheRoot = Join-Path $StateRoot "gocache"

function Initialize-DevState {
    foreach ($path in @($StateRoot, $PidRoot, $LogRoot, $BinRoot, $DockerConfigRoot, $GoCacheRoot)) {
        if (-not (Test-Path -LiteralPath $path)) {
            New-Item -ItemType Directory -Path $path -Force | Out-Null
        }
    }

    # 1. 当前桌面环境里可能同时注入 Path / PATH 两个大小写不同的环境变量。
    # 2. Windows PowerShell 的 Env: 提供器与 Start-Process 在这种情况下会报“重复键”错误。
    # 3. 这里统一只保留一个进程级 Path，避免启动脚本因为环境脏数据直接失败。
    $effectivePath = ""
    try {
        $pathValue = [System.Environment]::GetEnvironmentVariable("Path", "Process")
        if ($null -ne $pathValue) {
            $effectivePath = [string]$pathValue
        }
    }
    catch {
        $effectivePath = ""
    }
    [System.Environment]::SetEnvironmentVariable("PATH", $null, "Process")
    [System.Environment]::SetEnvironmentVariable("Path", $null, "Process")
    [System.Environment]::SetEnvironmentVariable("Path", $effectivePath, "Process")

    $env:DOCKER_CONFIG = $DockerConfigRoot
    $env:GOCACHE = $GoCacheRoot
}

function Assert-ToolExists {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Command not found: $Name"
    }
}

function Invoke-ExternalCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [Parameter(Mandatory = $true)]
        [string[]]$Arguments,

        [Parameter(Mandatory = $true)]
        [string]$WorkingDirectory
    )

    Push-Location $WorkingDirectory
    try {
        & $FilePath @Arguments
        if ($LASTEXITCODE -ne 0) {
            throw "Command failed: $FilePath $($Arguments -join ' ')"
        }
    }
    finally {
        Pop-Location
    }
}

function Get-InfrastructureDefinitions {
    return @(
        [pscustomobject]@{
            Name       = "mysql"
            Container  = "smartflow-mysql"
            TimeoutSec = 120
        },
        [pscustomobject]@{
            Name       = "redis"
            Container  = "smartflow-redis"
            TimeoutSec = 90
        },
        [pscustomobject]@{
            Name       = "kafka"
            Container  = "smartflow-kafka"
            TimeoutSec = 180
        },
        [pscustomobject]@{
            Name       = "etcd"
            Container  = "smartflow-etcd"
            TimeoutSec = 120
        },
        [pscustomobject]@{
            Name       = "minio"
            Container  = "smartflow-minio"
            TimeoutSec = 120
        },
        [pscustomobject]@{
            Name       = "milvus-standalone"
            Container  = "smartflow-milvus"
            TimeoutSec = 240
        }
    )
}

function Get-InfrastructureComposeServices {
    return @(
        "mysql",
        "redis",
        "kafka",
        "etcd",
        "minio",
        "milvus-standalone",
        "kafka-init"
    )
}

function Get-KafkaTopicDefinitions {
    return @(
        "smartflow.agent.outbox",
        "smartflow.task.outbox",
        "smartflow.memory.outbox",
        "smartflow.active-scheduler.outbox",
        "smartflow.notification.outbox",
        "smartflow.taskclass-forum.outbox",
        "smartflow.llm.outbox",
        "smartflow.token-store.outbox"
    )
}

function Get-BackendServiceDefinitions {
    return @(
        [pscustomobject]@{
            Name            = "userauth"
            Package         = "./cmd/userauth"
            BinaryPath      = (Join-Path $BinRoot "userauth.exe")
            Port            = 9081
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 60
            Dependencies    = @()
        },
        [pscustomobject]@{
            Name            = "task"
            Package         = "./cmd/task"
            BinaryPath      = (Join-Path $BinRoot "task.exe")
            Port            = 9085
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 90
            Dependencies    = @()
        },
        [pscustomobject]@{
            Name            = "schedule"
            Package         = "./cmd/schedule"
            BinaryPath      = (Join-Path $BinRoot "schedule.exe")
            Port            = 9084
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 90
            Dependencies    = @()
        },
        [pscustomobject]@{
            Name            = "task-class"
            Package         = "./cmd/task-class"
            BinaryPath      = (Join-Path $BinRoot "task-class.exe")
            Port            = 9086
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 90
            Dependencies    = @()
        },
        [pscustomobject]@{
            Name            = "llm"
            Package         = "./cmd/llm"
            BinaryPath      = (Join-Path $BinRoot "llm.exe")
            Port            = 9096
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 120
            Dependencies    = @()
        },
        [pscustomobject]@{
            Name            = "course"
            Package         = "./cmd/course"
            BinaryPath      = (Join-Path $BinRoot "course.exe")
            Port            = 9087
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 120
            Dependencies    = @("llm")
        },
        [pscustomobject]@{
            Name            = "tokenstore"
            Package         = "./cmd/tokenstore"
            BinaryPath      = (Join-Path $BinRoot "tokenstore.exe")
            Port            = 9095
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 90
            Dependencies    = @()
        },
        [pscustomobject]@{
            Name            = "notification"
            Package         = "./cmd/notification"
            BinaryPath      = (Join-Path $BinRoot "notification.exe")
            Port            = 9082
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 90
            Dependencies    = @()
        },
        [pscustomobject]@{
            Name            = "memory"
            Package         = "./cmd/memory"
            BinaryPath      = (Join-Path $BinRoot "memory.exe")
            Port            = 9088
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 150
            Dependencies    = @("llm")
        },
        [pscustomobject]@{
            Name            = "taskclassforum"
            Aliases         = @("forum")
            Package         = "./cmd/taskclassforum"
            BinaryPath      = (Join-Path $BinRoot "taskclassforum.exe")
            Port            = 9090
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 90
            Dependencies    = @("task-class")
        },
        [pscustomobject]@{
            Name            = "active-scheduler"
            Package         = "./cmd/active-scheduler"
            BinaryPath      = (Join-Path $BinRoot "active-scheduler.exe")
            Port            = 9083
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 120
            Dependencies    = @("task", "schedule", "llm")
        },
        [pscustomobject]@{
            Name            = "agent"
            Package         = "./cmd/agent"
            BinaryPath      = (Join-Path $BinRoot "agent.exe")
            Port            = 9089
            ProbeType       = "tcp"
            ProbeTarget     = $null
            StartTimeoutSec = 180
            Dependencies    = @("task", "schedule", "task-class", "memory", "llm")
        },
        [pscustomobject]@{
            Name            = "api"
            Package         = "./cmd/api"
            BinaryPath      = (Join-Path $BinRoot "api.exe")
            Port            = 8080
            ProbeType       = "http"
            ProbeTarget     = "http://127.0.0.1:8080/api/v1/health"
            StartTimeoutSec = 90
            Dependencies    = @(
                "userauth",
                "task",
                "schedule",
                "task-class",
                "course",
                "llm",
                "tokenstore",
                "notification",
                "memory",
                "taskclassforum",
                "active-scheduler",
                "agent"
            )
        }
    )
}

function Get-BackendServiceDefinition {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    $serviceDefinitions = @(Get-BackendServiceDefinitions)
    foreach ($service in $serviceDefinitions) {
        $aliases = @()
        if ($service.PSObject.Properties.Name -contains "Aliases" -and $null -ne $service.Aliases) {
            $aliases = @($service.Aliases)
        }

        if ($service.Name -eq $Name -or $aliases -contains $Name) {
            return $service
        }
    }

    $availableNames = foreach ($service in $serviceDefinitions) {
        $aliases = @()
        if ($service.PSObject.Properties.Name -contains "Aliases" -and $null -ne $service.Aliases) {
            $aliases = @($service.Aliases)
        }

        if ($aliases.Count -gt 0) {
            "{0} ({1})" -f $service.Name, ($aliases -join ", ")
        }
        else {
            $service.Name
        }
    }

    throw "Service definition not found: $Name. Available names: $($availableNames -join '; ')"
}

function Get-ServicePidFilePath {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    return (Join-Path $PidRoot "$($Service.Name).json")
}

function Get-ServiceStdoutLogPath {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    return (Join-Path $LogRoot "$($Service.Name).log")
}

function Get-ServiceStderrLogPath {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    return (Join-Path $LogRoot "$($Service.Name).err.log")
}

function New-ServiceLogPaths {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    return [pscustomobject]@{
        Stdout = (Join-Path $LogRoot "$($Service.Name)-$stamp.log")
        Stderr = (Join-Path $LogRoot "$($Service.Name)-$stamp.err.log")
    }
}

function Read-ServicePidInfo {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    $path = Get-ServicePidFilePath -Service $Service
    if (-not (Test-Path -LiteralPath $path)) {
        return $null
    }

    return (Get-Content -LiteralPath $path -Encoding UTF8 | ConvertFrom-Json)
}

function Get-LatestServiceLogPath {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service,

        [Parameter(Mandatory = $true)]
        [ValidateSet("stdout", "stderr")]
        [string]$Stream
    )

    $pidInfo = Read-ServicePidInfo -Service $Service
    if ($null -ne $pidInfo) {
        $managedPath = if ($Stream -eq "stdout") { [string]$pidInfo.StdoutLogPath } else { [string]$pidInfo.StderrLogPath }
        if (-not [string]::IsNullOrWhiteSpace($managedPath) -and (Test-Path -LiteralPath $managedPath)) {
            return $managedPath
        }
    }

    if ($Stream -eq "stdout") {
        $candidates = @(Get-ChildItem -LiteralPath $LogRoot -Filter "$($Service.Name)-*.log" -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -notlike "*.err.log" } |
            Sort-Object LastWriteTime -Descending)
    }
    else {
        $candidates = @(Get-ChildItem -LiteralPath $LogRoot -Filter "$($Service.Name)-*.err.log" -ErrorAction SilentlyContinue |
            Sort-Object LastWriteTime -Descending)
    }

    if ($null -eq $candidates -or $candidates.Count -eq 0) {
        return $null
    }

    return $candidates[0].FullName
}

function Write-ServicePidInfo {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service,

        [Parameter(Mandatory = $true)]
        [System.Diagnostics.Process]$Process,

        [Parameter(Mandatory = $true)]
        [string]$StdoutLogPath,

        [Parameter(Mandatory = $true)]
        [string]$StderrLogPath
    )

    $payload = [pscustomobject]@{
        Name          = $Service.Name
        Pid           = [int]$Process.Id
        Port          = [int]$Service.Port
        StartedAt     = (Get-Date).ToString("o")
        BinaryPath    = $Service.BinaryPath
        StdoutLogPath = $StdoutLogPath
        StderrLogPath = $StderrLogPath
    }

    $path = Get-ServicePidFilePath -Service $Service
    $payload | ConvertTo-Json | Set-Content -LiteralPath $path -Encoding UTF8
}

function Remove-ServicePidInfo {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    $path = Get-ServicePidFilePath -Service $Service
    if (Test-Path -LiteralPath $path) {
        Remove-Item -LiteralPath $path -Force
    }
}

function Test-ProcessAlive {
    param(
        [Parameter(Mandatory = $true)]
        [int]$ProcessId
    )

    return ($null -ne (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue))
}

function Get-ListeningProcessId {
    param(
        [Parameter(Mandatory = $true)]
        [int]$Port
    )

    try {
        $connection = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction Stop |
            Sort-Object -Property OwningProcess |
            Select-Object -First 1
        if ($null -ne $connection) {
            return [int]$connection.OwningProcess
        }
    }
    catch {
    }

    $pattern = "^\s*TCP\s+\S+:$Port\s+\S+\s+LISTENING\s+(\d+)\s*$"
    foreach ($line in (netstat -ano -p tcp)) {
        if ($line -match $pattern) {
            return [int]$matches[1]
        }
    }

    return $null
}

function Test-TcpPort {
    param(
        [Parameter(Mandatory = $true)]
        [int]$Port,

        [string]$HostName = "127.0.0.1",

        [int]$TimeoutMs = 500
    )

    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $asyncResult = $client.BeginConnect($HostName, $Port, $null, $null)
        if (-not $asyncResult.AsyncWaitHandle.WaitOne($TimeoutMs, $false)) {
            return $false
        }

        $client.EndConnect($asyncResult)
        return $true
    }
    catch {
        return $false
    }
    finally {
        $client.Close()
    }
}

function Test-HttpEndpoint {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Url,

        [int]$TimeoutSec = 2
    )

    try {
        $response = Invoke-WebRequest -Uri $Url -Method Get -TimeoutSec $TimeoutSec -UseBasicParsing
        return ($response.StatusCode -ge 200 -and $response.StatusCode -lt 300)
    }
    catch {
        return $false
    }
}

function Test-ServiceHealthy {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    switch ($Service.ProbeType) {
        "http" {
            return (Test-HttpEndpoint -Url $Service.ProbeTarget)
        }
        default {
            return (Test-TcpPort -Port $Service.Port)
        }
    }
}

function Wait-ServiceReady {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service,

        [int]$TimeoutSec,

        [int]$ProcessId
    )

    $effectiveTimeout = $TimeoutSec
    if ($effectiveTimeout -le 0) {
        $effectiveTimeout = [int]$Service.StartTimeoutSec
    }

    $deadline = (Get-Date).AddSeconds($effectiveTimeout)
    while ((Get-Date) -lt $deadline) {
        if ($ProcessId -gt 0 -and -not (Test-ProcessAlive -ProcessId $ProcessId)) {
            return $false
        }

        if (Test-ServiceHealthy -Service $Service) {
            return $true
        }

        Start-Sleep -Seconds 1
    }

    return $false
}

function Get-ServiceStatus {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    $pidInfo = Read-ServicePidInfo -Service $Service
    $managedPid = $null
    $hasStalePid = $false
    $stdoutLogPath = Get-ServiceStdoutLogPath -Service $Service
    $stderrLogPath = Get-ServiceStderrLogPath -Service $Service
    if ($null -ne $pidInfo) {
        $managedPid = [int]$pidInfo.Pid
        if (-not [string]::IsNullOrWhiteSpace($pidInfo.StdoutLogPath)) {
            $stdoutLogPath = [string]$pidInfo.StdoutLogPath
        }
        if (-not [string]::IsNullOrWhiteSpace($pidInfo.StderrLogPath)) {
            $stderrLogPath = [string]$pidInfo.StderrLogPath
        }
        if (-not (Test-ProcessAlive -ProcessId $managedPid)) {
            $hasStalePid = $true
            $managedPid = $null
        }
    }

    $healthy = Test-ServiceHealthy -Service $Service
    $listenerPid = Get-ListeningProcessId -Port $Service.Port

    if ($null -ne $managedPid) {
        if ($healthy) {
            return [pscustomobject]@{
                State        = "managed"
                Summary      = "managed-running"
                Pid          = $managedPid
                Port         = $Service.Port
                HasStalePid  = $false
                StdoutLog    = $stdoutLogPath
                StderrLog    = $stderrLogPath
            }
        }

        return [pscustomobject]@{
            State        = "managed_unhealthy"
            Summary      = "managed-not-ready"
            Pid          = $managedPid
            Port         = $Service.Port
            HasStalePid  = $false
            StdoutLog    = $stdoutLogPath
            StderrLog    = $stderrLogPath
        }
    }

    if ($healthy) {
        return [pscustomobject]@{
            State        = "external"
            Summary      = "external-running"
            Pid          = $listenerPid
            Port         = $Service.Port
            HasStalePid  = $hasStalePid
            StdoutLog    = $null
            StderrLog    = $null
        }
    }

    if ($null -ne $listenerPid) {
        return [pscustomobject]@{
            State        = "conflict"
            Summary      = "port-conflict"
            Pid          = $listenerPid
            Port         = $Service.Port
            HasStalePid  = $hasStalePid
            StdoutLog    = $null
            StderrLog    = $null
        }
    }

    if ($hasStalePid) {
        return [pscustomobject]@{
            State        = "stale"
            Summary      = "stale-pid-file"
            Pid          = $null
            Port         = $Service.Port
            HasStalePid  = $true
            StdoutLog    = $stdoutLogPath
            StderrLog    = $stderrLogPath
        }
    }

    return [pscustomobject]@{
        State        = "stopped"
        Summary      = "stopped"
        Pid          = $null
        Port         = $Service.Port
        HasStalePid  = $false
        StdoutLog    = $stdoutLogPath
        StderrLog    = $stderrLogPath
    }
}

function Build-ServiceBinary {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    Initialize-DevState
    Invoke-ExternalCommand -FilePath "go" -Arguments @(
        "build",
        "-o",
        $Service.BinaryPath,
        $Service.Package
    ) -WorkingDirectory $BackendRoot
}

function Start-ServiceProcess {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    $logPaths = New-ServiceLogPaths -Service $Service
    $stdoutLog = $logPaths.Stdout
    $stderrLog = $logPaths.Stderr

    $process = Start-Process -FilePath $Service.BinaryPath `
        -WorkingDirectory $BackendRoot `
        -WindowStyle Hidden `
        -RedirectStandardOutput $stdoutLog `
        -RedirectStandardError $stderrLog `
        -PassThru

    Write-ServicePidInfo -Service $Service -Process $process -StdoutLogPath $stdoutLog -StderrLogPath $stderrLog
    if (-not (Wait-ServiceReady -Service $Service -TimeoutSec $Service.StartTimeoutSec -ProcessId $process.Id)) {
        if (Test-ProcessAlive -ProcessId $process.Id) {
            Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        }
        Remove-ServicePidInfo -Service $Service
        throw "Service start failed: $($Service.Name). Check logs: $stdoutLog and $stderrLog"
    }

    return $process
}

function Stop-ServiceProcess {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    $pidInfo = Read-ServicePidInfo -Service $Service
    if ($null -eq $pidInfo) {
        return [pscustomobject]@{
            Name    = $Service.Name
            Action  = "skip"
            Message = "no managed process"
        }
    }

    $managedProcessId = [int]$pidInfo.Pid
    if (Test-ProcessAlive -ProcessId $managedProcessId) {
        try {
            Stop-Process -Id $managedProcessId -ErrorAction Stop
        }
        catch {
            Stop-Process -Id $managedProcessId -Force -ErrorAction SilentlyContinue
        }

        $deadline = (Get-Date).AddSeconds(15)
        while ((Get-Date) -lt $deadline -and (Test-ProcessAlive -ProcessId $managedProcessId)) {
            Start-Sleep -Milliseconds 500
        }

        if (Test-ProcessAlive -ProcessId $managedProcessId) {
            Stop-Process -Id $managedProcessId -Force -ErrorAction SilentlyContinue
        }
    }

    Remove-ServicePidInfo -Service $Service
    return [pscustomobject]@{
        Name    = $Service.Name
        Action  = "stopped"
        Message = "managed process stopped"
    }
}

function Ensure-ServiceStarted {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    $status = Get-ServiceStatus -Service $Service
    if ($status.HasStalePid) {
        Remove-ServicePidInfo -Service $Service
        $status = Get-ServiceStatus -Service $Service
    }

    switch ($status.State) {
        "managed" {
            return [pscustomobject]@{
                Name   = $Service.Name
                Port   = $Service.Port
                Action = "skip"
                Detail = "managed and healthy"
            }
        }
        "external" {
            return [pscustomobject]@{
                Name   = $Service.Name
                Port   = $Service.Port
                Action = "skip"
                Detail = "external process already healthy"
            }
        }
        "managed_unhealthy" {
            if (Wait-ServiceReady -Service $Service -TimeoutSec 15 -ProcessId $status.Pid) {
                return [pscustomobject]@{
                    Name   = $Service.Name
                    Port   = $Service.Port
                    Action = "skip"
                    Detail = "existing managed process became healthy"
                }
            }

            throw "Service $($Service.Name) already has a managed process, but it is still not healthy after waiting. Check log: $($status.StdoutLog)"
        }
        "conflict" {
            throw "Service $($Service.Name) port $($Service.Port) is occupied by process $($status.Pid), but the health check failed. Resolve the conflict first."
        }
    }

    Assert-ServiceDependenciesReady -Service $Service
    Build-ServiceBinary -Service $Service
    $process = Start-ServiceProcess -Service $Service
    return [pscustomobject]@{
        Name   = $Service.Name
        Port   = $Service.Port
        Action = "start"
        Detail = "started, PID=$($process.Id)"
    }
}

function Restart-BackendService {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    $status = Get-ServiceStatus -Service $Service
    if ($status.HasStalePid) {
        Remove-ServicePidInfo -Service $Service
        $status = Get-ServiceStatus -Service $Service
    }

    switch ($status.State) {
        "external" {
            throw "Service $($Service.Name) is managed by an external process. Refuse to restart it automatically."
        }
        "conflict" {
            throw "Service $($Service.Name) port $($Service.Port) is occupied by process $($status.Pid), but the health check failed. Resolve the conflict first."
        }
        "managed" {
            Stop-ServiceProcess -Service $Service | Out-Null
        }
        "managed_unhealthy" {
            Stop-ServiceProcess -Service $Service | Out-Null
        }
    }

    Assert-ServiceDependenciesReady -Service $Service
    Build-ServiceBinary -Service $Service
    $process = Start-ServiceProcess -Service $Service
    return [pscustomobject]@{
        Name   = $Service.Name
        Port   = $Service.Port
        Action = "restart"
        Detail = "restarted, PID=$($process.Id)"
    }
}

function Get-ContainerStatus {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ContainerName
    )

    $status = cmd.exe /d /c "docker inspect --format ""{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}"" $ContainerName 2>nul"
    if ($LASTEXITCODE -ne 0) {
        return "unavailable"
    }

    return (($status | Select-Object -First 1).Trim())
}

function Wait-ContainerStatus {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$ContainerDefinition
    )

    $deadline = (Get-Date).AddSeconds([int]$ContainerDefinition.TimeoutSec)
    while ((Get-Date) -lt $deadline) {
        $status = Get-ContainerStatus -ContainerName $ContainerDefinition.Container
        if ($status -eq "healthy") {
            return
        }

        Start-Sleep -Seconds 2
    }

        throw "Container did not become healthy in time: $($ContainerDefinition.Name)"
}

function Start-BackendInfrastructure {
    Assert-ToolExists -Name "docker"
    if (-not (Test-Path -LiteralPath $ComposeFile)) {
        throw "docker-compose.yml not found: $ComposeFile"
    }

    $composeServices = Get-InfrastructureComposeServices
    Invoke-ExternalCommand -FilePath "docker" -Arguments (@(
        "compose",
        "-f",
        $ComposeFile,
        "up",
        "-d"
    ) + $composeServices) -WorkingDirectory $RepoRoot

    foreach ($definition in (Get-InfrastructureDefinitions)) {
        Wait-ContainerStatus -ContainerDefinition $definition
    }

    Ensure-KafkaTopics
}

function Ensure-KafkaTopics {
    Assert-ToolExists -Name "docker"

    foreach ($topic in (Get-KafkaTopicDefinitions)) {
        $arguments = @(
            "exec",
            "smartflow-kafka",
            "/opt/kafka/bin/kafka-topics.sh",
            "--bootstrap-server",
            "localhost:9092",
            "--create",
            "--if-not-exists",
            "--topic",
            $topic,
            "--partitions",
            "3",
            "--replication-factor",
            "1"
        )
        Invoke-ExternalCommand -FilePath "docker" -Arguments $arguments -WorkingDirectory $RepoRoot
    }
}

function Stop-BackendInfrastructure {
    Assert-ToolExists -Name "docker"
    $composeServices = Get-InfrastructureComposeServices
    Invoke-ExternalCommand -FilePath "docker" -Arguments (@(
        "compose",
        "-f",
        $ComposeFile,
        "stop"
    ) + $composeServices) -WorkingDirectory $RepoRoot
}

function Get-InfrastructureStatus {
    $rows = @()
    foreach ($definition in (Get-InfrastructureDefinitions)) {
        $rows += [pscustomobject]@{
            Name      = $definition.Name
            Container = $definition.Container
            Status    = (Get-ContainerStatus -ContainerName $definition.Container)
        }
    }
    return $rows
}

function Assert-ServiceDependenciesReady {
    param(
        [Parameter(Mandatory = $true)]
        [pscustomobject]$Service
    )

    foreach ($dependencyName in $Service.Dependencies) {
        $dependency = Get-BackendServiceDefinition -Name $dependencyName
        $status = Get-ServiceStatus -Service $dependency
        if ($status.State -notin @("managed", "external")) {
            throw "Dependency not ready for service $($Service.Name): $dependencyName (current state: $($status.Summary))"
        }
    }
}
