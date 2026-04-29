param(
    [ValidateSet("main", "secondary", "both")]
    [string]$Scenario = "both",
    [bool]$ApproveMain = $true,
    [bool]$UseMySQL = $true,
    [ValidateSet("rules", "ollama")]
    [string]$ReasonerProvider = "rules",
    [string]$OllamaModel = "qwen3:4b",
    [string]$OutputRoot = ".\logs"
)

$ErrorActionPreference = "Stop"

function Test-DockerReady {
    cmd /c "docker info >nul 2>nul"
    return $LASTEXITCODE -eq 0
}

function Wait-DockerReady {
    param(
        [int]$TimeoutSeconds = 120
    )

    if (Test-DockerReady) {
        return
    }

    $dockerDesktop = "C:\Program Files\Docker\Docker\Docker Desktop.exe"
    if (Test-Path -LiteralPath $dockerDesktop) {
        Start-Process -FilePath $dockerDesktop -WindowStyle Hidden
    }

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if (Test-DockerReady) {
            return
        }
        Start-Sleep -Seconds 3
    }

    throw "Docker daemon is not available. Start Docker Desktop and rerun the script."
}

function Wait-HttpJsonField {
    param(
        [string]$Uri,
        [string]$Field,
        [string]$ExpectedValue,
        [int]$TimeoutSeconds = 60
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-RestMethod -Uri $Uri -Method Get -TimeoutSec 3
            if ($response.$Field -eq $ExpectedValue) {
                return
            }
        } catch {
        }
        Start-Sleep -Seconds 2
    }

    throw "Timed out waiting for $Uri to return $Field=$ExpectedValue."
}

function Wait-GrafanaHealth {
    param(
        [int]$TimeoutSeconds = 90
    )

    $pair = "admin:admin"
    $bytes = [System.Text.Encoding]::ASCII.GetBytes($pair)
    $token = [Convert]::ToBase64String($bytes)
    $headers = @{ Authorization = "Basic $token" }

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-RestMethod -Uri "http://127.0.0.1:3000/api/health" -Headers $headers -TimeoutSec 3
            if ($response.database -eq "ok") {
                return
            }
        } catch {
        }
        Start-Sleep -Seconds 2
    }

    throw "Timed out waiting for Grafana health endpoint."
}

function Invoke-PrometheusQuery {
    param(
        [string]$Query
    )

    $encoded = [System.Uri]::EscapeDataString($Query)
    return Invoke-RestMethod -Uri ("http://127.0.0.1:9090/api/v1/query?query={0}" -f $encoded) -Method Get -TimeoutSec 10
}

function Invoke-MySqlQuery {
    param(
        [string]$ComposeFile,
        [string]$Sql
    )

    $Sql | docker compose -f $ComposeFile exec -T mysql sh -lc "MYSQL_PWD=graphops_root mysql -B -uroot graphops"
}

function Save-MySqlExport {
    param(
        [string]$ComposeFile,
        [string]$Sql,
        [string]$Path
    )

    $content = Invoke-MySqlQuery -ComposeFile $ComposeFile -Sql $Sql
    Set-Content -LiteralPath $Path -Value $content -Encoding UTF8
}

function Format-IncidentRunMetricResult {
    param(
        [object]$MetricResponse
    )

    $lines = New-Object System.Collections.Generic.List[string]
    foreach ($item in @($MetricResponse.data.result)) {
        $scenarioType = Localize-Text $item.metric.scenario_type
        $status = Localize-Text $item.metric.status
        $metricValue = $item.value[1]
        $lines.Add("- $scenarioType / $status：$metricValue 次")
    }

    if ($lines.Count -eq 0) {
        $lines.Add("- 暂无数据")
    }
    return $lines
}

