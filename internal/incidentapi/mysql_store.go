package incidentapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
	contextPayload, err := marshalJSON(req.Context)
	if err != nil {
		return nil, err
	}

	incident := &Incident{
		ID:           fmt.Sprintf("inc-%d", now.UnixNano()),
		ServiceName:  req.ServiceName,
		Severity:     req.Severity,
		AlertSummary: req.AlertSummary,
		PlaybookKey:  req.PlaybookKey,
		Context:      cloneIncidentContext(req.Context),
		Status:       "created",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	_, err = s.db.ExecContext(
		context.Background(),
		`INSERT INTO incidents (
			id, service_name, severity, alert_summary, playbook_key, context_json, status, analysis_json, report_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
		incident.ID,
		incident.ServiceName,
		incident.Severity,
		incident.AlertSummary,
		incident.PlaybookKey,
		contextPayload,
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
		`SELECT id, service_name, severity, alert_summary, playbook_key, context_json, status, analysis_json, report_json, created_at, updated_at
		 FROM incidents
		 WHERE id = ?`,
		id,
	)

	var incident Incident
	var contextJSON sql.NullString
	var analysisJSON sql.NullString
	var reportJSON sql.NullString

	if err := row.Scan(
		&incident.ID,
		&incident.ServiceName,
		&incident.Severity,
		&incident.AlertSummary,
		&incident.PlaybookKey,
		&contextJSON,
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

	if contextJSON.Valid && contextJSON.String != "" {
		var incidentContext IncidentContext
		if err := json.Unmarshal([]byte(contextJSON.String), &incidentContext); err != nil {
			return nil, err
		}
		incident.Context = &incidentContext
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

func (s *MySQLStore) ListIncidents(req ListIncidentsRequest) ([]Incident, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id FROM incidents`
	args := make([]any, 0, 3)
	conditions := make([]string, 0, 2)
	if req.ServiceName != "" {
		conditions = append(conditions, "service_name = ?")
		args = append(args, req.ServiceName)
	}
	if req.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, req.Status)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Incident, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		incident, err := s.GetIncident(id)
		if err != nil {
			return nil, err
		}
		items = append(items, *incident)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *MySQLStore) ListEvents(id string) ([]IncidentEvent, error) {
	rows, err := s.db.QueryContext(
		context.Background(),
		`SELECT id, incident_id, event_type, actor_type, actor_name, payload_json, created_at
		 FROM incident_events
		 WHERE incident_id = ?
		 ORDER BY id ASC`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]IncidentEvent, 0)
	for rows.Next() {
		var item IncidentEvent
		var payloadJSON string
		if err := rows.Scan(
			&item.ID,
			&item.IncidentID,
			&item.EventType,
			&item.ActorType,
			&item.ActorName,
			&payloadJSON,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if payloadJSON != "" {
			if err := json.Unmarshal([]byte(payloadJSON), &item.Payload); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := s.GetIncident(id); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *MySQLStore) ListAgentRuns(id string) ([]AgentRun, error) {
	rows, err := s.db.QueryContext(
		context.Background(),
		`SELECT id, incident_id, node_name, model_name, prompt_version, input_json, output_json, latency_ms, status, checkpoint_id, created_at
		 FROM agent_runs
		 WHERE incident_id = ?
		 ORDER BY id ASC`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AgentRun, 0)
	for rows.Next() {
		var item AgentRun
		var inputJSON string
		var outputJSON string
		if err := rows.Scan(
			&item.ID,
			&item.IncidentID,
			&item.NodeName,
			&item.ModelName,
			&item.PromptVersion,
			&inputJSON,
			&outputJSON,
			&item.LatencyMs,
			&item.Status,
			&item.CheckpointID,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if inputJSON != "" {
			if err := json.Unmarshal([]byte(inputJSON), &item.Input); err != nil {
				return nil, err
			}
		}
		if outputJSON != "" {
			if err := json.Unmarshal([]byte(outputJSON), &item.Output); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := s.GetIncident(id); err != nil {
		return nil, err
	}
	return items, nil
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
		status = "waiting_for_approval"
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

func marshalJSON(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(payload), nil
}
