package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/store"
	"example.com/potable-water-pipeline/internal/workflow"
)

// Server exposes the HTTP API for jobs, leases, evidence, samples, incidents,
// retests, reviews, and the terminal verdict, and serves the built frontend at
// the root path. All state is persisted through the workflow service.
type Server struct {
	svc       *workflow.Service
	staticDir string
	start     time.Time
}

// New returns a Server that delegates to the given workflow service and serves
// the frontend built into staticDir.
func New(staticDir string, svc *workflow.Service) *Server {
	return &Server{svc: svc, staticDir: staticDir, start: time.Now()}
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("POST /api/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleGetJob)
	mux.HandleFunc("POST /api/jobs/{id}/lock", s.handleLockJob)
	mux.HandleFunc("GET /api/jobs/{id}/topology", s.handleTopology)
	mux.HandleFunc("GET /api/jobs/{id}/timeline", s.handleTimeline)
	mux.HandleFunc("GET /api/jobs/{id}/measurements", s.handleMeasurements)
	mux.HandleFunc("GET /api/jobs/{id}/samples", s.handleListSamples)

	mux.HandleFunc("POST /api/jobs/{id}/leases/acquire", s.handleAcquireLease)
	mux.HandleFunc("POST /api/jobs/{id}/leases/release", s.handleReleaseLease)

	mux.HandleFunc("POST /api/jobs/{id}/stages/{stage}/evidence", s.handleEvidence)
	mux.HandleFunc("POST /api/jobs/{id}/samples", s.handleCreateSample)

	mux.HandleFunc("POST /api/samples/{id}/custody", s.handleCustody)
	mux.HandleFunc("POST /api/samples/{id}/attempts", s.handleCreateAttempt)
	mux.HandleFunc("POST /api/lab-attempts/{id}/retry", s.handleRetryAttempt)
	mux.HandleFunc("POST /api/lab-attempts/{id}/result", s.handleLabResult)

	mux.HandleFunc("POST /api/jobs/{id}/incidents", s.handleCreateIncident)
	mux.HandleFunc("GET /api/jobs/{id}/retests", s.handleListRetests)
	mux.HandleFunc("POST /api/jobs/{id}/retests/{retestID}/start", s.handleStartTreatment)
	mux.HandleFunc("POST /api/jobs/{id}/reviews", s.handleReview)
	mux.HandleFunc("POST /api/jobs/{id}/terminal", s.handleTerminal)

	mux.Handle("GET /", s.handleStatic())
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.svc.ListJobs()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"uptime_s":  int64(time.Since(s.start).Seconds()),
		"jobs":      len(jobs),
		"component": "potable-water-pipeline",
	})
}

// handleStatic serves the built frontend, falling back to index.html.
func (s *Server) handleStatic() http.Handler {
	dir := s.staticDir
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		full := filepath.Join(dir, filepath.Clean("/"+path))
		if info, err := os.Stat(full); err != nil || info.IsDir() {
			full = filepath.Join(dir, "index.html")
		}
		http.ServeFile(w, r, full)
	})
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError maps domain and store errors to stable HTTP status codes.
func writeError(w http.ResponseWriter, err error) {
	var val *domain.ValidationError
	if errors.As(err, &val) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "reason": val.Error()})
		return
	}
	var conflict *domain.ConflictError
	if errors.As(err, &conflict) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "conflict", "code": string(conflict.Code), "reason": conflict.Reason})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal", "reason": err.Error()})
}

// decodeBody decodes a JSON request body, rejecting empty or malformed input.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json", "reason": err.Error()})
		return false
	}
	return true
}
