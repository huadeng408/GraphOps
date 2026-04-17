$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
cmd /c "docker info >nul 2>nul"
if ($LASTEXITCODE -ne 0) {
    throw "Docker daemon is not available. Start Docker Desktop before running mysql-down.ps1."
}

docker compose -f "$root\compose.yaml" down --remove-orphans
if ($LASTEXITCODE -ne 0) {
    throw "Failed to stop mysql docker resources."
}