function Format-RollbackMetricResult {
    param(
        [object]$MetricResponse
    )

    $lines = New-Object System.Collections.Generic.List[string]
    foreach ($item in @($MetricResponse.data.result)) {
        $result = Localize-Text $item.metric.result
        $metricValue = $item.value[1]
        $lines.Add("- $result：$metricValue 次")
    }

    if ($lines.Count -eq 0) {
        $lines.Add("- 暂无数据")
    }
    return $lines
}

function Format-VerificationMetricResult {
    param(
        [object]$MetricResponse
    )

    $lines = New-Object System.Collections.Generic.List[string]
    foreach ($item in @($MetricResponse.data.result)) {
        $status = Localize-Text $item.metric.status
        $metricValue = $item.value[1]
        $lines.Add("- $status：$metricValue 次")
    }

    if ($lines.Count -eq 0) {
        $lines.Add("- 暂无数据")
    }
    return $lines
}

function Format-NamedMetricResult {
    param(
        [object]$MetricResponse,
        [string]$MetricLabel
    )

    $lines = New-Object System.Collections.Generic.List[string]
    foreach ($item in @($MetricResponse.data.result)) {
        $name = Localize-Text $item.metric.$MetricLabel
        $metricValue = $item.value[1]
        $lines.Add("- $name：$metricValue 次")
    }

    if ($lines.Count -eq 0) {
        $lines.Add("- 暂无数据")
    }
    return $lines
}

function Add-MarkdownTable {
    param(
        [System.Collections.Generic.List[string]]$Lines,
        [object[]]$Rows
    )

    $Lines.Add("| 场景 | 事件 ID | 初始状态 | 最终状态 | 恢复验证 | 是否有动作 |")
    $Lines.Add("| --- | --- | --- | --- | --- | --- |")
    foreach ($row in $Rows) {
        $initial = if ($null -ne $row.initial_status) { Localize-Text $row.initial_status } else { "-" }
        $final = if ($null -ne $row.final_status) { Localize-Text $row.final_status } else { "-" }
        $verify = if ($null -ne $row.verify_status) { Localize-Text $row.verify_status } else { "-" }
        $hasAction = if ($null -ne $row.has_action) { Localize-Text ([string]$row.has_action) } else { "-" }
        $Lines.Add("| $(Localize-Text $row.scenario) | $($row.incident) | $initial | $final | $verify | $hasAction |")
    }
}

