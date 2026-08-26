package server

import (
	"net/http"

	"example.com/potable-water-pipeline/internal/domain"
)

// leaseRequest is the body for acquiring or releasing a resource lease.
type leaseRequest struct {
	Resource string `json:"resource"`
	Holder   string `json:"holder"`
	Clock    int64  `json:"clock"`
	Expires  int64  `json:"expires,omitempty"`
}

func (s *Server) handleAcquireLease(w http.ResponseWriter, r *http.Request) {
	var req leaseRequest
	if !decodeBody(w, r, &req) {
		return
	}
	lease, err := s.svc.AcquireLease(domain.JobID(r.PathValue("id")), req.Resource, req.Holder, req.Clock, req.Expires)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, lease)
}

func (s *Server) handleReleaseLease(w http.ResponseWriter, r *http.Request) {
	var req leaseRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if err := s.svc.ReleaseLease(domain.JobID(r.PathValue("id")), req.Resource, req.Holder, req.Clock); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"released": req.Resource})
}

func (s *Server) handleEvidence(w http.ResponseWriter, r *http.Request) {
	var ev domain.Evidence
	if !decodeBody(w, r, &ev) {
		return
	}
	ev.JobID = domain.JobID(r.PathValue("id"))
	ev.Stage = domain.Stage(r.PathValue("stage"))
	if !ev.Stage.Valid() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "reason": "unknown stage"})
		return
	}
	result, err := s.svc.SubmitEvidence(ev)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
