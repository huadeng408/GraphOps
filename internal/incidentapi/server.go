package incidentapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	store               Repository
	orchestratorBaseURL string
	opsGatewayBaseURL   string
	httpClient          *http.Client
	reasonerProvider    string
	ollamaMainModel     string
	ollamaParallelModel string
}

func NewServer(store Repository) *Server {
	return &Server{
		store:               store,
		orchestratorBaseURL: strings.TrimRight(envOrDefault("ORCHESTRATOR_URL", "http://127.0.0.1:8090"), "/"),
		opsGatewayBaseURL:   strings.TrimRight(envOrDefault("OPS_GATEWAY_URL", "http://127.0.0.1:8085"), "/"),
		reasonerProvider:    envOrDefault("REASONER_PROVIDER", "ollama"),
		ollamaMainModel:     envOrDefault("OLLAMA_MAIN_MODEL", envOrDefault("OLLAMA_MODEL", "qwen3:4b")),
		ollamaParallelModel: envOrDefault("OLLAMA_PARALLEL_MODEL", "qwen3:1.7b"),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleDemoHome)
	mux.HandleFunc("GET /demo", s.handleDemoIndex)
	mux.HandleFunc("GET /demo/", s.handleDemoIndex)
	mux.Handle("GET /demo/assets/", demoAssetsHandler())
	mux.HandleFunc("GET /demo/api/config", s.handleDemoConfig)
	mux.HandleFunc("GET /demo/api/metrics/{source}", s.handleDemoMetricsProxy)
	mux.HandleFunc("POST /demo/api/runs/incidents/{id}", s.handleDemoRunIncident)
	mux.HandleFunc("POST /demo/api/runs/incidents/{id}/resume", s.handleDemoResumeIncident)
	mux.HandleFunc("POST /incidents", s.handleCreateIncident)
	mux.HandleFunc("GET /incidents", s.handleListIncidents)
	mux.HandleFunc("GET /incidents/{id}", s.handleGetIncident)
	mux.HandleFunc("GET /incidents/{id}/events", s.handleListIncidentEvents)
	mux.HandleFunc("GET /incidents/{id}/agent-runs", s.handleListIncidentAgentRuns)
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

	if req.PlaybookKey == "" {
		req.PlaybookKey = "release_config_regression"
	}
	if req.ServiceName == "" || req.Severity == "" || req.AlertSummary == "" {
		writeError(w, http.StatusBadRequest, "service_name, severity, and alert_summary are required")
		return
	}
	if req.Context == nil || strings.TrimSpace(req.Context.Cluster) == "" || strings.TrimSpace(req.Context.Namespace) == "" ||
		strings.TrimSpace(req.Context.ReleaseVersion) == "" || strings.TrimSpace(req.Context.PreviousVersion) == "" ||
		req.Context.AlertStartedAt.IsZero() {
		writeError(w, http.StatusBadRequest, "context with cluster, namespace, alert_started_at, release_version, and previous_version is required")
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

func (s *Server) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	req := ListIncidentsRequest{
		ServiceName: strings.TrimSpace(r.URL.Query().Get("service_name")),
		Status:      strings.TrimSpace(r.URL.Query().Get("status")),
	}
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		req.Limit = limit
	}

	items, err := s.store.ListIncidents(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleListIncidentEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	events, err := s.store.ListEvents(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events})
}

func (s *Server) handleListIncidentAgentRuns(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	runs, err := s.store.ListAgentRuns(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
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

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
