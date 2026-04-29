const state = {
  incidentId: null,
  runResponse: null,
  metricHistory: {
    interrupts: [],
    replans: [],
    rollbacks: [],
    recovery: [],
    llm: [],
    audit: [],
  },
  metricTotals: null,
};

const LAST_INCIDENT_STORAGE_KEY = "graphops:last-incident-id";

const elements = {
  simulationForm: document.querySelector("#simulation-form"),
  reportQueryForm: document.querySelector("#report-query-form"),
  reportServiceName: document.querySelector("#report-service-name"),
  reportStatus: document.querySelector("#report-status"),
  reportQueryStatus: document.querySelector("#report-query-status"),
  reportHistoryResults: document.querySelector("#report-history-results"),
  variantIndex: document.querySelector("#variant-index"),
  autoApprove: document.querySelector("#auto-approve"),
  runButton: document.querySelector("#run-button"),
  configProvider: document.querySelector("#config-provider"),
  configMainModel: document.querySelector("#config-main-model"),
  configParallelModel: document.querySelector("#config-parallel-model"),
  parallelModels: {
    change: document.querySelector("#agent-model-change"),
    log: document.querySelector("#agent-model-log"),
    dependency: document.querySelector("#agent-model-dependency"),
  },
  simulationStatus: document.querySelector("#simulation-status"),
  latestRunSummary: document.querySelector("#latest-run-summary"),
  metricsStatus: document.querySelector("#metrics-status"),
  approvalBar: document.querySelector("#approval-bar"),
  approvalTitle: document.querySelector("#approval-title"),
  approvalCopy: document.querySelector("#approval-copy"),
  approveButton: document.querySelector("#approve-button"),
  rejectButton: document.querySelector("#reject-button"),
  incidentSummary: document.querySelector("#incident-summary"),
  incidentAction: document.querySelector("#incident-action"),
  incidentVerification: document.querySelector("#incident-verification"),
  evidenceGroups: document.querySelector("#evidence-groups"),
  incidentReport: document.querySelector("#incident-report"),
  eventList: document.querySelector("#event-list"),
  agentRunRows: document.querySelector("#agent-run-rows"),
  visualizations: {
    incidentStatus: document.querySelector("#viz-incident-status"),
    evidenceSource: document.querySelector("#viz-evidence-source"),
    rollbackOutcomes: document.querySelector("#viz-rollback-outcomes"),
    recoveryOutcomes: document.querySelector("#viz-recovery-outcomes"),
    nodeLatencyRows: document.querySelector("#node-latency-rows"),
    toolLatencyRows: document.querySelector("#tool-latency-rows"),
    llmBreakdownRows: document.querySelector("#llm-breakdown-rows"),
  },
  trends: {
    interrupts: document.querySelector("#trend-interrupts"),
    replans: document.querySelector("#trend-replans"),
    rollbacks: document.querySelector("#trend-rollbacks"),
    recovery: document.querySelector("#trend-recovery"),
    llm: document.querySelector("#trend-llm"),
    audit: document.querySelector("#trend-audit"),
  },
  metrics: {
    runs: document.querySelector("#metric-runs"),
    interrupts: document.querySelector("#metric-interrupts"),
    replans: document.querySelector("#metric-replans"),
    evidence: document.querySelector("#metric-evidence"),
    rollbacks: document.querySelector("#metric-rollbacks"),
    recovery: document.querySelector("#metric-recovery"),
    llm: document.querySelector("#metric-llm"),
    audit: document.querySelector("#metric-audit"),
  },
};

document.addEventListener("DOMContentLoaded", () => {
  bindEvents();
  loadConfig();
  restoreLastIncident();
  queryReportHistory();
  refreshMetrics();
  window.setInterval(refreshMetrics, 10000);
});

function bindEvents() {
  elements.simulationForm.addEventListener("submit", runSimulation);
  elements.reportQueryForm.addEventListener("submit", queryReportHistory);
  elements.approveButton.addEventListener("click", () => resumeIncident(true));
  elements.rejectButton.addEventListener("click", () => resumeIncident(false));
}

