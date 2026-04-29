package opsgateway

import "fmt"

var scenarioData = buildScenarioData()

func buildScenarioData() map[string]ScenarioData {
	data := map[string]ScenarioData{
		"release_config_regression":   releaseRegressionScenario("release_config_regression", 0),
		"downstream_inventory_outage": downstreamInventoryScenario(),
	}

	for i := 1; i <= 18; i++ {
		key := fmt.Sprintf("release_config_regression_%02d", i)
		data[key] = releaseRegressionScenario(key, i)
	}

	return data
}

func releaseRegressionScenario(key string, index int) ScenarioData {
	suffix := ""
	targetRevision := "order-api@2026.04.17-0142"
	currentRevision := "order-api@2026.04.17-0155"
	if index > 0 {
		suffix = fmt.Sprintf("-%02d", index)
		targetRevision = fmt.Sprintf("%s%s", targetRevision, suffix)
		currentRevision = fmt.Sprintf("%s%s", currentRevision, suffix)
	}

	beforeChecks := []SignalCheck{
		{
			Name:          "error_rate",
			QueryRef:      "promql:error_rate:order-api",
			ObservedValue: 12.4,
			Threshold:     "<= 1.0",
			Passed:        false,
			Summary:       "HTTP 5xx ratio is above the rollback recovery threshold.",
		},
		{
			Name:          "p95_latency_ms",
			QueryRef:      "promql:p95_latency:order-api",
			ObservedValue: 980,
			Threshold:     "<= 300",
			Passed:        false,
			Summary:       "P95 latency is elevated and confirms user-facing degradation.",
		},
	}
	afterChecks := []SignalCheck{
		{
			Name:          "error_rate",
			QueryRef:      "promql:error_rate:order-api",
			ObservedValue: 0.3,
			Threshold:     "<= 1.0",
			Passed:        true,
			Summary:       "5xx ratio returned to the healthy baseline after rollback.",
		},
		{
			Name:          "p95_latency_ms",
			QueryRef:      "promql:p95_latency:order-api",
			ObservedValue: 118,
			Threshold:     "<= 300",
			Passed:        true,
			Summary:       "P95 latency recovered after reverting the bad release.",
		},
	}

	releaseBaselineMetrics := []MetricObservation{
		metricObservation("timeout_rate", "超时率", "before_release", 0.1, "%", "<= 1.0", false, "Timeout rate stayed within the healthy baseline before the release."),
		metricObservation("avg_response_time_ms", "接口平均响应时间", "before_release", 92, "ms", "<= 150", false, "Average response time was stable before the release window."),
		metricObservation("p95_latency_ms", "P95 延迟", "before_release", 120, "ms", "<= 300", false, "P95 latency was healthy before the release."),
		metricObservation("p99_latency_ms", "P99 延迟", "before_release", 210, "ms", "<= 500", false, "P99 latency was healthy before the release."),
		metricObservation("requests_per_minute", "每分钟请求量", "before_release", 18200, "rpm", ">= 15000", false, "Traffic volume was normal before the release."),
		metricObservation("http_4xx_rate", "4xx 比例", "before_release", 0.4, "%", "<= 2.0", false, "Client-side errors were low before the release."),
		metricObservation("http_5xx_rate", "5xx 错误率", "before_release", 0.2, "%", "<= 1.0", false, "Server-side errors were low before the release."),
		metricObservation("gc_count", "GC 次数", "before_release", 6, "count/min", "<= 20", false, "GC frequency was normal before the release."),
		metricObservation("gc_pause_ms", "GC 耗时", "before_release", 12, "ms", "<= 50", false, "GC pause time was stable before the release."),
		metricObservation("db_slow_queries", "数据库慢查询数", "before_release", 3, "count/min", "<= 10", false, "Slow SQL volume was normal before the release."),
		metricObservation("redis_hit_rate", "Redis 命中率", "before_release", 97.8, "%", ">= 90", false, "Cache hit ratio was healthy before the release."),
		metricObservation("mq_backlog", "MQ 堆积量", "before_release", 8, "messages", "<= 100", false, "Message backlog was negligible before the release."),
	}
	alertWindowMetrics := []MetricObservation{
		metricObservation("timeout_rate", "超时率", "alert_window", 6.8, "%", "<= 1.0", true, "Timeout rate surged after the release and directly affected user traffic."),
		metricObservation("avg_response_time_ms", "接口平均响应时间", "alert_window", 640, "ms", "<= 150", true, "Average response time spiked immediately after the release."),
		metricObservation("p95_latency_ms", "P95 延迟", "alert_window", 980, "ms", "<= 300", true, "P95 latency is far above the rollback decision threshold."),
		metricObservation("p99_latency_ms", "P99 延迟", "alert_window", 1540, "ms", "<= 500", true, "Tail latency confirms severe user-facing degradation."),
		metricObservation("requests_per_minute", "每分钟请求量", "alert_window", 17400, "rpm", ">= 15000", false, "Traffic stayed near baseline, so the spike is unlikely to be caused by traffic collapse."),
		metricObservation("http_4xx_rate", "4xx 比例", "alert_window", 1.2, "%", "<= 2.0", false, "4xx ratio increased slightly but is not the dominant failure mode."),
		metricObservation("http_5xx_rate", "5xx 错误率", "alert_window", 12.4, "%", "<= 1.0", true, "5xx error rate surged in the same window as the release."),
		metricObservation("gc_count", "GC 次数", "alert_window", 34, "count/min", "<= 20", true, "GC frequency increased under retry and connection churn pressure."),
		metricObservation("gc_pause_ms", "GC 耗时", "alert_window", 145, "ms", "<= 50", true, "GC pause time increased and contributed to latency amplification."),
		metricObservation("db_slow_queries", "数据库慢查询数", "alert_window", 58, "count/min", "<= 10", true, "Slow SQL count rose sharply after the configuration regression."),
		metricObservation("redis_hit_rate", "Redis 命中率", "alert_window", 84.2, "%", ">= 90", true, "Cache hit rate dropped during the incident because retries increased database pressure."),
		metricObservation("mq_backlog", "MQ 堆积量", "alert_window", 620, "messages", "<= 100", true, "Message backlog rose because downstream handlers slowed down."),
	}
	afterRollbackMetrics := []MetricObservation{
		metricObservation("timeout_rate", "超时率", "after_rollback", 0.2, "%", "<= 1.0", false, "Timeout rate returned to the healthy baseline after rollback."),
		metricObservation("avg_response_time_ms", "接口平均响应时间", "after_rollback", 88, "ms", "<= 150", false, "Average response time recovered after rollback."),
		metricObservation("p95_latency_ms", "P95 延迟", "after_rollback", 118, "ms", "<= 300", false, "P95 latency recovered after rollback."),
		metricObservation("p99_latency_ms", "P99 延迟", "after_rollback", 205, "ms", "<= 500", false, "P99 latency returned to baseline after rollback."),
		metricObservation("requests_per_minute", "每分钟请求量", "after_rollback", 18150, "rpm", ">= 15000", false, "Traffic volume remained stable after rollback."),
		metricObservation("http_4xx_rate", "4xx 比例", "after_rollback", 0.5, "%", "<= 2.0", false, "4xx ratio returned to normal after rollback."),
		metricObservation("http_5xx_rate", "5xx 错误率", "after_rollback", 0.3, "%", "<= 1.0", false, "5xx error rate dropped back under the release guardrail."),
		metricObservation("gc_count", "GC 次数", "after_rollback", 7, "count/min", "<= 20", false, "GC frequency normalized after rollback."),
		metricObservation("gc_pause_ms", "GC 耗时", "after_rollback", 14, "ms", "<= 50", false, "GC pause time returned to baseline after rollback."),
		metricObservation("db_slow_queries", "数据库慢查询数", "after_rollback", 4, "count/min", "<= 10", false, "Slow query volume returned to baseline."),
		metricObservation("redis_hit_rate", "Redis 命中率", "after_rollback", 97.1, "%", ">= 90", false, "Redis hit rate recovered after rollback."),
		metricObservation("mq_backlog", "MQ 堆积量", "after_rollback", 12, "messages", "<= 100", false, "Message backlog was drained after rollback."),
	}
	releaseComparisons := []MetricComparison{
		metricComparison("avg_response_time_ms", "发布前后平均响应时间", 92, 640, "ms", "Average latency increased by 548ms after the release."),
		metricComparison("p95_latency_ms", "发布前后 P95 延迟", 120, 980, "ms", "P95 latency increased by 860ms after the release."),
		metricComparison("p99_latency_ms", "发布前后 P99 延迟", 210, 1540, "ms", "P99 latency increased by 1330ms after the release."),
		metricComparison("http_5xx_rate", "发布前后 5xx 错误率", 0.2, 12.4, "%", "5xx error rate surged by 12.2 percentage points after the release."),
	}
	anomalies := []AnomalyFinding{
		anomalyFinding("http_5xx_rate", "critical", "发布后 5xx 错误率从 0.2% 激增到 12.4%，已经明显超出发布回滚阈值。", "优先核对最近发布中的配置变更、数据库连接串和连接池参数；若持续扩大，执行回滚。"),
		anomalyFinding("timeout_rate", "high", "超时率升至 6.8%，说明用户请求已经受到明显影响。", "检查数据库连接建立、线程池和重试策略，必要时限流并快速回退异常版本。"),
		anomalyFinding("p95_latency_ms", "high", "P95 从 120ms 升至 980ms，性能退化与错误峰值同时出现。", "结合日志与慢查询信息排查本地资源争用，必要时用上一稳定版本进行对照验证。"),
		anomalyFinding("gc_pause_ms", "medium", "GC 停顿时间放大到 145ms，说明异常重试和对象抖动正在放大尾延迟。", "检查连接泄漏、对象分配热点和失败重试风暴，避免 GC 放大发布故障影响。"),
		anomalyFinding("db_slow_queries", "medium", "数据库慢查询数显著上升，说明错误配置已经向存储侧放大。", "复核 SQL 连接参数、池大小和慢 SQL 来源，必要时在回滚后继续做数据库侧复盘。"),
	}

	beforeMetrics := appendMetricSlices(releaseBaselineMetrics, alertWindowMetrics)
	afterMetrics := appendMetricSlices(releaseBaselineMetrics, alertWindowMetrics, afterRollbackMetrics)

	return ScenarioData{
		CurrentRevision: currentRevision,
		TargetRevision:  targetRevision,
		ChangeItems: []EvidenceItem{
			{
				SourceRef:  fmt.Sprintf("deploy/order-api@2026.04.17-0155%s", suffix),
				Summary:    "order-api released 8 minutes before the alert with a configuration bundle update.",
				Confidence: 0.94,
			},
			{
				SourceRef:  fmt.Sprintf("config/order-api/db-dsn%s", suffix),
				Summary:    "Database DSN and pool settings changed in the same release window.",
				Confidence: 0.92,
			},
		},
		LogItems: []EvidenceItem{
			{
				SourceRef:  fmt.Sprintf("logs/order-api#db-auth%s", suffix),
				Summary:    "High-frequency errors show invalid connection string and database authentication failures.",
				Confidence: 0.97,
			},
			{
				SourceRef:  fmt.Sprintf("logs/order-api#error-cluster-1%s", suffix),
				Summary:    "The error spike starts immediately after the release and stays local to order-api.",
				Confidence: 0.90,
			},
		},
		DependencyItems: []EvidenceItem{
			{
				SourceRef:  fmt.Sprintf("dep/order-api->inventory-service%s", suffix),
				Summary:    "No downstream error amplification detected on inventory-service.",
				Confidence: 0.78,
			},
			{
				SourceRef:  fmt.Sprintf("dep/order-api->payment-service%s", suffix),
				Summary:    "payment-service remains healthy; the blast radius is currently limited to order-api.",
				Confidence: 0.82,
			},
		},
		VerificationBefore: VerificationResult{
			Status:             "not_recovered",
			ErrorRate:          12.4,
			P95LatencyMs:       980,
			WindowMinutes:      10,
			QueryRefs:          []string{"promql:error_rate:order-api", "promql:p95_latency:order-api"},
			SignalChecks:       beforeChecks,
			Metrics:            beforeMetrics,
			ReleaseComparisons: releaseComparisons,
			Anomalies:          anomalies,
			DecisionBasis:      "0 of 2 recovery signals passed before rollback, and multiple user-impacting service metrics remain abnormal.",
			Summary:            "5xx, timeout rate, latency, GC pause, and slow query indicators are all above threshold before rollback.",
		},
		VerificationAfter: VerificationResult{
			Status:             "recovered",
			ErrorRate:          0.3,
			P95LatencyMs:       118,
			WindowMinutes:      10,
			QueryRefs:          []string{"promql:error_rate:order-api", "promql:p95_latency:order-api"},
			SignalChecks:       afterChecks,
			Metrics:            afterMetrics,
			ReleaseComparisons: releaseComparisons,
			Anomalies:          anomalies,
			DecisionBasis:      "2 of 2 recovery signals passed after rollback, and all tracked user, service, and storage indicators returned under threshold.",
			Summary:            "Historical anomalies during the alert window were mitigated after rollback.",
		},
	}
}

