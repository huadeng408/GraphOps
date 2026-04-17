package incidentapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type Server struct {
	store Repository
}

func NewServer(store Repository) *Server {
	return &Server{store: store}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /incidents", s.handleCreateIncident)
	mux.HandleFunc("GET /incidents/{id}", s.handleGetIncident)
	mux.HandleFunc("POST /incidents/{id}/approve", s.handleApproveIncident)
	mux.HandleFunc("POST /incidents/{id}/reject", s.handleRejectIncident)
	mux.HandleFunc("GET /incidents/{id}/report", s.handleGetReport)
	mux.HandleFunc("POST /internal/incidents/{id}/analysis", s.handleSaveAnalysis)
	mux.HandleFunc("POST /internal/incidents/{id}/report", s.handleSaveReport)
	mux.HandleFunc("POST /internal/incidents/{id}/events", s.handleRecordEvent)
	mux.HandleFunc("POST /internal/incidents/{id}/agent-runs", s.handleRecordAgentRun)
	return mux
}

func (s *Server) handleCreateIncident(w http.ResponseWriter, r *http.Request) {
	var req CreateIncidentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.ServiceName == "" || req.Severity == "" || req.AlertSummary == "" || req.ScenarioKey == "" {
		writeError(w, http.StatusBadRequest, "service_name, severity, alert_summary, scenario_key are required")
		return
	}

	incident, err := s.store.CreateIncident(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, incident)
}

func (s *Server) handleGetIncident(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	incident, err := s.store.GetIncident(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (s *Server) handleApproveIncident(w http.ResponseWriter, r *http.Request) {
	s.handleReviewIncident(w, r, "approved")
}

func (s *Server) handleRejectIncident(w http.ResponseWriter, r *http.Request) {
	s.handleReviewIncident(w, r, "rejected")
}

func (s *Server) handleReviewIncident(w http.ResponseWriter, r *http.Request, status string) {
	id := r.PathValue("id")

	var req ReviewIncidentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Reviewer) == "" {
		writeError(w, http.StatusBadRequest, "reviewer is required")
		return
	}

	incident, err := s.store.ReviewIncident(id, status, req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (s *Server) handleSaveAnalysis(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req UpsertAnalysisRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	incident, err := s.store.SaveAnalysis(id, req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (s *Server) handleSaveReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req UpsertReportRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	incident, err := s.store.SaveReport(id, req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	incident, err := s.store.GetIncident(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if incident.Report == nil {
		writeError(w, http.StatusNotFound, "report not found")
		return
	}
	writeJSON(w, http.StatusOK, incident.Report)
}

func (s *Server) handleRecordEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req RecordIncidentEventRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.EventType) == "" || strings.TrimSpace(req.ActorType) == "" || strings.TrimSpace(req.ActorName) == "" {
		writeError(w, http.StatusBadRequest, "event_type, actor_type, actor_name are required")
		return
	}
	if err := s.store.RecordEvent(id, req); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
}

func (s *Server) handleRecordAgentRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req RecordAgentRunRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.NodeName) == "" || strings.TrimSpace(req.ModelName) == "" || strings.TrimSpace(req.PromptVersion) == "" {
		writeError(w, http.StatusBadRequest, "node_name, model_name, prompt_version are required")
		return
	}
	if strings.TrimSpace(req.Status) == "" {
		writeError(w, http.StatusBadRequest, "status is required")
		return
	}
	if err := s.store.RecordAgentRun(id, req); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, errIncidentNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}
