CREATE TABLE IF NOT EXISTS incidents (
    id VARCHAR(64) PRIMARY KEY,
    service_name VARCHAR(128) NOT NULL,
    severity VARCHAR(32) NOT NULL,
    alert_summary TEXT NOT NULL,
    playbook_key VARCHAR(128) NULL,
    context_json JSON NULL,
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
    playbook_key VARCHAR(128) NULL,
    idempotency_key VARCHAR(191) NOT NULL,
    action_type VARCHAR(32) NOT NULL,
    target_service VARCHAR(128) NOT NULL,
    executor VARCHAR(128) NOT NULL,
    from_revision VARCHAR(128) NOT NULL,
    to_revision VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL,
    status_detail TEXT NOT NULL,
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

SET @graphops_db = DATABASE();

SET @incidents_playbook_key_sql = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @graphops_db
              AND TABLE_NAME = 'incidents'
              AND COLUMN_NAME = 'playbook_key'
        ),
        'SELECT 1',
        'ALTER TABLE incidents ADD COLUMN playbook_key VARCHAR(128) NULL AFTER alert_summary'
    )
);
PREPARE incidents_playbook_key_stmt FROM @incidents_playbook_key_sql;
EXECUTE incidents_playbook_key_stmt;
DEALLOCATE PREPARE incidents_playbook_key_stmt;

SET @incidents_context_json_sql = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @graphops_db
              AND TABLE_NAME = 'incidents'
              AND COLUMN_NAME = 'context_json'
        ),
        'SELECT 1',
        'ALTER TABLE incidents ADD COLUMN context_json JSON NULL AFTER playbook_key'
    )
);
PREPARE incidents_context_json_stmt FROM @incidents_context_json_sql;
EXECUTE incidents_context_json_stmt;
DEALLOCATE PREPARE incidents_context_json_stmt;

SET @incidents_scenario_key_nullable_sql = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @graphops_db
              AND TABLE_NAME = 'incidents'
              AND COLUMN_NAME = 'scenario_key'
              AND IS_NULLABLE = 'NO'
        ),
        'ALTER TABLE incidents MODIFY COLUMN scenario_key VARCHAR(128) NULL',
        'SELECT 1'
    )
);
PREPARE incidents_scenario_key_nullable_stmt FROM @incidents_scenario_key_nullable_sql;
EXECUTE incidents_scenario_key_nullable_stmt;
DEALLOCATE PREPARE incidents_scenario_key_nullable_stmt;

SET @action_receipts_playbook_key_sql = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @graphops_db
              AND TABLE_NAME = 'action_receipts'
              AND COLUMN_NAME = 'playbook_key'
        ),
        'SELECT 1',
        'ALTER TABLE action_receipts ADD COLUMN playbook_key VARCHAR(128) NULL AFTER incident_id'
    )
);
PREPARE action_receipts_playbook_key_stmt FROM @action_receipts_playbook_key_sql;
EXECUTE action_receipts_playbook_key_stmt;
DEALLOCATE PREPARE action_receipts_playbook_key_stmt;

SET @action_receipts_executor_sql = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @graphops_db
              AND TABLE_NAME = 'action_receipts'
              AND COLUMN_NAME = 'executor'
        ),
        'SELECT 1',
        'ALTER TABLE action_receipts ADD COLUMN executor VARCHAR(128) NOT NULL DEFAULT ''system'' AFTER target_service'
    )
);
PREPARE action_receipts_executor_stmt FROM @action_receipts_executor_sql;
EXECUTE action_receipts_executor_stmt;
DEALLOCATE PREPARE action_receipts_executor_stmt;

SET @action_receipts_from_revision_sql = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @graphops_db
              AND TABLE_NAME = 'action_receipts'
              AND COLUMN_NAME = 'from_revision'
        ),
        'SELECT 1',
        'ALTER TABLE action_receipts ADD COLUMN from_revision VARCHAR(128) NOT NULL DEFAULT '''' AFTER executor'
    )
);
PREPARE action_receipts_from_revision_stmt FROM @action_receipts_from_revision_sql;
EXECUTE action_receipts_from_revision_stmt;
DEALLOCATE PREPARE action_receipts_from_revision_stmt;

SET @action_receipts_to_revision_sql = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @graphops_db
              AND TABLE_NAME = 'action_receipts'
              AND COLUMN_NAME = 'to_revision'
        ),
        'SELECT 1',
        'ALTER TABLE action_receipts ADD COLUMN to_revision VARCHAR(128) NOT NULL DEFAULT '''' AFTER from_revision'
    )
);
PREPARE action_receipts_to_revision_stmt FROM @action_receipts_to_revision_sql;
EXECUTE action_receipts_to_revision_stmt;
DEALLOCATE PREPARE action_receipts_to_revision_stmt;

SET @action_receipts_status_detail_sql = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @graphops_db
              AND TABLE_NAME = 'action_receipts'
              AND COLUMN_NAME = 'status_detail'
        ),
        'SELECT 1',
        'ALTER TABLE action_receipts ADD COLUMN status_detail TEXT NOT NULL AFTER status'
    )
);
PREPARE action_receipts_status_detail_stmt FROM @action_receipts_status_detail_sql;
EXECUTE action_receipts_status_detail_stmt;
DEALLOCATE PREPARE action_receipts_status_detail_stmt;

SET @action_receipts_scenario_key_nullable_sql = (
    SELECT IF(
        EXISTS(
            SELECT 1
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = @graphops_db
              AND TABLE_NAME = 'action_receipts'
              AND COLUMN_NAME = 'scenario_key'
              AND IS_NULLABLE = 'NO'
        ),
        'ALTER TABLE action_receipts MODIFY COLUMN scenario_key VARCHAR(128) NULL',
        'SELECT 1'
    )
);
PREPARE action_receipts_scenario_key_nullable_stmt FROM @action_receipts_scenario_key_nullable_sql;
EXECUTE action_receipts_scenario_key_nullable_stmt;
DEALLOCATE PREPARE action_receipts_scenario_key_nullable_stmt;