function Localize-Text {
    param(
        [AllowNull()]
        [object]$Value
    )

    if ($null -eq $Value) {
        return ""
    }

    $text = [string]$Value
    $map = @{
        "both" = "主场景 + 副场景"
        "main" = "主场景"
        "secondary" = "副场景"
        "rules" = "规则模式"
        "ollama" = "Ollama 模式"
        "True" = "是"
        "False" = "否"
        "waiting_for_approval" = "等待审批"
        "completed" = "已完成"
        "recovered" = "已恢复"
        "diagnosed" = "已完成诊断"
        "report_ready" = "报告已生成"
        "not_recovered" = "未恢复"
        "approved" = "已批准"
        "release_config_regression" = "发布配置回归"
        "downstream_inventory_outage" = "下游依赖故障"
        "release_regression" = "发布配置回归"
        "downstream_dependency" = "下游依赖故障传播"
        "5xx spike after deploy" = "发布后 5xx 激增"
        "timeouts to inventory" = "调用 inventory 超时"
        "rollback" = "回滚"
        "executed" = "已执行"
        "oncall" = "值班人员"
        "order-api released 8 minutes before the alert with a configuration bundle update." = "告警发生前 8 分钟，order-api 刚完成一次配置包更新发布。"
        "Database DSN and pool settings changed in the same release window." = "同一发布窗口内同时修改了数据库 DSN 和连接池配置。"
        "High-frequency errors show invalid connection string and database authentication failures." = "高频错误显示连接串无效，并伴随数据库认证失败。"
        "The error spike starts immediately after the release and stays local to order-api." = "错误激增紧跟发布后出现，且影响范围主要局限在 order-api。"
        "No downstream error amplification detected on inventory-service." = "没有在 inventory-service 上观察到下游级联放大。"
        "payment-service remains healthy; the blast radius is currently limited to order-api." = "payment-service 保持健康，当前影响范围仅限于 order-api。"
        "No relevant order-api change in the last 2 hours." = "最近 2 小时内没有与 order-api 相关的可疑变更。"
        "order-api errors are dominated by timeouts when calling inventory-service." = "order-api 的主要错误模式是调用 inventory-service 时超时。"
        "The top error pattern is upstream timeout rather than local configuration failure." = "最主要的错误模式是上游调用超时，而不是本地配置失败。"
        "inventory-service is degraded with database pool exhaustion; downstream propagation is likely." = "inventory-service 因数据库连接池耗尽而降级，极有可能向上游传播。"
        "inventory-service depends on a saturated database connection pool." = "inventory-service 依赖的数据库连接池已经饱和。"
        "The latest release introduced a configuration regression in order-api database connectivity." = "最新一次发布在 order-api 的数据库连接配置上引入了回归。"
        "The error spike is local to order-api and started immediately after the release window." = "错误激增发生在发布窗口之后，并且主要是 order-api 自身故障。"
        "The primary fault is likely in inventory-service and is propagating to the caller." = "主要故障大概率位于 inventory-service，并向调用方传播。"
        "order-api is acting as the symptom carrier instead of the fault owner." = "order-api 更像是故障症状的承载者，而不是根故障本身。"
        "Recent change timing and local database errors indicate a release regression; rollback is the safest first action." = "最近变更时间点和本地数据库错误都指向发布回归，回滚是当前最安全的首选动作。"
        "Execute rollback for order-api." = "对 order-api 执行回滚。"
        "Do not rollback. Escalate to the downstream owner and continue manual investigation." = "不要回滚，升级给下游服务负责人并继续人工排查。"
        "Recovered: 5xx dropped to 0.3% and P95 to 118ms." = "已恢复：5xx 降至 0.3%，P95 延迟降至 118ms。"
        "No recovery action was executed. Diagnostic report only." = "未执行恢复动作，本次仅输出诊断报告。"
    }

    if ($map.ContainsKey($text)) {
        return $map[$text]
    }

    return $text
}

$root = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $root "compose.yaml"
$outputDir = Join-Path $root $OutputRoot
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$runDir = Join-Path $outputDir ("full-flow-{0}" -f $timestamp)
New-Item -ItemType Directory -Force -Path $runDir | Out-Null

Wait-DockerReady

$composeServices = @("redis", "prometheus", "grafana")
if ($UseMySQL) {
    $composeServices += "mysql"
}
$composeServiceArgs = $composeServices -join " "
cmd /c ('docker compose -f "{0}" up -d {1} >nul 2>nul' -f $composeFile, $composeServiceArgs)
if ($LASTEXITCODE -ne 0) {
    throw "Failed to start docker compose dependencies."
}

$env:REDIS_URL = "redis://127.0.0.1:6379/0"
$env:REASONER_PROVIDER = $ReasonerProvider
if ($ReasonerProvider -eq "ollama") {
    $env:OLLAMA_MODEL = $OllamaModel
}

$startupLog = Join-Path $runDir "startup.txt"
$devArgs = @{}
if ($UseMySQL) {
    $devArgs["UseMySQL"] = $true
}
$startupText = (& (Join-Path $PSScriptRoot "dev.ps1") @devArgs | Out-String).Trim()
Set-Content -LiteralPath $startupLog -Value $startupText -Encoding UTF8

Wait-HttpJsonField -Uri "http://127.0.0.1:8090/healthz" -Field "status" -ExpectedValue "ok"
Wait-GrafanaHealth

