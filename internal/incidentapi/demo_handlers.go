package incidentapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
)

func (s *Server) handleDemoHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/demo", http.StatusTemporaryRedirect)
}

func (s *Server) handleDemoIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/demo" && r.URL.Path != "/demo/" {
		http.NotFound(w, r)
		return
	}

	payload, err := demoStaticFS.ReadFile("demo/index.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load demo page")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *Server) handleDemoMetricsProxy(w http.ResponseWriter, r *http.Request) {
	source := strings.TrimSpace(r.PathValue("source"))
	switch source {
	case "orchestrator":
		s.proxyRequest(w, r, http.MethodGet, s.orchestratorBaseURL+"/metrics", nil, "")
	case "opsgateway":
		s.proxyRequest(w, r, http.MethodGet, s.opsGatewayBaseURL+"/metrics", nil, "")
	default:
		writeError(w, http.StatusBadRequest, "unknown metrics source")
	}
}

func (s *Server) handleDemoConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"reasoner_provider":     s.reasonerProvider,
		"ollama_main_model":     s.ollamaMainModel,
		"ollama_parallel_model": s.ollamaParallelModel,
		"parallel_agents":       []string{"change_agent", "log_agent", "dependency_agent"},
	})
}

func (s *Server) handleDemoRunIncident(w http.ResponseWriter, r *http.Request) {
	incidentID := strings.TrimSpace(r.PathValue("id"))
	if incidentID == "" {
		writeError(w, http.StatusBadRequest, "incident id is required")
		return
	}
	s.proxyRequest(w, r, http.MethodPost, s.orchestratorBaseURL+"/runs/incidents/"+incidentID, nil, "application/json")
}

func (s *Server) handleDemoResumeIncident(w http.ResponseWriter, r *http.Request) {
	incidentID := strings.TrimSpace(r.PathValue("id"))
	if incidentID == "" {
		writeError(w, http.StatusBadRequest, "incident id is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read resume payload")
		return
	}
	s.proxyRequest(
		w,
		r,
		http.MethodPost,
		s.orchestratorBaseURL+"/runs/incidents/"+incidentID+"/resume",
		body,
		"application/json",
	)
}

func (s *Server) proxyRequest(
	w http.ResponseWriter,
	r *http.Request,
	method string,
	targetURL string,
	body []byte,
	contentType string,
) {
	req, err := http.NewRequestWithContext(r.Context(), method, targetURL, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create upstream request")
		return
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream service unavailable")
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		if strings.EqualFold(key, "Content-Length") {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil && !errors.Is(err, io.EOF) {
		return
	}
}
