package server

import (
	"net/http"

	"example.com/potable-water-pipeline/internal/domain"
)

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.svc.ListJobs()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateJobRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if r.URL.Query().Get("id") == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "reason": "id query parameter required"})
		return
	}
	job, err := s.svc.CreateJob(domain.JobID(r.URL.Query().Get("id")), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.svc.GetJob(domain.JobID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleLockJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.svc.GetJob(domain.JobID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	// Re-validate the locked topology and rule currency, then return the job.
	topo, err := s.svc.GetTopology(job.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if reasons := topo.ToTopology().Validate(); len(reasons) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "conflict", "code": "topology_invalid", "reason": reasons[0].String()})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	topo, err := s.svc.GetTopology(domain.JobID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, topo)
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	events, err := s.svc.Timeline(domain.JobID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleMeasurements(w http.ResponseWriter, r *http.Request) {
	ms, err := s.svc.Measurements(domain.JobID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"measurements": ms})
}

func (s *Server) handleListSamples(w http.ResponseWriter, r *http.Request) {
	samples, err := s.svc.Samples(domain.JobID(r.PathValue("id")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"samples": samples})
}
