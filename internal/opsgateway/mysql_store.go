package opsgateway

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type MySQLStore struct {
	db    *sql.DB
	redis redisClient
}

func NewMySQLStore(db *sql.DB, redisClients ...*redis.Client) *MySQLStore {
	var redis redisClient
	if len(redisClients) > 0 && redisClients[0] != nil {
		redis = redisClients[0]
	}
	return &MySQLStore{db: db, redis: redis}
}

func (s *MySQLStore) QueryChanges(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[resolvePlaybookKey(req.PlaybookKey)]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.ChangeItems...)}, nil
}

func (s *MySQLStore) QueryLogs(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[resolvePlaybookKey(req.PlaybookKey)]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.LogItems...)}, nil
}

func (s *MySQLStore) QueryDependencies(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[resolvePlaybookKey(req.PlaybookKey)]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.DependencyItems...)}, nil
}

func (s *MySQLStore) Rollback(req RollbackRequest) (RollbackResponse, error) {
	ctx := context.Background()

	var redisKey string
	if s.redis != nil {
		redisKey = rollbackRedisKey(req)
		locked, err := s.redis.SetNX(ctx, redisKey, "pending", rollbackIdempotencyTTL).Result()
		if err == nil && !locked {
			if existing, dbErr := s.getReceiptByKey(ctx, req.IdempotencyKey); dbErr == nil && existing != nil {
				recordRollbackResult("idempotent")
				return RollbackResponse{Receipt: *existing}, nil
			}
		}
	}

	if existing, err := s.getReceiptByKey(ctx, req.IdempotencyKey); err != nil {
		if s.redis != nil && redisKey != "" {
			_ = s.redis.Del(ctx, redisKey).Err()
		}
		recordRollbackResult("db_error")
		return RollbackResponse{}, err
	} else if existing != nil {
		recordRollbackResult("idempotent")
		return RollbackResponse{Receipt: *existing}, nil
	}

	data, ok := scenarioData[resolvePlaybookKey(req.PlaybookKey)]
	if !ok {
		if s.redis != nil && redisKey != "" {
			_ = s.redis.Del(ctx, redisKey).Err()
		}
		recordRollbackResult("scenario_not_found")
		return RollbackResponse{}, errUnknownScenario
	}

	receipt := ActionReceipt{
		ReceiptID:      fmt.Sprintf("receipt-%d", time.Now().UTC().UnixNano()),
		IdempotencyKey: req.IdempotencyKey,
		ActionType:     "rollback",
		TargetService:  req.TargetService,
		Executor:       req.RequestedBy,
		FromRevision:   firstNonEmpty(req.CurrentRevision, data.CurrentRevision),
		ToRevision:     firstNonEmpty(req.TargetRevision, data.TargetRevision),
		Status:         "executed",
		StatusDetail:   "Mock rollback adapter executed the revert workflow and persisted the receipt.",
		ExecutedAt:     time.Now().UTC(),
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO action_receipts (
			receipt_id, incident_id, playbook_key, idempotency_key, action_type, target_service, executor, from_revision, to_revision, status, status_detail, verification_status, executed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		receipt.ReceiptID,
		req.IncidentID,
		resolvePlaybookKey(req.PlaybookKey),
		receipt.IdempotencyKey,
		receipt.ActionType,
		receipt.TargetService,
		receipt.Executor,
		receipt.FromRevision,
		receipt.ToRevision,
		receipt.Status,
		receipt.StatusDetail,
		receipt.VerificationStatus,
		receipt.ExecutedAt,
	)
	if err != nil {
		if s.redis != nil && redisKey != "" {
			_ = s.redis.Del(ctx, redisKey).Err()
		}
		recordRollbackResult("db_error")
		return RollbackResponse{}, err
	}

	if s.redis != nil && redisKey != "" {
		if err := s.redis.Set(ctx, redisKey, receipt.ReceiptID, rollbackIdempotencyTTL).Err(); err != nil {
			_ = s.redis.Del(ctx, redisKey).Err()
		}
	}
	recordRollbackResult("executed")

	return RollbackResponse{Receipt: receipt}, nil
}

func (s *MySQLStore) Verify(req VerifyRequest) (VerificationResult, error) {
	ctx := context.Background()

	data, ok := scenarioData[resolvePlaybookKey(req.PlaybookKey)]
	if !ok {
		return VerificationResult{}, errUnknownScenario
	}

	row := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(1) FROM action_receipts WHERE incident_id = ? AND action_type = 'rollback'`,
		req.IncidentID,
	)

	var count int
	if err := row.Scan(&count); err != nil {
		return VerificationResult{}, err
	}
	if count > 0 {
		if _, err := s.db.ExecContext(
			ctx,
			`UPDATE action_receipts
			 SET verification_status = ?
			 WHERE incident_id = ? AND action_type = 'rollback'`,
			data.VerificationAfter.Status,
			req.IncidentID,
		); err != nil {
			return VerificationResult{}, err
		}
		recordVerificationStatus(data.VerificationAfter.Status)
		return data.VerificationAfter, nil
	}
	recordVerificationStatus(data.VerificationBefore.Status)
	return data.VerificationBefore, nil
}

func (s *MySQLStore) getReceiptByKey(ctx context.Context, key string) (*ActionReceipt, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT receipt_id, idempotency_key, action_type, target_service, executor, from_revision, to_revision, status, status_detail, executed_at, verification_status
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
		&receipt.Executor,
		&receipt.FromRevision,
		&receipt.ToRevision,
		&receipt.Status,
		&receipt.StatusDetail,
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