async function runSimulation(event) {
  event.preventDefault();
  setSimulationBusy(true, "正在创建 incident 并触发 LangGraph 工作流...");

  try {
    const variantIndex = clampVariant(Number(elements.variantIndex.value));
    const playbookKey =
      variantIndex > 0
        ? `release_config_regression_${String(variantIndex).padStart(2, "0")}`
        : "release_config_regression";

    const createBody = {
      service_name: "order-api",
      severity: "P1",
      alert_summary: "5xx spike after deploy",
      playbook_key: playbookKey,
      context: buildIncidentContext(variantIndex),
    };

    const incident = await fetchJson("/incidents", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(createBody),
    });

    state.incidentId = incident.id;
    persistLastIncidentId(incident.id);
    state.runResponse = await fetchJson(`/demo/api/runs/incidents/${incident.id}`, {
      method: "POST",
    });

    await refreshIncidentBundle();

    if (elements.autoApprove.checked && state.runResponse.interrupt) {
      await resumeIncident(true, true);
      return;
    }

    const nextState = state.runResponse.status || "diagnosing";
    updateSimulationStatus(`模拟运行完成，当前状态：${nextState}`);
    if (state.runResponse.interrupt) {
      scrollToApproval();
    }
  } catch (error) {
    updateSimulationStatus(`运行失败：${error.message}`, true);
  } finally {
    setSimulationBusy(false);
  }
}

async function resumeIncident(approved, silent = false) {
  if (!state.incidentId) {
    return;
  }

  setApprovalBusy(true);
  if (!silent) {
    updateSimulationStatus(approved ? "正在批准回滚..." : "正在拒绝回滚...");
  }

  try {
    state.runResponse = await fetchJson(`/demo/api/runs/incidents/${state.incidentId}/resume`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        approved,
        reviewer: approved ? "demo-oncall" : "demo-reviewer",
        comment: approved ? "approved from demo console" : "rejected from demo console",
      }),
    });

    await refreshIncidentBundle();
    updateSimulationStatus(
      approved
        ? `审批通过，工作流已恢复执行，当前状态：${state.runResponse.status}`
        : `审批已拒绝，incident 当前状态：${state.runResponse.status}`
    );
  } catch (error) {
    updateSimulationStatus(`审批恢复失败：${error.message}`, true);
  } finally {
    setApprovalBusy(false);
  }
}

async function refreshIncidentBundle() {
  if (!state.incidentId) {
    renderLatestRunSummary(null, null, []);
    return;
  }

  const incident = await fetchJson(`/incidents/${state.incidentId}`);
  const events = await fetchJson(`/incidents/${state.incidentId}/events`);
  const agentRuns = await fetchJson(`/incidents/${state.incidentId}/agent-runs`);
  let report = null;

  try {
    report = await fetchJson(`/incidents/${state.incidentId}/report`);
  } catch (error) {
    if (!/404/.test(error.message)) {
      throw error;
    }
  }

  renderIncidentSummary(incident);
  renderActionPlan(incident);
  renderVerification(incident, report);
  renderEvidence(incident);
  renderReport(incident, report);
  renderEvents(events.items || []);
  renderAgentRuns(agentRuns.items || []);
  renderApprovalBar(incident);
  renderLatestRunSummary(incident, report, agentRuns.items || []);
}

async function refreshMetrics() {
  try {
    const [orchestratorMetrics, opsGatewayMetrics] = await Promise.all([
      fetchText("/demo/api/metrics/orchestrator"),
      fetchText("/demo/api/metrics/opsgateway"),
    ]);

    const orchestrator = parsePrometheusMetrics(orchestratorMetrics);
    const opsGateway = parsePrometheusMetrics(opsGatewayMetrics);

    elements.metrics.runs.textContent = formatStatusMap(orchestrator["incident_runs_total"]);
    elements.metrics.interrupts.textContent = formatNumber(sumMetric(orchestrator["graph_interrupts_total"]));
    elements.metrics.replans.textContent = formatNumber(sumMetric(orchestrator["graph_replans_total"]));
    elements.metrics.evidence.textContent = formatSourceMap(orchestrator["evidence_items_total"]);
    elements.metrics.rollbacks.textContent = formatResultMap(opsGateway["rollback_requests_total"]);
    elements.metrics.recovery.textContent = formatResultMap(opsGateway["recovery_verification_total"]);
    elements.metrics.llm.textContent = formatNumber(sumMetric(orchestrator["llm_calls_total"]));
    elements.metrics.audit.textContent = formatNumber(sumMetric(orchestrator["audit_write_failures_total"]));
    updateTrendHistory(orchestrator, opsGateway);
    renderMetricsBreakdown(orchestrator, opsGateway);
    renderTrendCharts();
    elements.metricsStatus.textContent = "指标刷新成功。";
  } catch (error) {
    elements.metricsStatus.textContent = `指标刷新失败：${error.message}`;
  }
}

