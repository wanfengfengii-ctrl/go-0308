package server

import (
	"net/http"

	"example.com/potable-water-pipeline/internal/domain"
)

// sampleRequest is the body for creating a sample at a sampling point.
type sampleRequest struct {
	Point     domain.SamplePointID `json:"point"`
	Collector string               `json:"collector"`
	Sealer    string               `json:"sealer"`
	Clock     int64                `json:"clock"`
}

// custodyRequest is the body for appending a custody link.
type custodyRequest struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Action string `json:"action"`
	Clock  int64  `json:"clock"`
}

// attemptRequest is the body for creating a detection attempt.
type attemptRequest struct {
	TestItem     string `json:"test_item"`
	Instrument   string `json:"instrument"`
	CalibratedAt int64  `json:"calibrated_at"`
	Clock        int64  `json:"clock"`
}

// resultRequest is the body for submitting a lab result.
type resultRequest struct {
	TestItem   string          `json:"test_item"`
	Value      domain.Quantity `json:"value"`
	Passed     bool            `json:"passed"`
	Calibrated bool            `json:"calibrated"`
}

func (s *Server) handleCreateSample(w http.ResponseWriter, r *http.Request) {
	var req sampleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	sm, err := s.svc.CreateSample(domain.JobID(r.PathValue("id")), req.Point, req.Collector, req.Sealer, req.Clock)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sm)
}

func (s *Server) handleCustody(w http.ResponseWriter, r *http.Request) {
	var req custodyRequest
	if !decodeBody(w, r, &req) {
		return
	}
	ev, err := s.svc.AppendCustody(domain.SampleID(r.PathValue("id")), req.From, req.To, req.Action, req.Clock)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ev)
}

func (s *Server) handleCreateAttempt(w http.ResponseWriter, r *http.Request) {
	var req attemptRequest
	if !decodeBody(w, r, &req) {
		return
	}
	a, err := s.svc.CreateAttempt(domain.SampleID(r.PathValue("id")), req.TestItem, req.Instrument, req.CalibratedAt, req.Clock)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleRetryAttempt(w http.ResponseWriter, r *http.Request) {
	a, err := s.svc.RetryAttempt(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleLabResult(w http.ResponseWriter, r *http.Request) {
	var req resultRequest
	if !decodeBody(w, r, &req) {
		return
	}
	res, err := s.svc.SubmitResult(r.PathValue("id"), req.TestItem, req.Value, req.Passed, req.Calibrated)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
