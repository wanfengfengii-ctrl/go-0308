package server

import (
	"net/http"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/retest"
)

// incidentRequest is the body for creating an incident.
type incidentRequest struct {
	Kind    retest.Kind            `json:"kind"`
	Section domain.SectionID       `json:"section"`
	SameRun []domain.SamplePointID `json:"same_run,omitempty"`
	Clock   int64                  `json:"clock"`
}

// reviewRequest is the body for submitting an independent review.
type reviewRequest struct {
	Person   string `json:"person_id"`
	Approved bool   `json:"approved"`
}

func (s *Server) handleCreateIncident(w http.ResponseWriter, r *http.Request) {
	var req incidentRequest
	if !decodeBody(w, r, &req) {
		return
	}
	inc, rs, err := s.svc.CreateIncident(domain.JobID(r.PathValue("id")), retest.IncidentSeed{
		Kind:    req.Kind,
		Section: req.Section,
		SameRun: req.SameRun,
	}, req.Clock)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"incident": inc, "retest": rs})
}

func (s *Server) handleListRetests(w http.ResponseWriter, r *http.Request) {
	sets, err := s.svc.Retests(domain.JobID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"retests": sets})
}

func (s *Server) handleStartTreatment(w http.ResponseWriter, r *http.Request) {
	tr, err := s.svc.StartTreatment(domain.JobID(r.PathValue("id")), r.PathValue("retestID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tr)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	var req reviewRequest
	if !decodeBody(w, r, &req) {
		return
	}
	rev, err := s.svc.SubmitReview(domain.JobID(r.PathValue("id")), req.Person, req.Approved)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rev)
}

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	v, err := s.svc.Terminal(domain.JobID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
