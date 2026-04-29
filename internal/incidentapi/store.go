package incidentapi

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var errIncidentNotFound = errors.New("incident not found")

type MemoryStore struct {
	mu                  sync.RWMutex
	seq                 atomic.Uint64
	eventSeq            atomic.Uint64
	agentRunSeq         atomic.Uint64
	incidents           map[string]*Incident
	eventsByIncident    map[string][]IncidentEvent
	agentRunsByIncident map[string][]AgentRun
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		incidents:           make(map[string]*Incident),
		eventsByIncident:    make(map[string][]IncidentEvent),
		agentRunsByIncident: make(map[string][]AgentRun),
	}
}

func (s *MemoryStore) CreateIncident(req CreateIncidentRequest) (*Incident, error) {
	now := time.Now().UTC()
	id := fmt.Sprintf("inc-%d-%06d", now.UnixNano(), s.seq.Add(1))

	incident := &Incident{
		ID:           id,
		ServiceName:  req.ServiceName,
		Severity:     req.Severity,
		AlertSummary: req.AlertSummary,
		PlaybookKey:  req.PlaybookKey,
		Context:      cloneIncidentContext(req.Context),
		Status:       "created",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.incidents[id] = incident
	return cloneIncident(incident), nil
}

func (s *MemoryStore) GetIncident(id string) (*Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	incident, ok := s.incidents[id]
	if !ok {
		return nil, errIncidentNotFound
	}
	return cloneIncident(incident), nil
}

func (s *MemoryStore) ListIncidents(req ListIncidentsRequest) ([]Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	items := make([]Incident, 0, len(s.incidents))
	for _, incident := range s.incidents {
		if req.ServiceName != "" && incident.ServiceName != req.ServiceName {
			continue
		}
		if req.Status != "" && incident.Status != req.Status {
			continue
		}
		items = append(items, *cloneIncident(incident))
	}

	sort.Slice(items, func(left, right int) bool {
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *MemoryStore) ListEvents(id string) ([]IncidentEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.incidents[id]; !ok {
		return nil, errIncidentNotFound
	}

	items := append([]IncidentEvent(nil), s.eventsByIncident[id]...)
	return items, nil
}

func (s *MemoryStore) ListAgentRuns(id string) ([]AgentRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.incidents[id]; !ok {
		return nil, errIncidentNotFound
	}

	items := append([]AgentRun(nil), s.agentRunsByIncident[id]...)
	return items, nil
}

func (s *MemoryStore) SaveAnalysis(id string, req UpsertAnalysisRequest) (*Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	incident, ok := s.incidents[id]
	if !ok {
		return nil, errIncidentNotFound
	}

	incident.Analysis = &AnalysisSnapshot{
		Evidence:       append([]Evidence(nil), req.Evidence...),
		Hypotheses:     append([]Hypothesis(nil), req.Hypotheses...),
		ProposedAction: req.ProposedAction,
	}

	if req.ProposedAction != nil && req.ProposedAction.RequiresApproval {
		incident.Status = "waiting_for_approval"
	} else {
		incident.Status = "diagnosed"
	}
	incident.UpdatedAt = time.Now().UTC()
	return cloneIncident(incident), nil
}

func (s *MemoryStore) SaveReport(id string, req UpsertReportRequest) (*Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	incident, ok := s.incidents[id]
	if !ok {
		return nil, errIncidentNotFound
	}

	incident.Report = req.Report
	switch {
	case req.Report == nil:
		incident.Status = "diagnosed"
	case req.Report.ActionReceipt == nil:
		incident.Status = "diagnosed"
	case req.Report.ActionReceipt.VerificationStatus == "recovered":
		incident.Status = "recovered"
	default:
		incident.Status = "report_ready"
	}
	incident.UpdatedAt = time.Now().UTC()
	return cloneIncident(incident), nil
}

func (s *MemoryStore) ReviewIncident(id, status string, req ReviewIncidentRequest) (*Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	incident, ok := s.incidents[id]
	if !ok {
		return nil, errIncidentNotFound
	}

	incident.Approval = &ApprovalDecision{
		Status:    status,
		Reviewer:  req.Reviewer,
		Comment:   req.Comment,
		UpdatedAt: time.Now().UTC(),
	}

	switch status {
	case "approved":
		incident.Status = "approved"
	case "rejected":
		incident.Status = "rejected"
	}
	incident.UpdatedAt = time.Now().UTC()
	return cloneIncident(incident), nil
}

func (s *MemoryStore) RecordEvent(id string, req RecordIncidentEventRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.incidents[id]; !ok {
		return errIncidentNotFound
	}
	s.eventsByIncident[id] = append(s.eventsByIncident[id], IncidentEvent{
		ID:         int64(s.eventSeq.Add(1)),
		IncidentID: id,
		EventType:  req.EventType,
		ActorType:  req.ActorType,
		ActorName:  req.ActorName,
		Payload:    cloneAnyMap(req.Payload),
		CreatedAt:  time.Now().UTC(),
	})
	return nil
}

func (s *MemoryStore) RecordAgentRun(id string, req RecordAgentRunRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.incidents[id]; !ok {
		return errIncidentNotFound
	}
	s.agentRunsByIncident[id] = append(s.agentRunsByIncident[id], AgentRun{
		ID:            int64(s.agentRunSeq.Add(1)),
		IncidentID:    id,
		NodeName:      req.NodeName,
		ModelName:     req.ModelName,
		PromptVersion: req.PromptVersion,
		Input:         cloneAnyMap(req.Input),
		Output:        cloneAnyMap(req.Output),
		LatencyMs:     req.LatencyMs,
		Status:        req.Status,
		CheckpointID:  req.CheckpointID,
		CreatedAt:     time.Now().UTC(),
	})
	return nil
}

func cloneIncidentContext(in *IncidentContext) *IncidentContext {
	if in == nil {
		return nil
	}

	out := *in
	if in.Labels != nil {
		out.Labels = make(map[string]string, len(in.Labels))
		for key, value := range in.Labels {
			out.Labels[key] = value
		}
	}
	return &out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}

	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneIncident(in *Incident) *Incident {
	if in == nil {
		return nil
	}

	out := *in
	if in.Context != nil {
		out.Context = cloneIncidentContext(in.Context)
	}
	if in.Analysis != nil {
		analysis := *in.Analysis
		analysis.Evidence = append([]Evidence(nil), in.Analysis.Evidence...)
		analysis.Hypotheses = append([]Hypothesis(nil), in.Analysis.Hypotheses...)
		if in.Analysis.ProposedAction != nil {
			action := *in.Analysis.ProposedAction
			action.EvidenceIDs = append([]string(nil), in.Analysis.ProposedAction.EvidenceIDs...)
			if in.Analysis.ProposedAction.VerificationPolicy != nil {
				policy := *in.Analysis.ProposedAction.VerificationPolicy
				action.VerificationPolicy = &policy
			}
			analysis.ProposedAction = &action
		}
		out.Analysis = &analysis
	}
	if in.Approval != nil {
		approval := *in.Approval
		out.Approval = &approval
	}
	if in.Report != nil {
		report := *in.Report
		if in.Report.ActionReceipt != nil {
			receipt := *in.Report.ActionReceipt
			report.ActionReceipt = &receipt
		}
		out.Report = &report
	}
	return &out
}