$replayArgs = @{
    Scenario = $Scenario
}
if ($ApproveMain) {
    $replayArgs["ApproveMain"] = $true
}
$replayJson = (& (Join-Path $PSScriptRoot "replay.ps1") @replayArgs | Out-String).Trim()
$replayPath = Join-Path $runDir "replay-result.json"
Set-Content -LiteralPath $replayPath -Value $replayJson -Encoding UTF8
$replayResults = @()
foreach ($item in ($replayJson | ConvertFrom-Json)) {
    $replayResults += $item
}

$incidentBundle = New-Object System.Collections.Generic.List[object]
$incidentIds = @($replayResults | ForEach-Object { $_.incident })
foreach ($replayItem in $replayResults) {
    $incident = Invoke-RestMethod -Uri ("http://127.0.0.1:8082/incidents/{0}" -f $replayItem.incident) -Method Get -TimeoutSec 10
    $incidentPath = Join-Path $runDir ("incident-{0}.json" -f $replayItem.incident)
    Set-Content -LiteralPath $incidentPath -Value ($incident | ConvertTo-Json -Depth 30) -Encoding UTF8

    $events = Invoke-RestMethod -Uri ("http://127.0.0.1:8082/incidents/{0}/events" -f $replayItem.incident) -Method Get -TimeoutSec 10
    $eventsPath = Join-Path $runDir ("events-{0}.json" -f $replayItem.incident)
    Set-Content -LiteralPath $eventsPath -Value ($events | ConvertTo-Json -Depth 30) -Encoding UTF8

    $agentRuns = Invoke-RestMethod -Uri ("http://127.0.0.1:8082/incidents/{0}/agent-runs" -f $replayItem.incident) -Method Get -TimeoutSec 10
    $agentRunsPath = Join-Path $runDir ("agent-runs-{0}.json" -f $replayItem.incident)
    Set-Content -LiteralPath $agentRunsPath -Value ($agentRuns | ConvertTo-Json -Depth 30) -Encoding UTF8

    $report = $null
    try {
        $report = Invoke-RestMethod -Uri ("http://127.0.0.1:8082/incidents/{0}/report" -f $replayItem.incident) -Method Get -TimeoutSec 10
        $reportPath = Join-Path $runDir ("report-{0}.json" -f $replayItem.incident)
        Set-Content -LiteralPath $reportPath -Value ($report | ConvertTo-Json -Depth 30) -Encoding UTF8
    } catch {
    }

    $incidentBundle.Add([pscustomobject]@{
        ReplayItem = $replayItem
        Incident   = $incident
        Events     = @($events.items)
        AgentRuns  = @($agentRuns.items)
        Report     = $report
    })
}

Start-Sleep -Seconds 15

$incidentRunsMetric = Invoke-PrometheusQuery -Query "incident_runs_total"
$rollbackMetric = Invoke-PrometheusQuery -Query "rollback_requests_total"
$verificationMetric = Invoke-PrometheusQuery -Query "recovery_verification_total"
$graphNodeMetric = Invoke-PrometheusQuery -Query "sum by (node) (graph_node_duration_seconds_count)"
$toolCallMetric = Invoke-PrometheusQuery -Query "sum by (tool) (tool_call_duration_seconds_count)"
$approvalWaitMetric = Invoke-PrometheusQuery -Query "sum(approval_wait_duration_seconds_count)"
$serviceObservationMetric = Invoke-PrometheusQuery -Query "service_observation_value"
$serviceObservationAbnormalMetric = Invoke-PrometheusQuery -Query "service_observation_abnormal"
$releaseDeltaValueMetric = Invoke-PrometheusQuery -Query "release_comparison_delta_value"
$releaseDeltaRatioMetric = Invoke-PrometheusQuery -Query "release_comparison_delta_ratio"

