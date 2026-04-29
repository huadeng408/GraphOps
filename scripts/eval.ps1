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

function New-ReleaseRegressionContext {
    param([int]$VariantIndex)

    $suffix = if ($VariantIndex -gt 0) { "-{0:d2}" -f $VariantIndex } else { "" }
    return @{
        cluster          = "prod-cn"
        namespace        = "checkout"
        environment      = "production"
        alert_name       = "OrderApiHigh5xxAfterRelease"
        alert_started_at = "2026-04-17T02:02:00Z"
        release_id       = "deploy-2026.04.17-0155$suffix"
        release_version  = "order-api@2026.04.17-0155$suffix"
        previous_version = "order-api@2026.04.17-0142$suffix"
        labels           = @{
            service = "order-api"
            team    = "payments"
        }
    }
}

function New-ScenarioCatalog {
    $items = @()
    foreach ($i in 1..18) {
        $items += [pscustomobject]@{
            playbook_key    = ("release_config_regression_{0:d2}" -f $i)
            variant_index   = $i
            service_name    = "order-api"
            severity        = "P1"
            alert_summary   = "5xx spike after deploy"
            expected_action = "rollback"
        }
    }
    return $items
}

function Invoke-Scenario {
    param([pscustomobject]$Scenario)

    $bodyObject = @{
        service_name  = $Scenario.service_name
        severity      = $Scenario.severity
        alert_summary = $Scenario.alert_summary
        playbook_key  = $Scenario.playbook_key
        context       = New-ReleaseRegressionContext -VariantIndex $Scenario.variant_index
    }
    $body = $bodyObject | ConvertTo-Json -Depth 6

    $incident = Invoke-RestMethod -Uri "$IncidentApiBase/incidents" -Method Post -ContentType "application/json" -Body $body

    $timer = [System.Diagnostics.Stopwatch]::StartNew()
    $run = Invoke-RestMethod -Uri ("$OrchestratorBase/runs/incidents/{0}" -f $incident.id) -Method Post
    $timer.Stop()
    $initialLatencyMs = $timer.Elapsed.TotalMilliseconds

    $predictedAction = if ($null -ne $run.proposed_action) { $run.proposed_action.action_type } else { "none" }
    $finalStatus = $run.status
    $verifyStatus = $null
    $endToEndLatencyMs = $initialLatencyMs

    if ($ApproveRollback -and $run.interrupt) {
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
        playbook_key       = $Scenario.playbook_key
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

$summary = [ordered]@{
    total_cases                     = $results.Count
    action_accuracy                 = [math]::Round((($results | Where-Object { $_.action_match }).Count / [math]::Max($results.Count, 1)), 4)
    rollback_recovered_rate         = [math]::Round((($results | Where-Object { $_.verify_status -eq "recovered" }).Count / [math]::Max($results.Count, 1)), 4)
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
