package opsgateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

var errUnknownScenario = errors.New("unknown scenario")

type Store struct {
	mu              sync.RWMutex
	receiptSeq      atomic.Uint64
	receiptsByKey   map[string]ActionReceipt
	receiptKeyByInc map[string]string
	rolledBackByInc map[string]bool
	redis           redisClient
}

func NewStore(redisClients ...*redis.Client) *Store {
	var redis redisClient
	if len(redisClients) > 0 && redisClients[0] != nil {
		redis = redisClients[0]
	}
	return &Store{
		receiptsByKey:   make(map[string]ActionReceipt),
		receiptKeyByInc: make(map[string]string),
		rolledBackByInc: make(map[string]bool),
		redis:           redis,
	}
}

func (s *Store) QueryChanges(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[resolvePlaybookKey(req.PlaybookKey)]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.ChangeItems...)}, nil
}

func (s *Store) QueryLogs(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[resolvePlaybookKey(req.PlaybookKey)]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.LogItems...)}, nil
}

func (s *Store) QueryDependencies(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[resolvePlaybookKey(req.PlaybookKey)]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.DependencyItems...)}, nil
}

func (s *Store) Rollback(req RollbackRequest) (RollbackResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if receipt, ok := s.receiptsByKey[req.IdempotencyKey]; ok {
		recordRollbackResult("idempotent")
		return RollbackResponse{Receipt: receipt}, nil
	}

	data, ok := scenarioData[resolvePlaybookKey(req.PlaybookKey)]
	if !ok {
		recordRollbackResult("scenario_not_found")
		return RollbackResponse{}, errUnknownScenario
	}

	var redisKey string
	if s.redis != nil {
		redisKey = rollbackRedisKey(req)
		locked, err := s.redis.SetNX(context.Background(), redisKey, "pending", rollbackIdempotencyTTL).Result()
		if err == nil && !locked {
			if receipt, ok := s.receiptsByKey[req.IdempotencyKey]; ok {
				recordRollbackResult("idempotent")
				return RollbackResponse{Receipt: receipt}, nil
			}
		}
	}

	receipt := ActionReceipt{
		ReceiptID:      fmt.Sprintf("receipt-%06d", s.receiptSeq.Add(1)),
		IdempotencyKey: req.IdempotencyKey,
		ActionType:     "rollback",
		TargetService:  req.TargetService,
		Executor:       req.RequestedBy,
		FromRevision:   firstNonEmpty(req.CurrentRevision, data.CurrentRevision),
		ToRevision:     firstNonEmpty(req.TargetRevision, data.TargetRevision),
		Status:         "executed",
		StatusDetail:   "Mock rollback adapter executed the revert workflow and returned a deterministic receipt.",
		ExecutedAt:     time.Now().UTC(),
	}

	s.receiptsByKey[req.IdempotencyKey] = receipt
	s.receiptKeyByInc[req.IncidentID] = req.IdempotencyKey
	s.rolledBackByInc[req.IncidentID] = true
	if s.redis != nil && redisKey != "" {
		if err := s.redis.Set(context.Background(), redisKey, receipt.ReceiptID, rollbackIdempotencyTTL).Err(); err != nil {
			_ = s.redis.Del(context.Background(), redisKey).Err()
		}
	}
	recordRollbackResult("executed")

	return RollbackResponse{Receipt: receipt}, nil
}

func (s *Store) Verify(req VerifyRequest) (VerificationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, ok := scenarioData[resolvePlaybookKey(req.PlaybookKey)]
	if !ok {
		return VerificationResult{}, errUnknownScenario
	}

	if s.rolledBackByInc[req.IncidentID] {
		if key, ok := s.receiptKeyByInc[req.IncidentID]; ok {
			if receipt, ok := s.receiptsByKey[key]; ok {
				receipt.VerificationStatus = data.VerificationAfter.Status
				s.receiptsByKey[key] = receipt
			}
		}
		recordVerificationStatus(data.VerificationAfter.Status)
		recordVerificationSnapshot(req.ServiceName, data.VerificationAfter)
		return data.VerificationAfter, nil
	}
	recordVerificationStatus(data.VerificationBefore.Status)
	recordVerificationSnapshot(req.ServiceName, data.VerificationBefore)
	return data.VerificationBefore, nil
}