func downstreamInventoryScenario() ScenarioData {
	beforeChecks := []SignalCheck{
		{
			Name:          "error_rate",
			QueryRef:      "promql:error_rate:order-api",
			ObservedValue: 8.7,
			Threshold:     "<= 1.0",
			Passed:        false,
			Summary:       "Order traffic remains degraded while inventory-service is timing out.",
		},
		{
			Name:          "p95_latency_ms",
			QueryRef:      "promql:p95_latency:order-api",
			ObservedValue: 840,
			Threshold:     "<= 300",
			Passed:        false,
			Summary:       "P95 latency remains elevated because the downstream dependency is still unhealthy.",
		},
	}
	afterChecks := []SignalCheck{
		{
			Name:          "error_rate",
			QueryRef:      "promql:error_rate:order-api",
			ObservedValue: 8.2,
			Threshold:     "<= 1.0",
			Passed:        false,
			Summary:       "Rolling back order-api does not remove the upstream timeout source.",
		},
		{
			Name:          "p95_latency_ms",
			QueryRef:      "promql:p95_latency:order-api",
			ObservedValue: 801,
			Threshold:     "<= 300",
			Passed:        false,
			Summary:       "Latency remains degraded because inventory-service is still saturated.",
		},
	}

	releaseBaselineMetrics := []MetricObservation{
		metricObservation("timeout_rate", "超时率", "before_release", 0.2, "%", "<= 1.0", false, "Timeout rate was healthy before the dependency outage."),
		metricObservation("avg_response_time_ms", "接口平均响应时间", "before_release", 95, "ms", "<= 150", false, "Average response time was healthy before the outage."),
		metricObservation("p95_latency_ms", "P95 延迟", "before_release", 126, "ms", "<= 300", false, "P95 latency was healthy before the outage."),
		metricObservation("p99_latency_ms", "P99 延迟", "before_release", 230, "ms", "<= 500", false, "P99 latency was healthy before the outage."),
		metricObservation("requests_per_minute", "每分钟请求量", "before_release", 17850, "rpm", ">= 15000", false, "Traffic volume was normal before the outage."),
		metricObservation("http_4xx_rate", "4xx 比例", "before_release", 0.5, "%", "<= 2.0", false, "4xx ratio was normal before the outage."),
		metricObservation("http_5xx_rate", "5xx 错误率", "before_release", 0.3, "%", "<= 1.0", false, "5xx ratio was healthy before the outage."),
		metricObservation("gc_count", "GC 次数", "before_release", 7, "count/min", "<= 20", false, "GC frequency was normal before the outage."),
		metricObservation("gc_pause_ms", "GC 耗时", "before_release", 16, "ms", "<= 50", false, "GC pause time was normal before the outage."),
		metricObservation("db_slow_queries", "数据库慢查询数", "before_release", 6, "count/min", "<= 10", false, "Slow query volume was normal before the outage."),
		metricObservation("redis_hit_rate", "Redis 命中率", "before_release", 96.4, "%", ">= 90", false, "Cache hit ratio was healthy before the outage."),
		metricObservation("mq_backlog", "MQ 堆积量", "before_release", 14, "messages", "<= 100", false, "Message backlog was negligible before the outage."),
	}
	alertWindowMetrics := []MetricObservation{
		metricObservation("timeout_rate", "超时率", "alert_window", 9.4, "%", "<= 1.0", true, "Timeout rate is driven by the degraded inventory-service dependency."),
		metricObservation("avg_response_time_ms", "接口平均响应时间", "alert_window", 588, "ms", "<= 150", true, "Average response time increased because downstream calls are timing out."),
		metricObservation("p95_latency_ms", "P95 延迟", "alert_window", 840, "ms", "<= 300", true, "P95 latency is elevated due to dependency saturation."),
		metricObservation("p99_latency_ms", "P99 延迟", "alert_window", 1380, "ms", "<= 500", true, "P99 latency confirms severe tail degradation from downstream blocking."),
		metricObservation("requests_per_minute", "每分钟请求量", "alert_window", 16920, "rpm", ">= 15000", false, "Traffic remained steady, so the issue is not caused by a traffic cliff."),
		metricObservation("http_4xx_rate", "4xx 比例", "alert_window", 0.8, "%", "<= 2.0", false, "4xx ratio remained low during the outage."),
		metricObservation("http_5xx_rate", "5xx 错误率", "alert_window", 8.7, "%", "<= 1.0", true, "5xx ratio increased because upstream requests are failing after dependency timeouts."),
		metricObservation("gc_count", "GC 次数", "alert_window", 18, "count/min", "<= 20", false, "GC frequency increased slightly but is not the dominant signal."),
		metricObservation("gc_pause_ms", "GC 耗时", "alert_window", 38, "ms", "<= 50", false, "GC pause stayed under threshold during the dependency outage."),
		metricObservation("db_slow_queries", "数据库慢查询数", "alert_window", 86, "count/min", "<= 10", true, "Downstream storage is showing a surge in slow queries."),
		metricObservation("redis_hit_rate", "Redis 命中率", "alert_window", 63.5, "%", ">= 90", true, "Redis hit rate dropped during the dependency incident, increasing database pressure."),
		metricObservation("mq_backlog", "MQ 堆积量", "alert_window", 4200, "messages", "<= 100", true, "MQ backlog built up because downstream consumers were stalled."),
	}
	afterRollbackMetrics := []MetricObservation{
		metricObservation("timeout_rate", "超时率", "after_rollback", 8.9, "%", "<= 1.0", true, "Timeout rate remained high because rollback did not remove the downstream fault."),
		metricObservation("avg_response_time_ms", "接口平均响应时间", "after_rollback", 561, "ms", "<= 150", true, "Average response time remained elevated after rollback."),
		metricObservation("p95_latency_ms", "P95 延迟", "after_rollback", 801, "ms", "<= 300", true, "P95 remained degraded because the dependency is still unhealthy."),
		metricObservation("p99_latency_ms", "P99 延迟", "after_rollback", 1320, "ms", "<= 500", true, "Tail latency remained elevated after rollback."),
		metricObservation("requests_per_minute", "每分钟请求量", "after_rollback", 16810, "rpm", ">= 15000", false, "Traffic volume remained steady after rollback."),
		metricObservation("http_4xx_rate", "4xx 比例", "after_rollback", 0.7, "%", "<= 2.0", false, "4xx ratio stayed low after rollback."),
		metricObservation("http_5xx_rate", "5xx 错误率", "after_rollback", 8.2, "%", "<= 1.0", true, "5xx ratio remained above threshold after rollback."),
		metricObservation("gc_count", "GC 次数", "after_rollback", 17, "count/min", "<= 20", false, "GC frequency remained acceptable after rollback."),
		metricObservation("gc_pause_ms", "GC 耗时", "after_rollback", 34, "ms", "<= 50", false, "GC pause remained below threshold after rollback."),
		metricObservation("db_slow_queries", "数据库慢查询数", "after_rollback", 79, "count/min", "<= 10", true, "Slow query volume remained high because the dependency bottleneck still exists."),
		metricObservation("redis_hit_rate", "Redis 命中率", "after_rollback", 66.1, "%", ">= 90", true, "Redis hit rate improved only slightly after rollback."),
		metricObservation("mq_backlog", "MQ 堆积量", "after_rollback", 4010, "messages", "<= 100", true, "MQ backlog remained high because the stalled dependency path was not fixed."),
	}
	releaseComparisons := []MetricComparison{
		metricComparison("avg_response_time_ms", "告警窗口平均响应时间变化", 95, 588, "ms", "Average latency increased by 493ms during the dependency outage."),
		metricComparison("p95_latency_ms", "告警窗口 P95 延迟变化", 126, 840, "ms", "P95 latency increased by 714ms during the dependency outage."),
		metricComparison("http_5xx_rate", "告警窗口 5xx 错误率变化", 0.3, 8.7, "%", "5xx error rate increased by 8.4 percentage points during the dependency outage."),
		metricComparison("mq_backlog", "告警窗口 MQ 堆积量变化", 14, 4200, "messages", "MQ backlog increased sharply during the dependency outage."),
	}
	anomalies := []AnomalyFinding{
		anomalyFinding("timeout_rate", "critical", "超时率升到 9.4%，主要错误模式来自对 inventory-service 的超时调用。", "优先联系下游服务负责人，检查库存服务线程池、数据库连接池和超时重试配置。"),
		anomalyFinding("p95_latency_ms", "high", "P95 延迟升到 840ms，说明上游服务正在承受下游阻塞传播。", "不要先回滚上游发布，优先验证下游依赖是否已经恢复。"),
		anomalyFinding("http_5xx_rate", "high", "5xx 错误率升到 8.7%，但日志和变更证据并不支持本地发布回归。", "结合 Change Agent 结论，避免误回滚，把排障重点放在 dependency path。"),
		anomalyFinding("db_slow_queries", "high", "数据库慢查询数飙升到 86 次/分钟，说明下游存储已经成为瓶颈。", "检查库存服务数据库执行计划、慢 SQL 和连接池配置。"),
		anomalyFinding("redis_hit_rate", "medium", "Redis 命中率跌到 63.5%，说明缓存未能有效保护下游存储。", "检查缓存预热、TTL 设置和热点 key，避免缓存失效放大数据库压力。"),
		anomalyFinding("mq_backlog", "high", "MQ 堆积量达到 4200，异步链路正在持续积压。", "检查消费者吞吐、消费报错和下游依赖阻塞，必要时扩容消费者。"),
	}

	beforeMetrics := appendMetricSlices(releaseBaselineMetrics, alertWindowMetrics)
	afterMetrics := appendMetricSlices(releaseBaselineMetrics, alertWindowMetrics, afterRollbackMetrics)

	return ScenarioData{
		CurrentRevision: "order-api@2026.04.16-2210",
		TargetRevision:  "order-api@2026.04.16-2150",
		ChangeItems: []EvidenceItem{
			{
				SourceRef:  "deploy/order-api@2026.04.16-2210",
				Summary:    "No relevant order-api change in the last 2 hours.",
				Confidence: 0.93,
			},
		},
		LogItems: []EvidenceItem{
			{
				SourceRef:  "logs/order-api#inventory-timeouts",
				Summary:    "order-api errors are dominated by timeouts when calling inventory-service.",
				Confidence: 0.96,
			},
			{
				SourceRef:  "logs/order-api#error-cluster-2",
				Summary:    "The top error pattern is upstream timeout rather than local configuration failure.",
				Confidence: 0.91,
			},
		},
		DependencyItems: []EvidenceItem{
			{
				SourceRef:  "dep/inventory-service#pool-exhaustion",
				Summary:    "inventory-service is degraded with database pool exhaustion; downstream propagation is likely.",
				Confidence: 0.97,
			},
			{
				SourceRef:  "dep/inventory-service#db-pool",
				Summary:    "inventory-service depends on a saturated database connection pool.",
				Confidence: 0.92,
			},
		},
		VerificationBefore: VerificationResult{
			Status:             "not_recovered",
			ErrorRate:          8.7,
			P95LatencyMs:       840,
			WindowMinutes:      10,
			QueryRefs:          []string{"promql:error_rate:order-api", "promql:p95_latency:order-api"},
			SignalChecks:       beforeChecks,
			Metrics:            beforeMetrics,
			ReleaseComparisons: releaseComparisons,
			Anomalies:          anomalies,
			DecisionBasis:      "0 of 2 recovery signals passed because the downstream dependency is still degraded, and user/service/middleware indicators remain abnormal.",
			Summary:            "Order-api remains degraded while inventory-service and its storage path are still unhealthy.",
		},
		VerificationAfter: VerificationResult{
			Status:             "not_recovered",
			ErrorRate:          8.2,
			P95LatencyMs:       801,
			WindowMinutes:      10,
			QueryRefs:          []string{"promql:error_rate:order-api", "promql:p95_latency:order-api"},
			SignalChecks:       afterChecks,
			Metrics:            afterMetrics,
			ReleaseComparisons: releaseComparisons,
			Anomalies:          anomalies,
			DecisionBasis:      "0 of 2 recovery signals passed after rollback because the downstream dependency, cache layer, and queue backlog are still unhealthy.",
			Summary:            "Rolling back order-api does not address the downstream fault owner or the remaining middleware pressure.",
		},
	}
}