async function loadConfig() {
  try {
    const config = await fetchJson("/demo/api/config");
    elements.configProvider.textContent = config.reasoner_provider || "-";
    elements.configMainModel.textContent = config.ollama_main_model || "-";
    elements.configParallelModel.textContent = config.ollama_parallel_model || "-";
    elements.parallelModels.change.textContent = config.ollama_parallel_model || "-";
    elements.parallelModels.log.textContent = config.ollama_parallel_model || "-";
    elements.parallelModels.dependency.textContent = config.ollama_parallel_model || "-";
  } catch (error) {
    elements.configProvider.textContent = "unavailable";
    elements.configMainModel.textContent = "unavailable";
    elements.configParallelModel.textContent = "unavailable";
    elements.parallelModels.change.textContent = "unavailable";
    elements.parallelModels.log.textContent = "unavailable";
    elements.parallelModels.dependency.textContent = "unavailable";
  }
}

async function queryReportHistory(event) {
  if (event) {
    event.preventDefault();
  }

  const serviceName = elements.reportServiceName.value.trim();
  const status = elements.reportStatus.value.trim();
  const params = new URLSearchParams();
  if (serviceName) {
    params.set("service_name", serviceName);
  }
  if (status) {
    params.set("status", status);
  }
  params.set("limit", "20");

  elements.reportQueryStatus.textContent = "正在查询历史 incident...";

  try {
    const result = await fetchJson(`/incidents?${params.toString()}`);
    renderReportHistory(result.items || []);
    elements.reportQueryStatus.textContent = `已加载 ${(result.items || []).length} 条历史记录。`;
  } catch (error) {
    elements.reportQueryStatus.textContent = `历史查询失败：${error.message}`;
  }
}

function restoreLastIncident() {
  try {
    const incidentId = window.localStorage.getItem(LAST_INCIDENT_STORAGE_KEY);
    if (!incidentId) {
      return;
    }
    state.incidentId = incidentId;
    updateSimulationStatus(`已恢复最近一次 incident：${incidentId}`);
    refreshIncidentBundle().catch(() => {
      state.incidentId = null;
      window.localStorage.removeItem(LAST_INCIDENT_STORAGE_KEY);
      renderLatestRunSummary(null, null, []);
      renderReportHistory([]);
    });
  } catch {
    // Best effort only.
  }
}

function persistLastIncidentId(incidentId) {
  try {
    window.localStorage.setItem(LAST_INCIDENT_STORAGE_KEY, incidentId);
  } catch {
    // Best effort only.
  }
}

function renderIncidentSummary(incident) {
  const context = incident.context || {};
  const analysis = incident.analysis || {};
  elements.incidentSummary.innerHTML = `
    <div class="summary-line"><strong>ID</strong> <span class="mono">${escapeHtml(incident.id)}</span></div>
    <div class="summary-line"><strong>Status</strong> ${renderStatusBadge(incident.status)}</div>
    <div class="summary-line"><strong>Service</strong> ${escapeHtml(incident.service_name)}</div>
    <div class="summary-line"><strong>Playbook</strong> <span class="mono">${escapeHtml(incident.playbook_key || "-")}</span></div>
    <div class="summary-line"><strong>Release</strong> <span class="mono">${escapeHtml(context.release_version || "-")}</span></div>
    <div class="summary-line"><strong>Rollback Target</strong> <span class="mono">${escapeHtml(context.previous_version || "-")}</span></div>
    <div class="summary-line"><strong>Parallel Evidence</strong> ${Array.isArray(analysis.evidence) ? analysis.evidence.length : 0} items</div>
  `;
}

function renderLatestRunSummary(incident, report, agentRuns) {
  if (!incident) {
    elements.latestRunSummary.innerHTML = `<div class="empty-state">还没有保存的联调结果。</div>`;
    return;
  }

  const approval = incident.approval || {};
  const reportBody = report || incident.report || {};
  const rootCause = reportBody.root_cause || reportBody.rootCause || "-";
  const verification = reportBody.verification || "-";
  const nodeCount = Array.isArray(agentRuns) ? agentRuns.length : 0;

  elements.latestRunSummary.innerHTML = `
    <div class="latest-summary-grid">
      <div class="summary-line"><strong>Incident</strong><span class="mono">${escapeHtml(incident.id)}</span></div>
      <div class="summary-line"><strong>Status</strong>${renderStatusBadge(incident.status)}</div>
      <div class="summary-line"><strong>Approval</strong><span>${escapeHtml(approval.status || "not_reviewed")}</span></div>
      <div class="summary-line"><strong>Playbook</strong><span class="mono">${escapeHtml(incident.playbook_key || "-")}</span></div>
      <div class="summary-line"><strong>Agent Runs</strong><span>${escapeHtml(String(nodeCount))}</span></div>
      <div class="summary-line"><strong>Root Cause</strong><span>${escapeHtml(rootCause)}</span></div>
      <div><strong>Verification</strong><p>${escapeHtml(verification)}</p></div>
    </div>
  `;
}

