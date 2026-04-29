param(
    [switch]$UseMySQL
)

$ErrorActionPreference = "Stop"

function Wait-Port {
    param(
        [int]$Port,
        [int]$TimeoutSeconds = 20
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        $listening = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
        if ($listening) {
            return
        }
        Start-Sleep -Milliseconds 300
    }

    throw "Timed out waiting for port $Port."
}

function Wait-HttpOk {
    param(
        [string]$Uri,
        [int]$TimeoutSeconds = 20
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-RestMethod -Uri $Uri -Method Get -TimeoutSec 3
            if ($response.status -eq "ok") {
                return
            }
        } catch {
        }
        Start-Sleep -Milliseconds 300
    }

    throw "Timed out waiting for HTTP endpoint $Uri."
}

$root = Split-Path -Parent $PSScriptRoot
$logs = Join-Path $root "logs"
New-Item -ItemType Directory -Force -Path $logs | Out-Null

$ports = 8082, 8085, 8090
foreach ($port in $ports) {
    Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue |
        Select-Object -ExpandProperty OwningProcess -Unique |
        ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
}

$incidentStore = "memory"
$receiptStore = "memory"
if ($UseMySQL) {
    if (-not $env:MYSQL_DSN) {
        $env:MYSQL_DSN = "graphops:graphops@tcp(127.0.0.1:3307)/graphops?parseTime=true"
    }
    & (Join-Path $PSScriptRoot "mysql-up.ps1")
    & (Join-Path $PSScriptRoot "mysql-migrate.ps1")
    $incidentStore = "mysql"
    $receiptStore = "mysql"
}

$incident = Start-Process -FilePath "powershell.exe" -ArgumentList @(
    "-NoProfile",
    "-Command",
    "`$env:INCIDENT_API_ADDR=':8082'; `$env:INCIDENT_STORE='$incidentStore'; if (`$env:MYSQL_DSN) { `$env:MYSQL_DSN=`$env:MYSQL_DSN }; Set-Location '$root'; go run ./cmd/incident-api"
) -RedirectStandardOutput (Join-Path $logs "incident-api.out.log") `
  -RedirectStandardError (Join-Path $logs "incident-api.err.log") `
  -PassThru

$gateway = Start-Process -FilePath "powershell.exe" -ArgumentList @(
    "-NoProfile",
    "-Command",
    "`$env:OPS_GATEWAY_ADDR=':8085'; `$env:ACTION_RECEIPT_STORE='$receiptStore'; if (`$env:MYSQL_DSN) { `$env:MYSQL_DSN=`$env:MYSQL_DSN }; if (`$env:REDIS_URL) { `$env:REDIS_URL=`$env:REDIS_URL }; Set-Location '$root'; go run ./cmd/ops-gateway"
) -RedirectStandardOutput (Join-Path $logs "ops-gateway.out.log") `
  -RedirectStandardError (Join-Path $logs "ops-gateway.err.log") `
  -PassThru

$orchestrator = Start-Process -FilePath "powershell.exe" -ArgumentList @(
    "-NoProfile",
    "-Command",
    "`$env:INCIDENT_API_URL='http://127.0.0.1:8082'; `$env:OPS_GATEWAY_URL='http://127.0.0.1:8085'; `$env:CHECKPOINTER_BACKEND='sqlite'; `$env:CHECKPOINTER_SQLITE_PATH='$root\orchestrator\data\langgraph.sqlite'; if (`$env:REDIS_URL) { `$env:REDIS_URL=`$env:REDIS_URL }; if (-not `$env:REASONER_PROVIDER) { `$env:REASONER_PROVIDER='ollama' }; if (-not `$env:OLLAMA_MAIN_MODEL) { if (`$env:OLLAMA_MODEL) { `$env:OLLAMA_MAIN_MODEL=`$env:OLLAMA_MODEL } else { `$env:OLLAMA_MAIN_MODEL='qwen3:4b' } }; if (-not `$env:OLLAMA_PARALLEL_MODEL) { `$env:OLLAMA_PARALLEL_MODEL='qwen3:1.7b' }; if (-not `$env:OLLAMA_NUM_CTX) { `$env:OLLAMA_NUM_CTX='8192' }; if (-not `$env:OLLAMA_PARALLEL_NUM_CTX) { `$env:OLLAMA_PARALLEL_NUM_CTX=`$env:OLLAMA_NUM_CTX }; Set-Location '$root\orchestrator'; python -m uvicorn graphops_orchestrator.app:app --host 127.0.0.1 --port 8090"
) -RedirectStandardOutput (Join-Path $logs "orchestrator.out.log") `
  -RedirectStandardError (Join-Path $logs "orchestrator.err.log") `
  -PassThru

Wait-Port -Port 8082
Wait-Port -Port 8085
Wait-Port -Port 8090
Wait-HttpOk -Uri "http://127.0.0.1:8090/healthz"

[pscustomobject]@{
    incident_api_pid = $incident.Id
    ops_gateway_pid  = $gateway.Id
    orchestrator_pid = $orchestrator.Id
    incident_api     = "http://127.0.0.1:8082"
    ops_gateway      = "http://127.0.0.1:8085"
    orchestrator     = "http://127.0.0.1:8090"
    logs             = $logs
} | Format-List
