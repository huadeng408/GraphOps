package opsgateway

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(db *sql.DB) *MySQLStore {
	return &MySQLStore{db: db}
}

func (s *MySQLStore) QueryChanges(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[req.ScenarioKey]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.ChangeItems...)}, nil
}

func (s *MySQLStore) QueryLogs(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[req.ScenarioKey]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.LogItems...)}, nil
}

func (s *MySQLStore) QueryDependencies(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[req.ScenarioKey]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.DependencyItems...)}, nil
}

func (s *MySQLStore) Rollback(req RollbackRequest) (RollbackResponse, error) {
	ctx := context.Background()

	if existing, err := s.getReceiptByKey(ctx, req.IdempotencyKey); err != nil {
		return RollbackResponse{}, err
	} else if existing != nil {
		return RollbackResponse{Receipt: *existing}, nil
	}

	if _, ok := scenarioData[req.ScenarioKey]; !ok {
		return RollbackResponse{}, errUnknownScenario
	}

	receipt := ActionReceipt{
		ReceiptID:      fmt.Sprintf("receipt-%d", time.Now().UTC().UnixNano()),
		IdempotencyKey: req.IdempotencyKey,
		ActionType:     "rollback",
		TargetService:  req.TargetService,
		Status:         "executed",
		ExecutedAt:     time.Now().UTC(),
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO action_receipts (
			receipt_id, incident_id, scenario_key, idempotency_key, action_type, target_service, status, verification_status, executed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		receipt.ReceiptID,
		req.IncidentID,
		req.ScenarioKey,
		receipt.IdempotencyKey,
		receipt.ActionType,
		receipt.TargetService,
		receipt.Status,
		receipt.VerificationStatus,
		receipt.ExecutedAt,
	)
	if err != nil {
		return RollbackResponse{}, err
	}

	return RollbackResponse{Receipt: receipt}, nil
}

func (s *MySQLStore) Verify(req VerifyRequest) (VerificationResult, error) {
	data, ok := scenarioData[req.ScenarioKey]
	if !ok {
		return VerificationResult{}, errUnknownScenario
	}

	row := s.db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(1) FROM action_receipts WHERE incident_id = ? AND action_type = 'rollback'`,
		req.IncidentID,
	)

	var count int
	if err := row.Scan(&count); err != nil {
		return VerificationResult{}, err
	}
	if count > 0 {
		return data.VerificationAfter, nil
	}
	return data.VerificationBefore, nil
}

func (s *MySQLStore) getReceiptByKey(ctx context.Context, key string) (*ActionReceipt, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT receipt_id, idempotency_key, action_type, target_service, status, executed_at, verification_status
		 FROM action_receipts
		 WHERE idempotency_key = ?`,
		key,
	)

	var receipt ActionReceipt
	if err := row.Scan(
		&receipt.ReceiptID,
		&receipt.IdempotencyKey,
		&receipt.ActionType,
		&receipt.TargetService,
		&receipt.Status,
		&receipt.ExecutedAt,
		&receipt.VerificationStatus,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &receipt, nil
}
