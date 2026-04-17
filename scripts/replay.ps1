param(
    [ValidateSet("main", "secondary", "both")]
    [string]$Scenario = "both",
    [switch]$ApproveMain
)

$ErrorActionPreference = "Stop"

function Invoke-MainScenario {
    $body = @{
        service_name  = "order-api"
        severity      = "P1"
        alert_summary = "5xx spike after deploy"
        scenario_key  = "release_config_regression"
    } | ConvertTo-Json

    $incident = Invoke-RestMethod -Uri "http://127.0.0.1:8082/incidents" -Method Post -ContentType "application/json" -Body $body
    $run = Invoke-RestMethod -Uri ("http://127.0.0.1:8090/runs/incidents/{0}" -f $incident.id) -Method Post

    if ($ApproveMain -and $run.interrupt) {
        $resumeBody = @{
            approved = $true
            reviewer = "oncall"
            comment  = "rollback approved from replay script"
        } | ConvertTo-Json

        $resumed = Invoke-RestMethod -Uri ("http://127.0.0.1:8090/runs/incidents/{0}/resume" -f $incident.id) -Method Post -ContentType "application/json" -Body $resumeBody
        return [pscustomobject]@{
            scenario = "main"
            incident = $incident.id
            initial  = $run.status
            final    = $resumed.status
            verify   = $resumed.verification_result.status
        }
    }

    return [pscustomobject]@{
        scenario = "main"
        incident = $incident.id
        initial  = $run.status
        final    = $null
        verify   = $null
    }
}

function Invoke-SecondaryScenario {
    $body = @{
        service_name  = "order-api"
        severity      = "P1"
        alert_summary = "timeouts to inventory"
        scenario_key  = "downstream_inventory_outage"
    } | ConvertTo-Json

    $incident = Invoke-RestMethod -Uri "http://127.0.0.1:8082/incidents" -Method Post -ContentType "application/json" -Body $body
    $run = Invoke-RestMethod -Uri ("http://127.0.0.1:8090/runs/incidents/{0}" -f $incident.id) -Method Post

    return [pscustomobject]@{
        scenario   = "secondary"
        incident   = $incident.id
        status     = $run.status
        has_action = ($null -ne $run.proposed_action)
    }
}

$results = @()
if ($Scenario -in @("main", "both")) {
    $results += Invoke-MainScenario
}
if ($Scenario -in @("secondary", "both")) {
    $results += Invoke-SecondaryScenario
}

$results | ConvertTo-Json -Depth 6
