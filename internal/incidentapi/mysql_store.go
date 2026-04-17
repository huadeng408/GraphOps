package incidentapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore {
	return &MySQLStore{db: db}
}

func (s *MySQLStore) CreateIncident(req CreateIncidentRequest) (*Incident, error) {
	now := time.Now().UTC()
	incident := &Incident{
		ID:           fmt.Sprintf("inc-%d", now.UnixNano()),
		ServiceName:  req.ServiceName,
		Severity:     req.Severity,
		AlertSummary: req.AlertSummary,
		ScenarioKey:  req.ScenarioKey,
		Status:       "created",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	_, err := s.db.ExecContext(
		context.Background(),
		`INSERT INTO incidents (
			id, service_name, severity, alert_summary, scenario_key, status, analysis_json, report_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
		incident.ID,
		incident.ServiceName,
		incident.Severity,
		incident.AlertSummary,
		incident.ScenarioKey,
		incident.Status,
		incident.CreatedAt,
		incident.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return incident, nil
}

func (s *MySQLStore) GetIncident(id string) (*Incident, error) {
	row := s.db.QueryRowContext(
		context.Background(),
		`SELECT id, service_name, severity, alert_summary, scenario_key, status, analysis_json, report_json, created_at, updated_at
		 FROM incidents
		 WHERE id = ?`,
		id,
	)

	var incident Incident
	var analysisJSON sql.NullString
	var reportJSON sql.NullString

	if err := row.Scan(
		&incident.ID,
		&incident.ServiceName,
		&incident.Severity,
		&incident.AlertSummary,
		&incident.ScenarioKey,
		&incident.Status,
		&analysisJSON,
		&reportJSON,
		&incident.CreatedAt,
		&incident.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, errIncidentNotFound
		}
		return nil, err
	}

	if analysisJSON.Valid && analysisJSON.String != "" {
		var analysis AnalysisSnapshot
		if err := json.Unmarshal([]byte(analysisJSON.String), &analysis); err != nil {
			return nil, err
		}
		incident.Analysis = &analysis
	}

	if reportJSON.Valid && reportJSON.String != "" {
		var report FinalReport
		if err := json.Unmarshal([]byte(reportJSON.String), &report); err != nil {
			return nil, err
		}
		incident.Report = &report
	}

	if approval, err := s.getApproval(id); err != nil {
		return nil, err
	} else {
		incident.Approval = approval
	}

	return &incident, nil
}

func (s *MySQLStore) SaveAnalysis(id string, req UpsertAnalysisRequest) (*Incident, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	status := "diagnosed"
	if req.ProposedAction != nil && req.ProposedAction.RequiresApproval {
		status = "awaiting_approval"
	}

	analysis := AnalysisSnapshot{
		Evidence:       append([]Evidence(nil), req.Evidence...),
		Hypotheses:     append([]Hypothesis(nil), req.Hypotheses...),
		ProposedAction: req.ProposedAction,
	}
	analysisPayload, err := json.Marshal(analysis)
	if err != nil {
		return nil, err
	}

	result, err := tx.ExecContext(
		ctx,
		`UPDATE incidents SET status = ?, analysis_json = ?, updated_at = ? WHERE id = ?`,
		status,
		string(analysisPayload),
		time.Now().UTC(),
		id,
	)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, errIncidentNotFound
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM evidence_items WHERE incident_id = ?`, id); err != nil {
		return nil, err
	}
	for _, item := range req.Evidence {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO evidence_items (incident_id, evidence_id, source_type, source_ref, summary, confidence, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id,
			item.EvidenceID,
			item.SourceType,
			item.SourceRef,
			item.Summary,
			item.Confidence,
			time.Now().UTC(),
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetIncident(id)
}

func (s *MySQLStore) SaveReport(id string, req UpsertReportRequest) (*Incident, error) {
	if req.Report == nil {
		return nil, fmt.Errorf("report is required")
	}

	reportPayload, err := json.Marshal(req.Report)
	if err != nil {
		return nil, err
	}

	status := "diagnosed"
	if req.Report.ActionReceipt != nil {
		if req.Report.ActionReceipt.VerificationStatus == "recovered" {
			status = "recovered"
		} else {
			status = "report_ready"
		}
	}

	result, err := s.db.ExecContext(
		context.Background(),
		`UPDATE incidents SET status = ?, report_json = ?, updated_at = ? WHERE id = ?`,
		status,
		string(reportPayload),
		time.Now().UTC(),
		id,
	)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, errIncidentNotFound
	}

	return s.GetIncident(id)
}

func (s *MySQLStore) ReviewIncident(id, status string, req ReviewIncidentRequest) (*Incident, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(
		ctx,
		`UPDATE incidents SET status = ?, updated_at = ? WHERE id = ?`,
		status,
		time.Now().UTC(),
		id,
	)
	if err != nil {
		return nil, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, errIncidentNotFound
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO approvals (incident_id, status, reviewer, comment, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE status = VALUES(status), reviewer = VALUES(reviewer), comment = VALUES(comment), updated_at = VALUES(updated_at)`,
		id,
		status,
		req.Reviewer,
		req.Comment,
		time.Now().UTC(),
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetIncident(id)
}

func (s *MySQLStore) getApproval(incidentID string) (*ApprovalDecision, error) {
	row := s.db.QueryRowContext(
		context.Background(),
		`SELECT status, reviewer, comment, updated_at FROM approvals WHERE incident_id = ?`,
		incidentID,
	)

	var approval ApprovalDecision
	if err := row.Scan(&approval.Status, &approval.Reviewer, &approval.Comment, &approval.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &approval, nil
}

func (s *MySQLStore) RecordEvent(id string, req RecordIncidentEventRequest) error {
	payload, err := json.Marshal(req.Payload)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(
		context.Background(),
		`INSERT INTO incident_events (
			incident_id, event_type, actor_type, actor_name, payload_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		id,
		req.EventType,
		req.ActorType,
		req.ActorName,
		string(payload),
		time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return errIncidentNotFound
	}
	return nil
}

func (s *MySQLStore) RecordAgentRun(id string, req RecordAgentRunRequest) error {
	inputPayload, err := json.Marshal(req.Input)
	if err != nil {
		return err
	}
	outputPayload, err := json.Marshal(req.Output)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(
		context.Background(),
		`INSERT INTO agent_runs (
			incident_id, node_name, model_name, prompt_version, input_json, output_json, latency_ms, status, checkpoint_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		req.NodeName,
		req.ModelName,
		req.PromptVersion,
		string(inputPayload),
		string(outputPayload),
		req.LatencyMs,
		req.Status,
		req.CheckpointID,
		time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return errIncidentNotFound
	}
	return nil
}
