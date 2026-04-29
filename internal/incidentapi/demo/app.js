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
  incidentMetricSnapshots: document.querySelector("#incident-metric-snapshots"),
  releaseComparisons: document.querySelector("#release-comparisons"),
  incidentAnomalies: document.querySelector("#incident-anomalies"),
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
  renderMetricSnapshots(incident, report);
  renderReleaseComparisons(incident, report);
  renderIncidentAnomalies(incident, report);
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
    <div class="summary-line"><strong>并行证据数</strong> ${Array.isArray(analysis.evidence) ? analysis.evidence.length : 0} 条</div>
  `;
}

function renderLatestRunSummary(incident, report, agentRuns) {
  if (!incident) {
    elements.latestRunSummary.innerHTML = `<div class="empty-state">\u8fd8\u6ca1\u6709\u4fdd\u5b58\u7684\u8054\u8c03\u7ed3\u679c\u3002</div>`;
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
      <div class="summary-line"><strong>\u5f53\u524d\u72b6\u6001</strong>${renderStatusBadge(incident.status)}</div>
      <div class="summary-line"><strong>\u5ba1\u6279\u72b6\u6001</strong><span>${escapeHtml(approval.status || "not_reviewed")}</span></div>
      <div class="summary-line"><strong>Playbook</strong><span class="mono">${escapeHtml(incident.playbook_key || "-")}</span></div>
      <div class="summary-line"><strong>Agent \u8fd0\u884c\u6570</strong><span>${escapeHtml(String(nodeCount))}</span></div>
      <div class="summary-line"><strong>\u6839\u56e0\u7ed3\u8bba</strong><span>${escapeHtml(localizeDemoText(rootCause))}</span></div>
      <div><strong>\u6062\u590d\u7ed3\u8bba</strong><p>${escapeHtml(localizeDemoText(verification))}</p></div>
    </div>
  `;
}



