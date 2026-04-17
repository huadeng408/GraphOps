package incidentapi

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var errIncidentNotFound = errors.New("incident not found")

type MemoryStore struct {
	mu        sync.RWMutex
	seq       atomic.Uint64
	incidents map[string]*Incident
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		incidents: make(map[string]*Incident),
	}
}

func (s *MemoryStore) CreateIncident(req CreateIncidentRequest) (*Incident, error) {
	now := time.Now().UTC()
	id := fmt.Sprintf("inc-%06d", s.seq.Add(1))

	incident := &Incident{
		ID:           id,
		ServiceName:  req.ServiceName,
		Severity:     req.Severity,
		AlertSummary: req.AlertSummary,
		ScenarioKey:  req.ScenarioKey,
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
		incident.Status = "awaiting_approval"
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.incidents[id]; !ok {
		return errIncidentNotFound
	}
	return nil
}

func (s *MemoryStore) RecordAgentRun(id string, req RecordAgentRunRequest) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.incidents[id]; !ok {
		return errIncidentNotFound
	}
	return nil
}

func cloneIncident(in *Incident) *Incident {
	if in == nil {
		return nil
	}

	out := *in
	if in.Analysis != nil {
		analysis := *in.Analysis
		analysis.Evidence = append([]Evidence(nil), in.Analysis.Evidence...)
		analysis.Hypotheses = append([]Hypothesis(nil), in.Analysis.Hypotheses...)
		if in.Analysis.ProposedAction != nil {
			action := *in.Analysis.ProposedAction
			action.EvidenceIDs = append([]string(nil), in.Analysis.ProposedAction.EvidenceIDs...)
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