function renderReportHistory(items) {
  if (!items.length) {
    elements.reportHistoryResults.innerHTML = `<div class="empty-state">还没有查询结果。</div>`;
    return;
  }

  elements.reportHistoryResults.innerHTML = `
    <div class="history-list">
      ${items
        .map((item) => {
          const report = item.report || {};
          const summary = report.summary || item.alert_summary || "No report summary yet.";
          const rootCause = report.root_cause || report.rootCause || "No root cause saved.";
          return `
            <article class="history-item">
              <div class="history-head">
                <div>
                  <strong class="mono">${escapeHtml(item.id)}</strong>
                  <div class="history-meta">${escapeHtml(item.service_name)} / ${escapeHtml(item.playbook_key || "-")}</div>
                </div>
                ${renderStatusBadge(item.status)}
              </div>
              <div class="history-meta">Created: ${escapeHtml(item.created_at || "-")}</div>
              <p>${escapeHtml(summary)}</p>
              <p><strong>Root Cause</strong> ${escapeHtml(rootCause)}</p>
              <div class="history-actions">
                <button type="button" class="secondary-button history-load-button" data-incident-id="${escapeHtml(item.id)}">加载到主视图</button>
              </div>
            </article>
          `;
        })
        .join("")}
    </div>
  `;

  elements.reportHistoryResults.querySelectorAll(".history-load-button").forEach((button) => {
    button.addEventListener("click", () => {
      const incidentId = button.getAttribute("data-incident-id");
      if (!incidentId) {
        return;
      }
      state.incidentId = incidentId;
      persistLastIncidentId(incidentId);
      refreshIncidentBundle()
        .then(() => {
          updateSimulationStatus(`已加载历史 incident：${incidentId}`);
          scrollToTop();
        })
        .catch((error) => {
          updateSimulationStatus(`加载历史 incident 失败：${error.message}`, true);
        });
    });
  });
}

function renderActionPlan(incident) {
  const action = incident.analysis?.proposed_action;
  if (!action) {
    elements.incidentAction.innerHTML = `<div class="empty-state">Planner 暂未建议回滚动作。</div>`;
    return;
  }

  const policy = action.verification_policy || {};
  elements.incidentAction.innerHTML = `
    <div class="summary-line"><strong>Action</strong> ${escapeHtml(action.action_type)}</div>
    <div class="summary-line"><strong>Target Service</strong> ${escapeHtml(action.target_service)}</div>
    <div class="summary-line"><strong>Current Revision</strong> <span class="mono">${escapeHtml(action.current_revision)}</span></div>
    <div class="summary-line"><strong>Target Revision</strong> <span class="mono">${escapeHtml(action.target_revision)}</span></div>
    <div class="summary-line"><strong>Risk</strong> ${escapeHtml(action.risk_level)}</div>
    <div class="summary-line"><strong>Verification Policy</strong> ${escapeHtml(
      `${policy.window_minutes || "-"} min / error_rate <= ${policy.max_error_rate ?? "-"} / p95 <= ${policy.max_p95_latency_ms ?? "-"} ms`
    )}</div>
    <p>${escapeHtml(action.reason)}</p>
  `;
}

function renderVerification(incident, report) {
  const receipt = report?.action_receipt || incident.report?.action_receipt || null;
  const verificationText = report?.verification || incident.report?.verification;
  const verificationResult = state.runResponse?.verification_result;

  if (!receipt && !verificationText && !verificationResult) {
    elements.incidentVerification.innerHTML = `<div class="empty-state">等待回滚执行或恢复验证。</div>`;
    return;
  }

  const status = receipt?.verification_status || verificationResult?.status || incident.status;
  elements.incidentVerification.innerHTML = `
    <div class="summary-line"><strong>Verification Status</strong> ${renderStatusBadge(status)}</div>
    <div class="summary-line"><strong>From Revision</strong> <span class="mono">${escapeHtml(receipt?.from_revision || "-")}</span></div>
    <div class="summary-line"><strong>To Revision</strong> <span class="mono">${escapeHtml(receipt?.to_revision || "-")}</span></div>
    <div class="summary-line"><strong>Executor</strong> ${escapeHtml(receipt?.executor || "-")}</div>
    <p>${escapeHtml(verificationText || verificationResult?.summary || receipt?.status_detail || "验证结果待生成。")}</p>
  `;
}

