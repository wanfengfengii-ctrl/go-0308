package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/store"
	"example.com/potable-water-pipeline/internal/workflow"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc, err := workflow.New(st, rules.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return New(t.TempDir(), svc)
}

func TestHealthEndpoint(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("health body = %v, want status ok", body)
	}
}

func TestCreateAndListJobs(t *testing.T) {
	s := newTestServer(t)
	req := domain.CreateJobRequest{
		Topology: domain.TopologySpec{
			Nodes:    []domain.PipeNode{{ID: "n1", IsBoundary: true}, {ID: "n2"}},
			Sections: []domain.PipeSection{{ID: "s1", From: "n1", To: "n2", DiameterMM: 100, LengthM: 100, IsBlindEnd: true}},
			Valves:   []domain.ValveBoundary{{ID: "v1", SectionID: "s1", Closed: true}},
			Outlets:  []domain.FlushOutlet{{ID: "o1", SectionID: "s1"}},
			Sampling: []domain.SamplingPoint{{ID: "sp1", SectionID: "s1", Order: 1}},
		},
		Targets: domain.JobTargets{MinWindowCount: 1},
		RuleVer: 1,
	}
	body, _ := json.Marshal(req)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/jobs?id=job-http", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var list struct {
		Jobs []domain.LockedJob `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Jobs) != 1 || list.Jobs[0].ID != "job-http" {
		t.Fatalf("jobs = %+v, want single job-http", list.Jobs)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/health", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
