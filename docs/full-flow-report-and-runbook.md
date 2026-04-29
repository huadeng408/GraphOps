# GraphOps 全流程运行手册与报告

这份文档做两件事：

1. 记录最近一次已经验证成功的“故障自动诊断 → 根因分析 → 自动执行恢复动作 → 验证恢复结果 → 生成报告”全流程结果。
2. 给出你下次复跑这条链路时最省事的方式，包括一键脚本和手动命令。

## 一键运行

推荐直接执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\run-full-flow.ps1
```

脚本会自动完成：

1. 启动 `redis / prometheus / grafana / mysql`
2. 启动 `incident-api / ops-gateway / orchestrator`
3. 执行回放场景
4. 自动审批主场景回滚
5. 拉取 incident、report、Prometheus 指标
6. 导出原始事件数据
7. 在 `logs\full-flow-时间戳\run-report.md` 生成 Markdown 报告

### 常用参数

```powershell
# 默认：rules 模式、双场景、启用 MySQL、自动审批主场景
powershell -ExecutionPolicy Bypass -File .\scripts\run-full-flow.ps1

# 只跑主场景
powershell -ExecutionPolicy Bypass -File .\scripts\run-full-flow.ps1 -Scenario main

# 只跑副场景
powershell -ExecutionPolicy Bypass -File .\scripts\run-full-flow.ps1 -Scenario secondary

# 使用 Ollama
powershell -ExecutionPolicy Bypass -File .\scripts\run-full-flow.ps1 -ReasonerProvider ollama -OllamaModel qwen3:4b
```

## 如果你想手动执行一次完整流程

### 1. 启动依赖

```powershell
docker compose up -d redis prometheus grafana mysql
```

### 2. 启动应用服务

```powershell
$env:REDIS_URL="redis://127.0.0.1:6379/0"
$env:REASONER_PROVIDER="rules"
.\scripts\dev.ps1 -UseMySQL
```

### 3. 执行完整回放

```powershell
.\scripts\replay.ps1 -Scenario both -ApproveMain
```

这条命令会做两件事：

- 主场景 `release_config_regression`
  - 创建 incident
  - 自动触发诊断
  - 进入审批
  - 自动批准回滚
  - 执行恢复验证
  - 生成最终报告
- 副场景 `downstream_inventory_outage`
  - 创建 incident
  - 自动触发诊断
  - 不执行回滚
  - 只生成诊断报告

### 4. 看结果

- incident-api: `http://127.0.0.1:8082`
- ops-gateway: `http://127.0.0.1:8085`
- orchestrator: `http://127.0.0.1:8090`
- Prometheus: `http://127.0.0.1:9090`
- Grafana: `http://127.0.0.1:3000`

Grafana 用户名密码：

- 用户名：`admin`
- 密码：`admin`

重点看板：

- `GraphOps Incident Overview`
- `GraphOps Agent Runtime`

## 最近一次已验证成功的结果

以下结果来自最近一次已经实际跑通并校验过 Grafana 的演示：

- 运行模式：`rules`
- 持久化：`MySQL + Redis + SQLite checkpoint`
- 可观测：`Prometheus + Grafana`

### 主场景

- 事件 ID：`inc-1777125707059430800`
- 场景：`release_config_regression`
- 告警：`5xx spike after deploy`
- 初始状态：`waiting_for_approval`
- 最终状态：`recovered`

根因结论：

- 最新发布引入了 `order-api` 的数据库连接配置回归。

恢复动作：

- 对 `order-api` 执行 `rollback`

恢复验证：

- `5xx` 降到 `0.3%`
- `P95` 降到 `118ms`
- `action_receipt.verification_status = recovered`

### 副场景

- 事件 ID：`inc-1777125712925933000`
- 场景：`downstream_inventory_outage`
- 告警：`timeouts to inventory`
- 最终状态：`diagnosed`

根因结论：

- 主故障位于 `inventory-service`，`order-api` 只是症状承载者。

处置决策：

- 不回滚
- 输出诊断报告并建议人工继续排查下游

## 已确认可观测结果

最近一次成功验证时，Grafana 看板上已经出现以下观测结果：

- `Incident Runs by Status`
  - `completed`
  - `waiting_for_approval`
- `Approval Wait p95`
  - `4.75 ms`
- `Rollback Requests`
  - `executed`
- `Recovery Verification Outcomes`
  - `recovered`

`GraphOps Agent Runtime` 也已验证通过：

- `Graph Node p95` 有数据
- `Tool Call p95` 有数据

## 报告和原始数据会落到哪里

执行 `scripts\run-full-flow.ps1` 后，会在如下目录生成一整套产物：

```text
logs\full-flow-<timestamp>\
```

其中通常会包含：

- `run-report.md`
- `replay-result.json`
- `incident-*.json`
- `report-*.json`
- `metric-incident-runs.json`
- `metric-rollback-requests.json`
- `metric-recovery-verification.json`
- `db-incidents.tsv`
- `db-approvals.tsv`
- `db-action-receipts.tsv`
- `db-incident-events.tsv`
- `db-agent-runs.tsv`
- `db-evidence-items.tsv`

这套输出已经足够覆盖：

- 诊断输入
- 根因分析结果
- 回滚执行回执
- 恢复验证结果
- 事件时间线
- Agent 运行审计
- Prometheus 指标快照

## 已修复的已知问题

为了保证你在 Grafana 上看到的不是 `No data`，当前仓库已经修复了一个 datasource 配置问题：

- 文件：`grafana/provisioning/datasources/datasource.yml`
- 修复点：固定 `Prometheus` datasource 的 `uid: prometheus`

否则 dashboard 引用的数据源 UID 对不上，会导致 Grafana 面板无数据。

## 建议操作

如果你的目标是“看一次完整链路是否真的跑通”，建议优先执行一键脚本：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\run-full-flow.ps1
```

如果你的目标是“理解每一步到底发生了什么”，就按上面的手动步骤跑，并结合以下两个地方一起看：

- Grafana 看板
- `logs\full-flow-时间戳\run-report.md`