function renderEvidence(incident) {
  const evidence = incident.analysis?.evidence || [];
  if (!evidence.length) {
    elements.evidenceGroups.innerHTML = `<div class="empty-state">还没有 evidence。</div>`;
    return;
  }

  const grouped = new Map();
  evidence.forEach((item) => {
    const key = item.source_type || "unknown";
    if (!grouped.has(key)) {
      grouped.set(key, []);
    }
    grouped.get(key).push(item);
  });

  elements.evidenceGroups.innerHTML = Array.from(grouped.entries())
    .map(
      ([sourceType, items]) => `
        <section class="evidence-section">
          <h3>${escapeHtml(sourceType)}</h3>
          <div class="evidence-list">
            ${items
              .map(
                (item) => `
                  <article class="evidence-item">
                    <strong class="mono">${escapeHtml(item.source_ref)}</strong>
                    <p>${escapeHtml(item.summary)}</p>
                    <small>confidence: ${escapeHtml(String(item.confidence))}</small>
                  </article>
                `
              )
              .join("")}
          </div>
        </section>
      `
    )
    .join("");
}

function renderReport(incident, report) {
  const hypotheses = incident.analysis?.hypotheses || [];
  const reportBody = report || incident.report;
  const hypothesisHtml = hypotheses.length
    ? hypotheses
        .map(
          (item) => `
            <article class="evidence-item">
              <strong>${escapeHtml(item.hypothesis_id || "hypothesis")}</strong>
              <p>${escapeHtml(item.cause)}</p>
              <small>confidence: ${escapeHtml(String(item.confidence))}</small>
            </article>
          `
        )
        .join("")
    : `<div class="empty-state">还没有 hypothesis。</div>`;

  const reportHtml = reportBody
    ? `
      <article class="evidence-item">
        <strong>Final Report</strong>
        <p>${escapeHtml(reportBody.summary || "")}</p>
        <p><strong>Root Cause</strong> ${escapeHtml(reportBody.root_cause || "-")}</p>
        <p><strong>Recommended Action</strong> ${escapeHtml(reportBody.recommended_action || "-")}</p>
        <p><strong>Verification</strong> ${escapeHtml(reportBody.verification || "-")}</p>
      </article>
    `
    : `<div class="empty-state">等待最终报告。</div>`;

  elements.incidentReport.innerHTML = `
    <div class="evidence-list">${hypothesisHtml}</div>
    <div class="evidence-list" style="margin-top: 16px;">${reportHtml}</div>
  `;
}

function renderEvents(events) {
  if (!events.length) {
    elements.eventList.innerHTML = `<li class="empty-state">还没有 event。</li>`;
    return;
  }

  elements.eventList.innerHTML = events
    .map(
      (event) => `
        <li class="timeline-item">
          <strong>${escapeHtml(event.event_type)}</strong>
          <div class="timeline-meta">${escapeHtml(event.actor_type)} / ${escapeHtml(event.actor_name)}</div>
          <p>${escapeHtml(JSON.stringify(event.payload || {}))}</p>
        </li>
      `
    )
    .join("");
}

function renderAgentRuns(agentRuns) {
  if (!agentRuns.length) {
    elements.agentRunRows.innerHTML = `<tr><td colspan="5" class="empty-state">还没有 agent run。</td></tr>`;
    return;
  }

  elements.agentRunRows.innerHTML = agentRuns
    .map(
      (run) => `
        <tr>
          <td>${escapeHtml(run.node_name)}</td>
          <td class="mono">${escapeHtml(run.model_name || "-")}</td>
          <td>${renderStatusBadge(run.status)}</td>
          <td>${escapeHtml(String(run.latency_ms))} ms</td>
          <td class="mono">${escapeHtml(run.prompt_version)}</td>
        </tr>
      `
    )
    .join("");
}