Set-Content -LiteralPath (Join-Path $runDir "metric-incident-runs.json") -Value ($incidentRunsMetric | ConvertTo-Json -Depth 30) -Encoding UTF8
Set-Content -LiteralPath (Join-Path $runDir "metric-rollback-requests.json") -Value ($rollbackMetric | ConvertTo-Json -Depth 30) -Encoding UTF8
Set-Content -LiteralPath (Join-Path $runDir "metric-recovery-verification.json") -Value ($verificationMetric | ConvertTo-Json -Depth 30) -Encoding UTF8
Set-Content -LiteralPath (Join-Path $runDir "metric-graph-node-duration.json") -Value ($graphNodeMetric | ConvertTo-Json -Depth 30) -Encoding UTF8
Set-Content -LiteralPath (Join-Path $runDir "metric-tool-call-duration.json") -Value ($toolCallMetric | ConvertTo-Json -Depth 30) -Encoding UTF8
Set-Content -LiteralPath (Join-Path $runDir "metric-approval-wait-count.json") -Value ($approvalWaitMetric | ConvertTo-Json -Depth 30) -Encoding UTF8
Set-Content -LiteralPath (Join-Path $runDir "metric-service-observation-value.json") -Value ($serviceObservationMetric | ConvertTo-Json -Depth 30) -Encoding UTF8
Set-Content -LiteralPath (Join-Path $runDir "metric-service-observation-abnormal.json") -Value ($serviceObservationAbnormalMetric | ConvertTo-Json -Depth 30) -Encoding UTF8
Set-Content -LiteralPath (Join-Path $runDir "metric-release-comparison-delta-value.json") -Value ($releaseDeltaValueMetric | ConvertTo-Json -Depth 30) -Encoding UTF8
Set-Content -LiteralPath (Join-Path $runDir "metric-release-comparison-delta-ratio.json") -Value ($releaseDeltaRatioMetric | ConvertTo-Json -Depth 30) -Encoding UTF8

if ($UseMySQL -and $incidentIds.Count -gt 0) {
    $idClause = ($incidentIds | ForEach-Object { "'{0}'" -f $_ }) -join ", "

    Save-MySqlExport -ComposeFile $composeFile -Path (Join-Path $runDir "db-incidents.tsv") -Sql @"
SELECT id, service_name, severity, alert_summary, playbook_key, status, created_at, updated_at
FROM incidents
WHERE id IN ($idClause)
ORDER BY created_at;
"@

    Save-MySqlExport -ComposeFile $composeFile -Path (Join-Path $runDir "db-approvals.tsv") -Sql @"
SELECT incident_id, status, reviewer, comment, updated_at
FROM approvals
WHERE incident_id IN ($idClause)
ORDER BY updated_at;
"@

    Save-MySqlExport -ComposeFile $composeFile -Path (Join-Path $runDir "db-action-receipts.tsv") -Sql @"
SELECT incident_id, receipt_id, playbook_key, idempotency_key, action_type, target_service, status, verification_status, executed_at
FROM action_receipts
WHERE incident_id IN ($idClause)
ORDER BY executed_at;
"@

    Save-MySqlExport -ComposeFile $composeFile -Path (Join-Path $runDir "db-incident-events.tsv") -Sql @"
SELECT incident_id, event_type, actor_type, actor_name, payload_json, created_at
FROM incident_events
WHERE incident_id IN ($idClause)
ORDER BY id;
"@

    Save-MySqlExport -ComposeFile $composeFile -Path (Join-Path $runDir "db-agent-runs.tsv") -Sql @"
SELECT incident_id, node_name, model_name, prompt_version, latency_ms, status, checkpoint_id, created_at
FROM agent_runs
WHERE incident_id IN ($idClause)
ORDER BY id;
"@

    Save-MySqlExport -ComposeFile $composeFile -Path (Join-Path $runDir "db-evidence-items.tsv") -Sql @"
SELECT incident_id, evidence_id, source_type, source_ref, summary, confidence, created_at
FROM evidence_items
WHERE incident_id IN ($idClause)
ORDER BY incident_id, id;
"@
}

