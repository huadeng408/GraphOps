package opsgateway

import (
	"encoding/json"
	"errors"
	"net/http"
)

type Server struct {
	store GatewayStore
}

func NewServer(store GatewayStore) *Server {
	return &Server{store: store}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tools/changes/query", s.handleQueryChanges)
	mux.HandleFunc("POST /tools/logs/query", s.handleQueryLogs)
	mux.HandleFunc("POST /tools/dependency/query", s.handleQueryDependencies)
	mux.HandleFunc("POST /actions/rollback", s.handleRollback)
	mux.HandleFunc("POST /actions/verify", s.handleVerify)
	return mux
}

func (s *Server) handleQueryChanges(w http.ResponseWriter, r *http.Request) {
	s.handleQuery(w, r, s.store.QueryChanges)
}

func (s *Server) handleQueryLogs(w http.ResponseWriter, r *http.Request) {
	s.handleQuery(w, r, s.store.QueryLogs)
}

func (s *Server) handleQueryDependencies(w http.ResponseWriter, r *http.Request) {
	s.handleQuery(w, r, s.store.QueryDependencies)
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request, fn func(QueryRequest) (QueryResponse, error)) {
	var req QueryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ServiceName == "" || req.ScenarioKey == "" {
		writeError(w, http.StatusBadRequest, "service_name and scenario_key are required")
		return
	}

	resp, err := fn(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	var req RollbackRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.IncidentID == "" || req.TargetService == "" || req.ScenarioKey == "" || req.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "incident_id, target_service, scenario_key, idempotency_key are required")
		return
	}

	resp, err := s.store.Rollback(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req VerifyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.IncidentID == "" || req.ServiceName == "" || req.ScenarioKey == "" {
		writeError(w, http.StatusBadRequest, "incident_id, service_name, scenario_key are required")
		return
	}

	resp, err := s.store.Verify(req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
	if errors.Is(err, errUnknownScenario) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}