function renderApprovalBar(incident) {
  const waiting = state.runResponse?.interrupt || incident.status === "waiting_for_approval";
  if (!waiting) {
    elements.approvalBar.classList.remove("waiting");
    elements.approvalBar.classList.add("resolved");
    elements.approvalTitle.textContent = "等待审批事件";
    elements.approvalCopy.textContent = "运行主场景后，如果进入 waiting_for_approval，这里会显示回滚摘要，并提供批准 / 拒绝按键。";
    elements.approveButton.disabled = true;
    elements.rejectButton.disabled = true;
    return;
  }

  const action = incident.analysis?.proposed_action;
  elements.approvalBar.classList.remove("resolved");
  elements.approvalBar.classList.add("waiting");
  elements.approvalTitle.textContent = "需要人工审批";
  elements.approvalCopy.textContent = action
    ? `Policy Agent 建议将 ${action.target_service} 从 ${action.current_revision} 回退到 ${action.target_revision}。`
    : "Policy Agent 已要求人工确认回滚。";
  elements.approveButton.disabled = false;
  elements.rejectButton.disabled = false;
}

function setSimulationBusy(busy, message = "") {
  elements.runButton.disabled = busy;
  elements.variantIndex.disabled = busy;
  elements.autoApprove.disabled = busy;
  if (message) {
    updateSimulationStatus(message);
  }
}

function setApprovalBusy(busy) {
  elements.approveButton.disabled = busy;
  elements.rejectButton.disabled = busy;
}

function updateSimulationStatus(message, isError = false) {
  elements.simulationStatus.textContent = message;
  elements.simulationStatus.style.color = isError ? "var(--danger)" : "";
}

function buildIncidentContext(variantIndex) {
  const suffix = variantIndex > 0 ? `-${String(variantIndex).padStart(2, "0")}` : "";
  return {
    cluster: "prod-cn",
    namespace: "checkout",
    environment: "production",
    alert_name: "OrderApiHigh5xxAfterRelease",
    alert_started_at: "2026-04-17T02:02:00Z",
    release_id: `deploy-2026.04.17-0155${suffix}`,
    release_version: `order-api@2026.04.17-0155${suffix}`,
    previous_version: `order-api@2026.04.17-0142${suffix}`,
    labels: {
      service: "order-api",
      team: "payments",
    },
  };
}

function clampVariant(value) {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.max(0, Math.min(18, Math.trunc(value)));
}

async function fetchJson(url, options = {}) {
  const response = await fetch(url, options);
  if (!response.ok) {
    throw new Error(`${response.status} ${await response.text()}`);
  }
  return response.json();
}

async function fetchText(url) {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`${response.status} ${await response.text()}`);
  }
  return response.text();
}

function parsePrometheusMetrics(payload) {
  const metrics = {};

  payload.split("\n").forEach((line) => {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      return;
    }

    const match = trimmed.match(/^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{([^}]*)\})?\s+(-?\d+(?:\.\d+)?)$/);
    if (!match) {
      return;
    }

    const [, name, , rawLabels = "", rawValue] = match;
    const labels = {};
    rawLabels.split(",").forEach((pair) => {
      if (!pair) {
        return;
      }
      const parts = pair.split("=");
      if (parts.length !== 2) {
        return;
      }
      labels[parts[0].trim()] = parts[1].trim().replace(/^"|"$/g, "");
    });

    if (!metrics[name]) {
      metrics[name] = [];
    }
    metrics[name].push({ labels, value: Number(rawValue) });
  });

  return metrics;
}

function sumMetric(entries = []) {
  return entries.reduce((sum, item) => sum + item.value, 0);
}

function renderMetricsBreakdown(orchestrator, opsGateway) {
  renderBarViz(
    elements.visualizations.incidentStatus,
    aggregateByLabel(orchestrator["incident_runs_total"], "status")
  );
  renderBarViz(
    elements.visualizations.evidenceSource,
    aggregateByLabel(orchestrator["evidence_items_total"], "source_type")
  );
  renderBarViz(
    elements.visualizations.rollbackOutcomes,
    aggregateByLabel(opsGateway["rollback_requests_total"], "result")
  );
  renderBarViz(
    elements.visualizations.recoveryOutcomes,
    aggregateByLabel(opsGateway["recovery_verification_total"], "status")
  );

  renderLatencyTable(
    elements.visualizations.nodeLatencyRows,
    buildAverageDurationRows(orchestrator, "graph_node_duration_seconds", "node")
  );
  renderLatencyTable(
    elements.visualizations.toolLatencyRows,
    buildAverageDurationRows(orchestrator, "tool_call_duration_seconds", "tool")
  );
  renderLlmBreakdown(
    elements.visualizations.llmBreakdownRows,
    buildLlmRows(orchestrator["llm_calls_total"])
  );
}