$reportLines = New-Object System.Collections.Generic.List[string]
$generatedAt = Get-Date -Format 'yyyy-MM-dd HH:mm:ss zzz'
$reportLines.Add("# GraphOps 全流程运行报告")
$reportLines.Add("")
$reportLines.Add("- 生成时间：$generatedAt")
$reportLines.Add('- 场景：`' + (Localize-Text $Scenario) + '`')
$reportLines.Add('- 推理模式：`' + (Localize-Text $ReasonerProvider) + '`')
$reportLines.Add('- 使用 MySQL 持久化：`' + (Localize-Text $UseMySQL) + '`')
$reportLines.Add('- 主场景自动审批回滚：`' + (Localize-Text $ApproveMain) + '`')
$reportLines.Add("")
$reportLines.Add("## 一键命令")
$reportLines.Add("")
$reportLines.Add('```powershell')
$reportLines.Add("powershell -ExecutionPolicy Bypass -File .\scripts\run-full-flow.ps1")
$reportLines.Add('```')
$reportLines.Add("")
$reportLines.Add("## 本次脚本执行内容")
$reportLines.Add("")
$reportLines.Add("1. 启动 Docker 依赖：Redis、Prometheus、Grafana$(if ($UseMySQL) { '、MySQL' } else { '' })。")
$reportLines.Add('2. 通过 `scripts/dev.ps1` 启动 `incident-api`、`ops-gateway` 和 `orchestrator`。')
$reportLines.Add('3. 通过 `scripts/replay.ps1` 回放指定故障场景。')
$reportLines.Add("4. 拉取 incident 详情、事件时间线、Agent 审计、最终报告和 Prometheus 指标快照。")
$reportLines.Add("5. 导出原始数据库表数据$(if ($UseMySQL) { '' } else { '（未启用 MySQL 持久化，因此跳过）' })。")
$reportLines.Add("")
$reportLines.Add("## 回放结果概览")
$reportLines.Add("")
Add-MarkdownTable -Lines $reportLines -Rows $replayResults
$reportLines.Add("")
$reportLines.Add("## 事件详情")
$reportLines.Add("")

