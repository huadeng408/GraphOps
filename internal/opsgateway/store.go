package opsgateway

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var errUnknownScenario = errors.New("unknown scenario")

type Store struct {
	mu              sync.RWMutex
	receiptSeq      atomic.Uint64
	receiptsByKey   map[string]ActionReceipt
	rolledBackByInc map[string]bool
}

func NewStore() *Store {
	return &Store{
		receiptsByKey:   make(map[string]ActionReceipt),
		rolledBackByInc: make(map[string]bool),
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
		return RollbackResponse{Receipt: receipt}, nil
	}

	if _, ok := scenarioData[req.ScenarioKey]; !ok {
		return RollbackResponse{}, errUnknownScenario
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
		return data.VerificationAfter, nil
	}
	return data.VerificationBefore, nil
}