function updateTrendHistory(orchestrator, opsGateway) {
  const nextTotals = {
    interrupts: sumMetric(orchestrator["graph_interrupts_total"]),
    replans: sumMetric(orchestrator["graph_replans_total"]),
    rollbacks: sumMetric(opsGateway["rollback_requests_total"]),
    recovery: sumMetric(opsGateway["recovery_verification_total"]),
    llm: sumMetric(orchestrator["llm_calls_total"]),
    audit: sumMetric(orchestrator["audit_write_failures_total"]),
  };

  if (!state.metricTotals) {
    state.metricTotals = nextTotals;
    Object.keys(state.metricHistory).forEach((key) => {
      appendHistoryPoint(key, 0);
    });
    return;
  }

  Object.entries(nextTotals).forEach(([key, value]) => {
    const previous = state.metricTotals[key] ?? 0;
    appendHistoryPoint(key, Math.max(0, value - previous));
  });
  state.metricTotals = nextTotals;
}

function appendHistoryPoint(key, value) {
  const bucket = state.metricHistory[key];
  if (!bucket) {
    return;
  }
  bucket.push(value);
  if (bucket.length > 18) {
    bucket.shift();
  }
}

function renderTrendCharts() {
  renderTrendCard(elements.trends.interrupts, state.metricHistory.interrupts, "最近轮询周期内新增审批中断");
  renderTrendCard(elements.trends.replans, state.metricHistory.replans, "最近轮询周期内新增 replan");
  renderTrendCard(elements.trends.rollbacks, state.metricHistory.rollbacks, "最近轮询周期内新增回滚执行");
  renderTrendCard(elements.trends.recovery, state.metricHistory.recovery, "最近轮询周期内新增恢复验证");
  renderTrendCard(elements.trends.llm, state.metricHistory.llm, "最近轮询周期内新增 LLM 调用");
  renderTrendCard(elements.trends.audit, state.metricHistory.audit, "最近轮询周期内新增审计降级");
}

function renderTrendCard(container, values, description) {
  if (!container) {
    return;
  }
  if (!values || !values.length) {
    container.innerHTML = `<div class="empty-state">暂无趋势数据。</div>`;
    return;
  }

  const current = values[values.length - 1] ?? 0;
  const average = values.reduce((sum, value) => sum + value, 0) / values.length;
  container.innerHTML = `
    <div class="sparkline-shell">
      <div class="sparkline-metrics">
        <div>
          <div class="sparkline-current">${escapeHtml(formatNumber(current))}</div>
          <div class="sparkline-subtle">${escapeHtml(description)}</div>
        </div>
        <div class="sparkline-subtle">avg ${escapeHtml(formatNumber(average))}</div>
      </div>
      ${buildSparklineSvg(values)}
      <div class="sparkline-foot">
        <span>older</span>
        <span>latest</span>
      </div>
    </div>
  `;
}

function formatStatusMap(entries = []) {
  if (!entries.length) {
    return "-";
  }
  return entries
    .map((item) => `${item.labels.status || "unknown"} ${formatNumber(item.value)}`)
    .join(" / ");
}

function formatResultMap(entries = []) {
  if (!entries.length) {
    return "-";
  }
  const key = entries[0].labels.result ? "result" : "status";
  return entries.map((item) => `${item.labels[key] || "unknown"} ${formatNumber(item.value)}`).join(" / ");
}

function formatSourceMap(entries = []) {
  if (!entries.length) {
    return "-";
  }
  return entries
    .map((item) => `${item.labels.source_type || "unknown"} ${formatNumber(item.value)}`)
    .join(" / ");
}

function formatNumber(value) {
  if (!Number.isFinite(value)) {
    return "-";
  }
  return value.toLocaleString("zh-CN", { maximumFractionDigits: 2 });
}

function formatDurationSeconds(value) {
  if (!Number.isFinite(value)) {
    return "-";
  }
  if (value < 1) {
    return `${(value * 1000).toFixed(0)} ms`;
  }
  return `${value.toFixed(2)} s`;
}

function aggregateByLabel(entries = [], labelKey) {
  const totals = new Map();
  entries.forEach((item) => {
    const key = item.labels?.[labelKey] || "unknown";
    totals.set(key, (totals.get(key) || 0) + item.value);
  });

  return Array.from(totals.entries())
    .map(([label, value]) => ({ label, value }))
    .sort((left, right) => right.value - left.value);
}

