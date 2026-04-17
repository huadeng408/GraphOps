CREATE TABLE IF NOT EXISTS incidents (
    id VARCHAR(64) PRIMARY KEY,
    service_name VARCHAR(128) NOT NULL,
    severity VARCHAR(32) NOT NULL,
    alert_summary TEXT NOT NULL,
    scenario_key VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL,
    analysis_json JSON NULL,
    report_json JSON NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL
);

CREATE TABLE IF NOT EXISTS approvals (
    incident_id VARCHAR(64) PRIMARY KEY,
    status VARCHAR(32) NOT NULL,
    reviewer VARCHAR(128) NOT NULL,
    comment TEXT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT fk_approvals_incident
        FOREIGN KEY (incident_id) REFERENCES incidents(id)
        ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS evidence_items (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    incident_id VARCHAR(64) NOT NULL,
    evidence_id VARCHAR(64) NOT NULL,
    source_type VARCHAR(32) NOT NULL,
    source_ref VARCHAR(255) NOT NULL,
    summary TEXT NOT NULL,
    confidence DOUBLE NOT NULL,
    created_at DATETIME(6) NOT NULL,
    KEY idx_evidence_incident (incident_id),
    KEY idx_evidence_source (source_type),
    CONSTRAINT fk_evidence_incident
        FOREIGN KEY (incident_id) REFERENCES incidents(id)
        ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS action_receipts (
    receipt_id VARCHAR(64) PRIMARY KEY,
    incident_id VARCHAR(64) NOT NULL,
    scenario_key VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(191) NOT NULL,
    action_type VARCHAR(32) NOT NULL,
    target_service VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL,
    verification_status VARCHAR(32) NOT NULL,
    executed_at DATETIME(6) NOT NULL,
    UNIQUE KEY uniq_action_idempotency (idempotency_key),
    KEY idx_action_incident (incident_id)
);

CREATE TABLE IF NOT EXISTS memory_cases (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    service_name VARCHAR(128) NOT NULL,
    incident_type VARCHAR(128) NOT NULL,
    summary TEXT NOT NULL,
    action_taken TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS incident_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    incident_id VARCHAR(64) NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    actor_type VARCHAR(32) NOT NULL,
    actor_name VARCHAR(128) NOT NULL,
    payload_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL,
    KEY idx_incident_events_incident (incident_id),
    CONSTRAINT fk_incident_events_incident
        FOREIGN KEY (incident_id) REFERENCES incidents(id)
        ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_runs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    incident_id VARCHAR(64) NOT NULL,
    node_name VARCHAR(128) NOT NULL,
    model_name VARCHAR(128) NOT NULL,
    prompt_version VARCHAR(64) NOT NULL,
    input_json JSON NOT NULL,
    output_json JSON NOT NULL,
    latency_ms BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL,
    checkpoint_id VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    KEY idx_agent_runs_incident (incident_id),
    KEY idx_agent_runs_node (node_name),
    CONSTRAINT fk_agent_runs_incident
        FOREIGN KEY (incident_id) REFERENCES incidents(id)
        ON DELETE CASCADE
);