func appendMetricSlices(slices ...[]MetricObservation) []MetricObservation {
	total := 0
	for _, items := range slices {
		total += len(items)
	}
	combined := make([]MetricObservation, 0, total)
	for _, items := range slices {
		combined = append(combined, items...)
	}
	return combined
}

func metricObservation(
	key string,
	displayName string,
	phase string,
	value float64,
	unit string,
	threshold string,
	abnormal bool,
	summary string,
) MetricObservation {
	return MetricObservation{
		Key:         key,
		DisplayName: displayName,
		Phase:       phase,
		Value:       value,
		Unit:        unit,
		Threshold:   threshold,
		Abnormal:    abnormal,
		SourceMode:  "simulated",
		Summary:     summary,
	}
}

func metricComparison(
	key string,
	displayName string,
	beforeValue float64,
	afterValue float64,
	unit string,
	summary string,
) MetricComparison {
	delta := afterValue - beforeValue
	deltaRatio := 0.0
	if beforeValue != 0 {
		deltaRatio = (delta / beforeValue) * 100
	}
	return MetricComparison{
		Key:         key,
		DisplayName: displayName,
		BeforeValue: beforeValue,
		AfterValue:  afterValue,
		DeltaValue:  delta,
		DeltaRatio:  deltaRatio,
		Unit:        unit,
		Summary:     summary,
	}
}

func anomalyFinding(metricKey string, severity string, description string, handlingSuggestion string) AnomalyFinding {
	return AnomalyFinding{
		MetricKey:          metricKey,
		Severity:           severity,
		Description:        description,
		HandlingSuggestion: handlingSuggestion,
		SourceMode:         "simulated",
	}
}