function renderBarViz(container, rows) {
  if (!container) {
    return;
  }
  if (!rows.length) {
    container.innerHTML = `<div class="empty-state">暂无数据。</div>`;
    return;
  }

  const maxValue = Math.max(...rows.map((row) => row.value), 1);
  container.innerHTML = rows
    .map(
      (row) => `
        <div class="viz-row">
          <div class="viz-row-head">
            <span>${escapeHtml(row.label)}</span>
            <strong>${formatNumber(row.value)}</strong>
          </div>
          <div class="viz-bar-track">
            <div class="viz-bar-fill" style="width:${Math.max((row.value / maxValue) * 100, 6)}%"></div>
          </div>
        </div>
      `
    )
    .join("");
}

function buildAverageDurationRows(metrics, metricBaseName, labelKey) {
  const sumEntries = metrics[`${metricBaseName}_sum`] || [];
  const countEntries = metrics[`${metricBaseName}_count`] || [];
  const grouped = new Map();

  sumEntries.forEach((item) => {
    const key = item.labels?.[labelKey] || "unknown";
    const current = grouped.get(key) || { sum: 0, count: 0 };
    current.sum += item.value;
    grouped.set(key, current);
  });
  countEntries.forEach((item) => {
    const key = item.labels?.[labelKey] || "unknown";
    const current = grouped.get(key) || { sum: 0, count: 0 };
    current.count += item.value;
    grouped.set(key, current);
  });

  return Array.from(grouped.entries())
    .map(([label, value]) => ({
      label,
      count: value.count,
      average: value.count > 0 ? value.sum / value.count : 0,
    }))
    .sort((left, right) => right.average - left.average);
}

function renderLatencyTable(container, rows) {
  if (!container) {
    return;
  }
  if (!rows.length) {
    container.innerHTML = `<tr><td colspan="3" class="empty-state">暂无数据。</td></tr>`;
    return;
  }

  container.innerHTML = rows
    .map(
      (row) => `
        <tr>
          <td>${escapeHtml(row.label)}</td>
          <td>${escapeHtml(formatDurationSeconds(row.average))}</td>
          <td>${escapeHtml(formatNumber(row.count))}</td>
        </tr>
      `
    )
    .join("");
}

function buildLlmRows(entries = []) {
  return entries
    .map((item) => ({
      agent: item.labels?.agent || "unknown",
      model: item.labels?.model || "unknown",
      status: item.labels?.status || "unknown",
      calls: item.value,
    }))
    .sort((left, right) => right.calls - left.calls);
}

function renderLlmBreakdown(container, rows) {
  if (!container) {
    return;
  }
  if (!rows.length) {
    container.innerHTML = `<tr><td colspan="4" class="empty-state">暂无数据。</td></tr>`;
    return;
  }

  container.innerHTML = rows
    .map(
      (row) => `
        <tr>
          <td>${escapeHtml(row.agent)}</td>
          <td class="mono">${escapeHtml(row.model)}</td>
          <td>${renderStatusBadge(row.status)}</td>
          <td>${escapeHtml(formatNumber(row.calls))}</td>
        </tr>
      `
    )
    .join("");
}

function buildSparklineSvg(values) {
  const width = 320;
  const height = 70;
  const padding = 6;
  const maxValue = Math.max(...values, 1);
  const step = values.length > 1 ? (width - padding * 2) / (values.length - 1) : 0;
  const points = values.map((value, index) => {
    const x = padding + index * step;
    const y = height - padding - ((value / maxValue) * (height - padding * 2));
    return { x, y };
  });

  const linePoints = points.map((point) => `${point.x},${point.y}`).join(" ");
  const areaPoints = [`${padding},${height - padding}`, ...points.map((point) => `${point.x},${point.y}`), `${points.at(-1)?.x ?? padding},${height - padding}`].join(" ");

  return `
    <svg class="sparkline-frame" viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" aria-hidden="true">
      <line class="sparkline-axis" x1="${padding}" y1="${height - padding}" x2="${width - padding}" y2="${height - padding}"></line>
      <polygon class="sparkline-area" points="${areaPoints}"></polygon>
      <polyline class="sparkline-line" points="${linePoints}"></polyline>
      ${points
        .map((point) => `<circle class="sparkline-points" cx="${point.x}" cy="${point.y}" r="2.8"></circle>`)
        .join("")}
    </svg>
  `;
}

function renderStatusBadge(status = "unknown") {
  const normalized = String(status).trim() || "unknown";
  const cssClass = normalized.replace(/\s+/g, "_");
  return `<span class="badge ${escapeHtml(cssClass)}">${escapeHtml(normalized)}</span>`;
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function scrollToApproval() {
  elements.approvalBar?.scrollIntoView({ behavior: "smooth", block: "center" });
}

function scrollToTop() {
  window.scrollTo({ top: 0, behavior: "smooth" });
}
