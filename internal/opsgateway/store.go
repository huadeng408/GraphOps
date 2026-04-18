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
		rolledBackByInc: make(map[string]bool),
		redis:           redis,
	}
}

func (s *Store) QueryChanges(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[req.ScenarioKey]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.ChangeItems...)}, nil
}

func (s *Store) QueryLogs(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[req.ScenarioKey]
	if !ok {
		return QueryResponse{}, errUnknownScenario
	}
	return QueryResponse{Items: append([]EvidenceItem(nil), data.LogItems...)}, nil
}

func (s *Store) QueryDependencies(req QueryRequest) (QueryResponse, error) {
	data, ok := scenarioData[req.ScenarioKey]
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

	if _, ok := scenarioData[req.ScenarioKey]; !ok {
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
		Status:         "executed",
		ExecutedAt:     time.Now().UTC(),
	}

	s.receiptsByKey[req.IdempotencyKey] = receipt
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := scenarioData[req.ScenarioKey]
	if !ok {
		return VerificationResult{}, errUnknownScenario
	}

	if s.rolledBackByInc[req.IncidentID] {
		recordVerificationStatus(data.VerificationAfter.Status)
		return data.VerificationAfter, nil
	}
	recordVerificationStatus(data.VerificationBefore.Status)
	return data.VerificationBefore, nil
}
