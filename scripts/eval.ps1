param(
    [string]$IncidentApiBase = "http://127.0.0.1:8082",
    [string]$OrchestratorBase = "http://127.0.0.1:8090",
    [string]$OutputPath = "",
    [switch]$ApproveRollback
)

$ErrorActionPreference = "Stop"

function Get-Median {
    param([double[]]$Values)

    if (-not $Values -or $Values.Count -eq 0) {
        return 0
    }

    $sorted = $Values | Sort-Object
    $count = $sorted.Count
    if ($count % 2 -eq 1) {
        return [double]$sorted[[int]($count / 2)]
    }

    $left = [double]$sorted[($count / 2) - 1]
    $right = [double]$sorted[$count / 2]
    return ($left + $right) / 2
}

function New-ScenarioCatalog {
    $items = @()
    foreach ($i in 1..18) {
        $items += [pscustomobject]@{
            scenario_key    = ("release_config_regression_{0:d2}" -f $i)
            service_name    = "order-api"
            severity        = "P1"
            alert_summary   = "5xx spike after deploy"
            expected_action = "rollback"
        }
    }
    foreach ($i in 1..18) {
        $items += [pscustomobject]@{
            scenario_key    = ("downstream_inventory_outage_{0:d2}" -f $i)
            service_name    = "order-api"
            severity        = "P1"
            alert_summary   = "timeouts to inventory"
            expected_action = "none"
        }
    }
    return $items
}

function Invoke-Scenario {
    param([pscustomobject]$Scenario)

    $body = @{
        service_name  = $Scenario.service_name
        severity      = $Scenario.severity
        alert_summary = $Scenario.alert_summary
        scenario_key  = $Scenario.scenario_key
    } | ConvertTo-Json

    $incident = Invoke-RestMethod -Uri "$IncidentApiBase/incidents" -Method Post -ContentType "application/json" -Body $body

    $timer = [System.Diagnostics.Stopwatch]::StartNew()
    $run = Invoke-RestMethod -Uri ("$OrchestratorBase/runs/incidents/{0}" -f $incident.id) -Method Post
    $timer.Stop()
    $initialLatencyMs = $timer.Elapsed.TotalMilliseconds

    $predictedAction = if ($null -ne $run.proposed_action) { $run.proposed_action.action_type } else { "none" }
    $finalStatus = $run.status
    $verifyStatus = $null
    $endToEndLatencyMs = $initialLatencyMs

    if ($ApproveRollback -and $Scenario.expected_action -eq "rollback" -and $run.interrupt) {
        $resumeTimer = [System.Diagnostics.Stopwatch]::StartNew()
        $resumeBody = @{
            approved = $true
            reviewer = "eval-bot"
            comment  = "automated evaluation approval"
        } | ConvertTo-Json

        $resumed = Invoke-RestMethod -Uri ("$OrchestratorBase/runs/incidents/{0}/resume" -f $incident.id) -Method Post -ContentType "application/json" -Body $resumeBody
        $resumeTimer.Stop()

        $finalStatus = $resumed.status
        $verifyStatus = $resumed.verification_result.status
        $endToEndLatencyMs += $resumeTimer.Elapsed.TotalMilliseconds
    }

    return [pscustomobject]@{
        scenario_key       = $Scenario.scenario_key
        expected_action    = $Scenario.expected_action
        predicted_action   = $predictedAction
        action_match       = ($predictedAction -eq $Scenario.expected_action)
        initial_status     = $run.status
        final_status       = $finalStatus
        verify_status      = $verifyStatus
        initial_latency_ms = [math]::Round($initialLatencyMs, 2)
        end_to_end_ms      = [math]::Round($endToEndLatencyMs, 2)
        incident_id        = $incident.id
    }
}

$catalog = New-ScenarioCatalog
$results = @()
foreach ($scenario in $catalog) {
    $results += Invoke-Scenario -Scenario $scenario
}

$rollbackExpected = $results | Where-Object { $_.expected_action -eq "rollback" }
$noActionExpected = $results | Where-Object { $_.expected_action -eq "none" }
$falseRollbackCount = ($noActionExpected | Where-Object { $_.predicted_action -eq "rollback" }).Count

$summary = [ordered]@{
    total_cases                     = $results.Count
    action_accuracy                 = [math]::Round((($results | Where-Object { $_.action_match }).Count / [math]::Max($results.Count, 1)), 4)
    false_rollback_rate             = [math]::Round(($falseRollbackCount / [math]::Max($noActionExpected.Count, 1)), 4)
    rollback_recovered_rate         = [math]::Round((($rollbackExpected | Where-Object { $_.verify_status -eq "recovered" }).Count / [math]::Max($rollbackExpected.Count, 1)), 4)
    median_initial_latency_ms       = [math]::Round((Get-Median ($results.initial_latency_ms)), 2)
    median_end_to_end_latency_ms    = [math]::Round((Get-Median ($results.end_to_end_ms)), 2)
    waiting_for_approval_case_count = ($results | Where-Object { $_.initial_status -eq "waiting_for_approval" }).Count
}

$report = [ordered]@{
    generated_at = (Get-Date).ToUniversalTime().ToString("o")
    summary      = $summary
    results      = $results
}

$json = $report | ConvertTo-Json -Depth 6
if ($OutputPath -ne "") {
    Set-Content -Path $OutputPath -Value $json -Encoding UTF8
}

$json
