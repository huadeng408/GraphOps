param(
    [ValidateSet("main", "secondary", "both")]
    [string]$Scenario = "main",
    [ValidateRange(0, 18)]
    [int]$VariantIndex = 0,
    [switch]$ApproveMain
)

$ErrorActionPreference = "Stop"

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

function New-DownstreamInventoryContext {
    return @{
        cluster          = "prod-cn"
        namespace        = "checkout"
        environment      = "production"
        alert_name       = "OrderApiTimeoutsToInventory"
        alert_started_at = "2026-04-17T03:14:00Z"
        release_id       = "deploy-2026.04.16-2210"
        release_version  = "order-api@2026.04.16-2210"
        previous_version = "order-api@2026.04.16-2150"
        labels           = @{
            service = "order-api"
            team    = "payments"
        }
    }
}

function Get-PlaybookKey {
    param([int]$VariantIndex)

    if ($VariantIndex -gt 0) {
        return "release_config_regression_{0:d2}" -f $VariantIndex
    }
    return "release_config_regression"
}

function New-ScenarioCatalog {
    $catalog = New-Object System.Collections.Generic.List[object]

    if ($Scenario -in @("main", "both")) {
        $catalog.Add([pscustomobject]@{
            name          = "main"
            playbook_key  = Get-PlaybookKey -VariantIndex $VariantIndex
            service_name  = "order-api"
            severity      = "P1"
            alert_summary = "5xx spike after deploy"
            context       = New-ReleaseRegressionContext -VariantIndex $VariantIndex
            auto_approve  = [bool]$ApproveMain
        })
    }

    if ($Scenario -in @("secondary", "both")) {
        $catalog.Add([pscustomobject]@{
            name          = "secondary"
            playbook_key  = "downstream_inventory_outage"
            service_name  = "order-api"
            severity      = "P2"
            alert_summary = "timeouts to inventory"
            context       = New-DownstreamInventoryContext
            auto_approve  = $false
        })
    }

    return $catalog
}

function Invoke-Scenario {
    param(
        [pscustomobject]$ScenarioItem
    )

    $body = @{
        service_name  = $ScenarioItem.service_name
        severity      = $ScenarioItem.severity
        alert_summary = $ScenarioItem.alert_summary
        playbook_key  = $ScenarioItem.playbook_key
        context       = $ScenarioItem.context
    } | ConvertTo-Json -Depth 6

    $incident = Invoke-RestMethod -Uri "http://127.0.0.1:8082/incidents" -Method Post -ContentType "application/json" -Body $body
    $run = Invoke-RestMethod -Uri ("http://127.0.0.1:8090/runs/incidents/{0}" -f $incident.id) -Method Post
    $verifyStatus = $null

    if ($ScenarioItem.auto_approve -and $run.interrupt) {
        $resumeBody = @{
            approved = $true
            reviewer = "oncall"
            comment  = "rollback approved from replay script"
        } | ConvertTo-Json

        $resumed = Invoke-RestMethod -Uri ("http://127.0.0.1:8090/runs/incidents/{0}/resume" -f $incident.id) -Method Post -ContentType "application/json" -Body $resumeBody
        if ($null -ne $resumed.verification_result) {
            $verifyStatus = $resumed.verification_result.status
        }
    }

    $finalIncident = Invoke-RestMethod -Uri ("http://127.0.0.1:8082/incidents/{0}" -f $incident.id) -Method Get
    $hasAction = $false
    if ($null -ne $finalIncident.analysis -and $null -ne $finalIncident.analysis.proposed_action) {
        $hasAction = $true
    }

    return [pscustomobject]@{
        scenario       = $ScenarioItem.name
        incident       = $incident.id
        playbook_key   = $ScenarioItem.playbook_key
        initial_status = $run.status
        final_status   = $finalIncident.status
        verify_status  = $verifyStatus
        has_action     = $hasAction
    }
}

$results = @()
foreach ($scenarioItem in (New-ScenarioCatalog)) {
    $results += Invoke-Scenario -ScenarioItem $scenarioItem
}

$results | ConvertTo-Json -Depth 6
