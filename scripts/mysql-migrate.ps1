$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$migration = Join-Path $root "sql\migrations\001_init.sql"

cmd /c "docker info >nul 2>nul"
if ($LASTEXITCODE -ne 0) {
    throw "Docker daemon is not available. Start Docker Desktop before running mysql-migrate.ps1."
}

if (-not (Test-Path -LiteralPath $migration)) {
    throw "Migration file not found: $migration"
}

$sql = Get-Content -LiteralPath $migration -Raw
$sql | docker compose -f "$root\compose.yaml" exec -T mysql sh -lc "MYSQL_PWD=graphops_root mysql -uroot graphops"
if ($LASTEXITCODE -ne 0) {
    throw "Failed to apply mysql migration."
}

Write-Output "mysql migration applied"
