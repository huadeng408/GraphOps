param(
    [switch]$ResetData
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot

cmd /c "docker info >nul 2>nul"
if ($LASTEXITCODE -ne 0) {
    throw "Docker daemon is not available. Start Docker Desktop before running mysql-up.ps1."
}

if ($ResetData) {
    docker compose -f "$root\compose.yaml" down -v --remove-orphans | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to reset mysql docker resources."
    }
}

docker compose -f "$root\compose.yaml" up -d mysql | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "Failed to start mysql container. Check Docker Desktop / daemon status."
}

$deadline = (Get-Date).AddMinutes(2)
while ((Get-Date) -lt $deadline) {
    cmd /c "docker compose -f ""$root\compose.yaml"" exec -T mysql sh -lc ""MYSQL_PWD=graphops_root mysqladmin ping -h 127.0.0.1 -uroot > /dev/null 2>&1"""
    if ($LASTEXITCODE -eq 0) {
        Write-Output "mysql is ready"
        exit 0
    }
    Start-Sleep -Seconds 2
}

throw "Timed out waiting for mysql container."