foreach ($bundle in $incidentBundle) {
    $incident = $bundle.Incident
    $report = $bundle.Report

    $reportLines.Add('### `' + $incident.id + '`')
    $reportLines.Add("")
    $reportLines.Add('- 服务：`' + $incident.service_name + '`')
    $reportLines.Add('- 严重级别：`' + $incident.severity + '`')
    $reportLines.Add('- 场景键：`' + $incident.playbook_key + '`（' + (Localize-Text $incident.playbook_key) + '）')
    $reportLines.Add("- 告警摘要：$(Localize-Text $incident.alert_summary)")
    $reportLines.Add('- 最终状态：`' + (Localize-Text $incident.status) + '`')
    $reportLines.Add('- 事件时间线条数：`' + $bundle.Events.Count + '`')
    $reportLines.Add('- Agent 运行记录条数：`' + $bundle.AgentRuns.Count + '`')
    if ($incident.approval) {
        $reportLines.Add('- 审批结果：`' + (Localize-Text $incident.approval.status) + '`，审批人：`' + (Localize-Text $incident.approval.reviewer) + '`')
    }
    if ($incident.analysis -and $incident.analysis.proposed_action) {
        $reportLines.Add('- 建议动作：`' + (Localize-Text $incident.analysis.proposed_action.action_type) + '`，目标服务：`' + $incident.analysis.proposed_action.target_service + '`')
        $reportLines.Add("- 动作原因：$(Localize-Text $incident.analysis.proposed_action.reason)")
    }
    if ($report) {
        $reportLines.Add("- 根因结论：$(Localize-Text $report.root_cause)")
        $reportLines.Add("- 建议处置：$(Localize-Text $report.recommended_action)")
        $reportLines.Add("- 验证结果：$(Localize-Text $report.verification)")
        if ($report.action_receipt) {
            $reportLines.Add('- 动作回执：`' + $report.action_receipt.receipt_id + '`（状态=`' + (Localize-Text $report.action_receipt.status) + '`，验证=`' + (Localize-Text $report.action_receipt.verification_status) + '`）')
        }
    }
    $reportLines.Add("")

    if ($report -and @($report.metrics).Count -gt 0) {
        $reportLines.Add("指标快照：")
        foreach ($metric in @($report.metrics)) {
            $reportLines.Add('- [' + $metric.phase + '] ' + $metric.display_name + ' = ' + $metric.value + ' ' + $metric.unit + '（阈值=' + $metric.threshold + '，异常=' + $metric.abnormal + '，source=' + $metric.source_mode + '）')
        }
        $reportLines.Add("")
    }

    if ($report -and @($report.release_comparisons).Count -gt 0) {
        $reportLines.Add("发布前后对比：")
        foreach ($comparison in @($report.release_comparisons)) {
            $reportLines.Add('- ' + $comparison.display_name + '：before=' + $comparison.before_value + ' ' + $comparison.unit + '，after=' + $comparison.after_value + ' ' + $comparison.unit + '，delta=' + $comparison.delta_value + ' ' + $comparison.unit + '，delta_ratio=' + $comparison.delta_ratio + '%')
        }
        $reportLines.Add("")
    }

    if ($report -and @($report.anomaly_summary).Count -gt 0) {
        $reportLines.Add("异常描述：")
        foreach ($item in @($report.anomaly_summary)) {
            $reportLines.Add('- ' + $item)
        }
        $reportLines.Add("")
    }

    if ($report -and @($report.handling_suggestions).Count -gt 0) {
        $reportLines.Add("处理建议：")
        foreach ($item in @($report.handling_suggestions)) {
            $reportLines.Add('- ' + $item)
        }
        $reportLines.Add("")
    }

    if ($bundle.Events.Count -gt 0) {
        $reportLines.Add("时间线：")
        foreach ($event in $bundle.Events) {
            $reportLines.Add('- `' + $event.created_at + '` [' + $event.actor_type + '/' + $event.actor_name + '] ' + $event.event_type)
        }
        $reportLines.Add("")
    }

    if ($bundle.AgentRuns.Count -gt 0) {
        $reportLines.Add("Agent 审计：")
        foreach ($agentRun in $bundle.AgentRuns) {
            $reportLines.Add('- `' + $agentRun.node_name + '` 状态=`' + $agentRun.status + '`，耗时=`' + $agentRun.latency_ms + 'ms`')
        }
        $reportLines.Add("")
    }

    if ($incident.analysis -and @($incident.analysis.evidence).Count -gt 0) {
        $reportLines.Add("证据：")
        foreach ($evidence in @($incident.analysis.evidence)) {
            $reportLines.Add('- `' + $evidence.evidence_id + '` [' + $evidence.source_type + '] ' + (Localize-Text $evidence.summary))
        }
        $reportLines.Add("")
    }

    if ($incident.analysis -and @($incident.analysis.hypotheses).Count -gt 0) {
        $reportLines.Add("假设：")
        foreach ($hypothesis in @($incident.analysis.hypotheses)) {
            $reportLines.Add('- `' + $hypothesis.hypothesis_id + '` ' + (Localize-Text $hypothesis.cause) + '（置信度=' + $hypothesis.confidence + '）')
        }
        $reportLines.Add("")
    }
}