function renderReportHistory(items) {
  if (!items.length) {
    elements.reportHistoryResults.innerHTML = `<div class="empty-state">\u8fd8\u6ca1\u6709\u67e5\u8be2\u7ed3\u679c\u3002</div>`;
    return;
  }

  elements.reportHistoryResults.innerHTML = `
    <div class="history-list">
      ${items
        .map((item) => {
          const report = item.report || {};
          const summary = report.summary || item.alert_summary || "\u6682\u65e0\u62a5\u544a\u6458\u8981\u3002";
          const rootCause = report.root_cause || report.rootCause || "\u6682\u672a\u4fdd\u5b58\u6839\u56e0\u7ed3\u8bba\u3002";
          return `
            <article class="history-item">
              <div class="history-head">
                <div>
                  <strong class="mono">${escapeHtml(item.id)}</strong>
                  <div class="history-meta">${escapeHtml(item.service_name)} / ${escapeHtml(item.playbook_key || "-")}</div>
                </div>
                ${renderStatusBadge(item.status)}
              </div>
              <div class="history-meta">\u521b\u5efa\u65f6\u95f4\uff1a${escapeHtml(item.created_at || "-")}</div>
              <p>${escapeHtml(localizeDemoText(summary))}</p>
              <p><strong>\u6839\u56e0\u7ed3\u8bba</strong> ${escapeHtml(localizeDemoText(rootCause))}</p>
              <div class="history-actions">
                <button type="button" class="secondary-button history-load-button" data-incident-id="${escapeHtml(item.id)}">\u52a0\u8f7d\u5230\u4e3b\u89c6\u56fe</button>
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
          updateSimulationStatus(`\u5df2\u52a0\u8f7d\u5386\u53f2 incident\uff1a${incidentId}`);
          scrollToTop();
        })
        .catch((error) => {
          updateSimulationStatus(`\u52a0\u8f7d\u5386\u53f2 incident \u5931\u8d25\uff1a${error.message}`, true);
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
  const verificationResult =
    state.runResponse?.incident_id === incident.id ? state.runResponse?.verification_result : null;

  if (!receipt && !verificationText && !verificationResult) {
    elements.incidentVerification.innerHTML = `<div class="empty-state">\u7b49\u5f85\u56de\u6eda\u6267\u884c\u6216\u6062\u590d\u9a8c\u8bc1\u3002</div>`;
    return;
  }

  const status = receipt?.verification_status || verificationResult?.status || incident.status;
  elements.incidentVerification.innerHTML = `
    <div class="summary-line"><strong>\u9a8c\u8bc1\u72b6\u6001</strong> ${renderStatusBadge(status)}</div>
    <div class="summary-line"><strong>\u56de\u6eda\u524d\u7248\u672c</strong> <span class="mono">${escapeHtml(receipt?.from_revision || "-")}</span></div>
    <div class="summary-line"><strong>\u56de\u6eda\u540e\u7248\u672c</strong> <span class="mono">${escapeHtml(receipt?.to_revision || "-")}</span></div>
    <div class="summary-line"><strong>\u6267\u884c\u4eba</strong> ${escapeHtml(receipt?.executor || "-")}</div>
    <p>${escapeHtml(localizeDemoText(verificationText || verificationResult?.summary || receipt?.status_detail || "\u9a8c\u8bc1\u7ed3\u679c\u5f85\u751f\u6210\u3002"))}</p>
  `;
}



function renderMetricSnapshots(incident, report) {
  const telemetry = resolveIncidentTelemetry(incident, report);
  if (!telemetry.metrics.length) {
    elements.incidentMetricSnapshots.innerHTML = `<div class="empty-state">\u6682\u65e0\u7ed3\u6784\u5316\u6307\u6807\u5feb\u7167\u3002</div>`;
    return;
  }

  const rows = telemetry.metrics
    .map(
      (item) => `
        <tr class="${item.abnormal ? "metric-table-row abnormal" : "metric-table-row"}">
          <td>${escapeHtml(item.display_name || item.key || "-")}</td>
          <td>${escapeHtml(localizePhase(item.phase || "-"))}</td>
          <td>${escapeHtml(formatMetricValue(item.value, item.unit))}</td>
          <td>${escapeHtml(item.threshold || "-")}</td>
          <td>${renderStatusBadge(item.abnormal ? "abnormal" : "normal")}</td>
          <td>${escapeHtml(localizeSourceMode(item.source_mode || "simulated"))}</td>
        </tr>
      `
    )
    .join("");

  elements.incidentMetricSnapshots.innerHTML = `
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>\u6307\u6807</th>
            <th>\u9636\u6bb5</th>
            <th>\u6570\u503c</th>
            <th>\u9608\u503c</th>
            <th>\u72b6\u6001</th>
            <th>\u6765\u6e90</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
  `;
}



function renderReleaseComparisons(incident, report) {
  const telemetry = resolveIncidentTelemetry(incident, report);
  if (!telemetry.releaseComparisons.length) {
    elements.releaseComparisons.innerHTML = `<div class="empty-state">\u6682\u65e0\u53d1\u5e03\u524d\u540e\u5bf9\u6bd4\u6570\u636e\u3002</div>`;
    return;
  }

  const rows = telemetry.releaseComparisons
    .map(
      (item) => `
        <tr>
          <td>${escapeHtml(item.display_name || item.key || "-")}</td>
          <td>${escapeHtml(formatMetricValue(item.before_value, item.unit))}</td>
          <td>${escapeHtml(formatMetricValue(item.after_value, item.unit))}</td>
          <td>${escapeHtml(formatMetricDelta(item.delta_value, item.unit))}</td>
          <td>${escapeHtml(formatPercent(item.delta_ratio))}</td>
        </tr>
      `
    )
    .join("");

  const notes = telemetry.releaseComparisons
    .map((item) => `<li>${escapeHtml(localizeDemoText(item.summary || `${item.display_name || item.key} \u5728\u53d1\u5e03\u540e\u53d1\u751f\u660e\u663e\u53d8\u5316\u3002`))}</li>`)
    .join("");

  elements.releaseComparisons.innerHTML = `
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>\u6307\u6807</th>
            <th>\u53d1\u5e03\u524d</th>
            <th>\u53d1\u5e03\u540e</th>
            <th>\u53d8\u5316\u503c</th>
            <th>\u53d8\u5316\u6bd4\u4f8b</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
    <ul class="insight-list">${notes}</ul>
  `;
}



function renderIncidentAnomalies(incident, report) {
  const telemetry = resolveIncidentTelemetry(incident, report);
  const anomalySummary = telemetry.anomalySummary.length
    ? telemetry.anomalySummary
    : telemetry.anomalies.map((item) => `[${item.severity}] ${item.description}`);
  const handlingSuggestions = telemetry.handlingSuggestions.length
    ? telemetry.handlingSuggestions
    : telemetry.anomalies.map((item) => item.handling_suggestion).filter(Boolean);

  if (!anomalySummary.length && !handlingSuggestions.length && !telemetry.anomalies.length) {
    elements.incidentAnomalies.innerHTML = `<div class="empty-state">\u6682\u65e0\u5f02\u5e38\u6458\u8981\u6216\u5904\u7f6e\u5efa\u8bae\u3002</div>`;
    return;
  }

  const anomalyCards = telemetry.anomalies.length
    ? telemetry.anomalies
        .map(
          (item) => `
            <article class="insight-card">
              <strong>${escapeHtml(localizeSeverity(item.severity || "info"))}</strong>
              <p>${escapeHtml(item.description || "-")}</p>
              <small>${escapeHtml(localizeSourceMode(item.source_mode || "simulated"))}</small>
            </article>
          `
        )
        .join("")
    : anomalySummary
        .map((item) => `<article class="insight-card"><p>${escapeHtml(item)}</p></article>`)
        .join("");

  const suggestionItems = handlingSuggestions.length
    ? handlingSuggestions.map((item) => `<li>${escapeHtml(item)}</li>`).join("")
    : `<li>\u6682\u65e0\u989d\u5916\u5efa\u8bae\u3002</li>`;

  elements.incidentAnomalies.innerHTML = `
    <div class="insight-stack">${anomalyCards}</div>
    <div class="suggestion-block">
      <strong>\u5904\u7406\u5efa\u8bae</strong>
      <ul class="insight-list">${suggestionItems}</ul>
    </div>
  `;
}



function renderEvidence(incident) {
  const evidence = incident.analysis?.evidence || [];
  if (!evidence.length) {
    elements.evidenceGroups.innerHTML = `<div class="empty-state">\u8fd8\u6ca1\u6709\u8bc1\u636e\u6570\u636e\u3002</div>`;
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
                    <p>${escapeHtml(localizeDemoText(item.summary))}</p>
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
              <p>${escapeHtml(localizeDemoText(item.cause))}</p>
              <small>confidence: ${escapeHtml(String(item.confidence))}</small>
            </article>
          `
        )
        .join("")
    : `<div class="empty-state">\u8fd8\u6ca1\u6709\u5047\u8bbe\u7ed3\u679c\u3002</div>`;

  const reportHtml = reportBody
    ? `
      <article class="evidence-item">
        <strong>\u6700\u7ec8\u62a5\u544a</strong>
        <p>${escapeHtml(localizeDemoText(reportBody.summary || ""))}</p>
        <p><strong>\u6839\u56e0\u7ed3\u8bba</strong> ${escapeHtml(localizeDemoText(reportBody.root_cause || "-"))}</p>
        <p><strong>\u5efa\u8bae\u5904\u7f6e</strong> ${escapeHtml(localizeDemoText(reportBody.recommended_action || "-"))}</p>
        <p><strong>\u6062\u590d\u7ed3\u8bba</strong> ${escapeHtml(localizeDemoText(reportBody.verification || "-"))}</p>
      </article>
    `
    : `<div class="empty-state">\u7b49\u5f85\u6700\u7ec8\u62a5\u544a\u751f\u6210\u3002</div>`;

  const reportInsights = reportBody
    ? `
      ${
        (reportBody.anomaly_summary || []).length
          ? `<div class="evidence-item"><strong>\u5f02\u5e38\u6458\u8981</strong><ul class="insight-list">${reportBody.anomaly_summary
              .map((item) => `<li>${escapeHtml(item)}</li>`)
              .join("")}</ul></div>`
          : ""
      }
      ${
        (reportBody.handling_suggestions || []).length
          ? `<div class="evidence-item"><strong>\u5904\u7406\u5efa\u8bae</strong><ul class="insight-list">${reportBody.handling_suggestions
              .map((item) => `<li>${escapeHtml(item)}</li>`)
              .join("")}</ul></div>`
          : ""
      }
    `
    : "";

  elements.incidentReport.innerHTML = `
    <div class="evidence-list">${hypothesisHtml}</div>
    <div class="evidence-list" style="margin-top: 16px;">${reportHtml}${reportInsights}</div>
  `;
}



function renderEvents(events) {
  if (!events.length) {
    elements.eventList.innerHTML = `<li class="empty-state">\u8fd8\u6ca1\u6709\u4e8b\u4ef6\u65f6\u95f4\u7ebf\u3002</li>`;
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
    elements.agentRunRows.innerHTML = `<tr><td colspan="5" class="empty-state">\u8fd8\u6ca1\u6709 Agent \u5ba1\u8ba1\u8bb0\u5f55\u3002</td></tr>`;
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

function resolveIncidentTelemetry(incident, report) {
  const reportBody = report || incident.report || {};
  const verificationResult =
    state.runResponse?.incident_id === incident.id ? state.runResponse?.verification_result || null : null;
  return {
    metrics: reportBody.metrics || verificationResult?.metrics || [],
    releaseComparisons: reportBody.release_comparisons || verificationResult?.release_comparisons || [],
    anomalies: reportBody.anomalies || verificationResult?.anomalies || [],
    anomalySummary: reportBody.anomaly_summary || [],
    handlingSuggestions: reportBody.handling_suggestions || [],
  };
}

function formatMetricValue(value, unit = "") {
  if (!Number.isFinite(Number(value))) {
    return "-";
  }
  const numeric = Number(value);
  switch (unit) {
    case "%":
      return `${numeric.toFixed(1)}%`;
    case "ms":
      return `${numeric.toFixed(numeric >= 1000 ? 0 : 1)} ms`;
    case "rpm":
    case "messages":
    case "count/min":
      return `${formatNumber(Math.round(numeric))} ${unit}`;
    default:
      return `${formatNumber(numeric)}${unit ? ` ${unit}` : ""}`;
  }
}

function formatMetricDelta(value, unit = "") {
  if (!Number.isFinite(Number(value))) {
    return "-";
  }
  const numeric = Number(value);
  const prefix = numeric > 0 ? "+" : "";
  switch (unit) {
    case "%":
      return `${prefix}${numeric.toFixed(1)}%`;
    case "ms":
      return `${prefix}${numeric.toFixed(numeric >= 1000 ? 0 : 1)} ms`;
    case "rpm":
    case "messages":
    case "count/min":
      return `${prefix}${formatNumber(Math.round(numeric))} ${unit}`;
    default:
      return `${prefix}${formatNumber(numeric)}${unit ? ` ${unit}` : ""}`;
  }
}

function formatPercent(value) {
  if (!Number.isFinite(Number(value))) {
    return "-";
  }
  const numeric = Number(value);
  const prefix = numeric > 0 ? "+" : "";
  return `${prefix}${numeric.toFixed(1)}%`;
}

function localizePhase(phase) {
  const map = {
    before_release: "发布前",
    alert_window: "告警窗口",
    after_rollback: "回滚后",
  };
  return map[phase] || phase;
}

function localizeSourceMode(mode) {
  const map = {
    simulated: "\u6a21\u62df\u6570\u636e",
    observed: "\u771f\u5b9e\u89c2\u6d4b",
  };
  return map[mode] || mode;
}



function localizeSeverity(severity) {
  const map = {
    critical: "\u4e25\u91cd",
    high: "\u9ad8",
    medium: "\u4e2d",
    low: "\u4f4e",
    info: "\u63d0\u793a",
  };
  return map[severity] || severity;
}




function localizeDemoText(value) {
  const text = String(value ?? "");
  const map = {
    "GraphOps completed a diagnostic run for order-api.": "GraphOps \u5df2\u5b8c\u6210 order-api \u7684\u4e00\u6b21\u8bca\u65ad\u8fd0\u884c\u3002",
    "The latest release introduced a configuration regression in order-api database connectivity.": "\u6700\u65b0\u4e00\u6b21\u53d1\u5e03\u5728 order-api \u7684\u6570\u636e\u5e93\u8fde\u63a5\u914d\u7f6e\u4e0a\u5f15\u5165\u4e86\u56de\u5f52\u3002",
    "The primary fault is likely in inventory-service and is propagating to the caller.": "\u4e3b\u8981\u6545\u969c\u5927\u6982\u7387\u4f4d\u4e8e inventory-service\uff0c\u5e76\u5411\u8c03\u7528\u65b9\u4f20\u64ad\u3002",
    "order-api is acting as the symptom carrier instead of the fault owner.": "order-api \u66f4\u50cf\u662f\u6545\u969c\u75c7\u72b6\u7684\u627f\u8f7d\u8005\uff0c\u800c\u4e0d\u662f\u6839\u6545\u969c\u6240\u5728\u3002",
    "Do not rollback. Escalate to the downstream owner and continue manual investigation.": "\u4e0d\u8981\u56de\u6eda\uff0c\u5347\u7ea7\u7ed9\u4e0b\u6e38\u670d\u52a1\u8d1f\u8d23\u4eba\u5e76\u7ee7\u7eed\u4eba\u5de5\u6392\u67e5\u3002",
    "Execute rollback for order-api.": "\u5bf9 order-api \u6267\u884c\u56de\u6eda\u3002",
    "No rollback was executed. Diagnostic report only.": "\u672a\u6267\u884c\u6062\u590d\u52a8\u4f5c\uff0c\u672c\u6b21\u4ec5\u8f93\u51fa\u8bca\u65ad\u62a5\u544a\u3002",
    "Not recovered: Order-api remains degraded while inventory-service and its storage path are still unhealthy.": "\u672a\u6062\u590d\uff1aorder-api \u4ecd\u5904\u4e8e\u964d\u7ea7\u72b6\u6001\uff0cinventory-service \u53ca\u5176\u5b58\u50a8\u94fe\u8def\u4ecd\u672a\u6062\u590d\u3002",
    "Recovered: 5xx dropped to 0.3% and P95 to 118ms. 2 of 2 recovery signals passed after rollback, and all tracked user, service, and storage indicators returned under threshold.": "\u5df2\u6062\u590d\uff1a5xx \u964d\u5230 0.3%\uff0cP95 \u964d\u5230 118ms\u3002\u56de\u6eda\u540e 2/2 \u6062\u590d\u4fe1\u53f7\u901a\u8fc7\uff0c\u7528\u6237\u3001\u670d\u52a1\u548c\u5b58\u50a8\u4fa7\u6307\u6807\u5747\u5df2\u56de\u5230\u9608\u503c\u5185\u3002",
    "No relevant order-api change in the last 2 hours.": "\u6700\u8fd1 2 \u5c0f\u65f6\u5185\u6ca1\u6709\u4e0e order-api \u76f8\u5173\u7684\u53ef\u7591\u53d8\u66f4\u3002",
    "order-api errors are dominated by timeouts when calling inventory-service.": "order-api \u7684\u4e3b\u8981\u9519\u8bef\u6a21\u5f0f\u662f\u8c03\u7528 inventory-service \u65f6\u8d85\u65f6\u3002",
    "The top error pattern is upstream timeout rather than local configuration failure.": "\u6700\u4e3b\u8981\u7684\u9519\u8bef\u6a21\u5f0f\u662f\u4e0a\u6e38\u8c03\u7528\u8d85\u65f6\uff0c\u800c\u4e0d\u662f\u672c\u5730\u914d\u7f6e\u5931\u8d25\u3002",
    "inventory-service is degraded with database pool exhaustion; downstream propagation is likely.": "inventory-service \u56e0\u6570\u636e\u5e93\u8fde\u63a5\u6c60\u8017\u5c3d\u800c\u964d\u7ea7\uff0c\u6781\u6709\u53ef\u80fd\u5411\u4e0a\u6e38\u4f20\u64ad\u3002",
    "inventory-service depends on a saturated database connection pool.": "inventory-service \u4f9d\u8d56\u7684\u6570\u636e\u5e93\u8fde\u63a5\u6c60\u5df2\u7ecf\u9971\u548c\u3002",
    "order-api released 8 minutes before the alert with a configuration bundle update.": "\u544a\u8b66\u53d1\u751f\u524d 8 \u5206\u949f\uff0corder-api \u521a\u5b8c\u6210\u4e00\u6b21\u914d\u7f6e\u5305\u66f4\u65b0\u53d1\u5e03\u3002",
    "Database DSN and pool settings changed in the same release window.": "\u540c\u4e00\u53d1\u5e03\u7a97\u53e3\u5185\u540c\u65f6\u4fee\u6539\u4e86\u6570\u636e\u5e93 DSN \u548c\u8fde\u63a5\u6c60\u914d\u7f6e\u3002",
    "High-frequency errors show invalid connection string and database authentication failures.": "\u9ad8\u9891\u9519\u8bef\u663e\u793a\u8fde\u63a5\u4e32\u65e0\u6548\uff0c\u5e76\u4f34\u968f\u6570\u636e\u5e93\u8ba4\u8bc1\u5931\u8d25\u3002",
    "The error spike starts immediately after the release and stays local to order-api.": "\u9519\u8bef\u6fc0\u589e\u7d27\u8ddf\u53d1\u5e03\u540e\u51fa\u73b0\uff0c\u4e14\u5f71\u54cd\u8303\u56f4\u4e3b\u8981\u5c40\u9650\u5728 order-api\u3002",
    "No downstream error amplification detected on inventory-service.": "\u6ca1\u6709\u5728 inventory-service \u4e0a\u89c2\u5bdf\u5230\u4e0b\u6e38\u7ea7\u8054\u653e\u5927\u3002",
    "payment-service remains healthy; the blast radius is currently limited to order-api.": "payment-service \u4fdd\u6301\u5065\u5eb7\uff0c\u5f53\u524d\u5f71\u54cd\u8303\u56f4\u4ec5\u9650\u4e8e order-api\u3002"
  };
  return map[text] || text;
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