$reportLines.Add("## Prometheus 指标快照")
$reportLines.Add("")
$reportLines.Add("以下为便于阅读的中文摘要，原始 Prometheus 响应仍保存在 `metric-*.json` 文件中。")
$reportLines.Add("")
$reportLines.Add("Incident 运行状态计数：")
foreach ($line in Format-IncidentRunMetricResult -MetricResponse $incidentRunsMetric) {
    $reportLines.Add($line)
}
$reportLines.Add("")
$reportLines.Add("回滚请求次数：")
foreach ($line in Format-RollbackMetricResult -MetricResponse $rollbackMetric) {
    $reportLines.Add($line)
}
$reportLines.Add("")
$reportLines.Add("恢复验证次数：")
foreach ($line in Format-VerificationMetricResult -MetricResponse $verificationMetric) {
    $reportLines.Add($line)
}
$reportLines.Add("")
$reportLines.Add("Graph 节点样本数：")
foreach ($line in Format-NamedMetricResult -MetricResponse $graphNodeMetric -MetricLabel "node") {
    $reportLines.Add($line)
}
$reportLines.Add("")
$reportLines.Add("工具调用样本数：")
foreach ($line in Format-NamedMetricResult -MetricResponse $toolCallMetric -MetricLabel "tool") {
    $reportLines.Add($line)
}
$reportLines.Add("")
$reportLines.Add("服务指标样本数：")
foreach ($line in Format-NamedMetricResult -MetricResponse $serviceObservationMetric -MetricLabel "metric") {
    $reportLines.Add($line)
}
$reportLines.Add("")
$reportLines.Add("发布前后差值样本数：")
foreach ($line in Format-NamedMetricResult -MetricResponse $releaseDeltaValueMetric -MetricLabel "metric") {
    $reportLines.Add($line)
}
$reportLines.Add("")
$reportLines.Add("审批等待样本数：")
foreach ($item in @($approvalWaitMetric.data.result)) {
    $reportLines.Add("- " + $item.value[1] + " 次")
}
$reportLines.Add("")
$reportLines.Add("## Grafana")
$reportLines.Add("")
$reportLines.Add("- Grafana 首页：http://127.0.0.1:3000")
$reportLines.Add("- Incident 总览看板：http://127.0.0.1:3000/d/graphops-incident-overview/graphops-incident-overview")
$reportLines.Add("- Agent 运行时看板：http://127.0.0.1:3000/d/graphops-agent-runtime/graphops-agent-runtime")
$reportLines.Add("")
$reportLines.Add("## 原始产物")
$reportLines.Add("")
$reportLines.Add('- 回放结果：`replay-result.json`')
$reportLines.Add('- Incident API 原始响应：`incident-*.json`、`events-*.json`、`agent-runs-*.json` 和 `report-*.json`')
$reportLines.Add('- Prometheus 指标快照：`metric-*.json`（包含 service observation 和 release delta）')
if ($UseMySQL) {
    $reportLines.Add('- 数据库导出：`db-incidents.tsv`、`db-approvals.tsv`、`db-action-receipts.tsv`、`db-incident-events.tsv`、`db-agent-runs.tsv`、`db-evidence-items.tsv`')
}
$reportLines.Add("")
$reportLines.Add("## 手动执行兜底方案")
$reportLines.Add("")
$reportLines.Add("如果你希望逐步执行，而不是使用一键脚本，可以按下面的命令手动跑：")
$reportLines.Add("")
$reportLines.Add('```powershell')
$reportLines.Add('$env:REDIS_URL="redis://127.0.0.1:6379/0"')
$reportLines.Add('$env:REASONER_PROVIDER="{0}"' -f $ReasonerProvider)
if ($ReasonerProvider -eq "ollama") {
    $reportLines.Add('$env:OLLAMA_MODEL="{0}"' -f $OllamaModel)
}
$reportLines.Add("docker compose up -d redis prometheus grafana$(if ($UseMySQL) { ' mysql' } else { '' })")
$reportLines.Add(".\scripts\dev.ps1$(if ($UseMySQL) { ' -UseMySQL' } else { '' })")
$reportLines.Add(".\scripts\replay.ps1 -Scenario $Scenario$(if ($ApproveMain) { ' -ApproveMain' } else { '' })")
$reportLines.Add('```')

$reportPath = Join-Path $runDir "run-report.md"
Set-Content -LiteralPath $reportPath -Value $reportLines -Encoding UTF8

[pscustomobject]@{
    output_dir     = $runDir
    report_markdown = $reportPath
    replay_json    = $replayPath
} | Format-List
