package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
)

func TestModel_CreateJobReplayContentBinding(t *testing.T) {
	baseRequest := func() domain.CreateJobRequest {
		return domain.CreateJobRequest{
			Topology: domain.TopologySpec{
				Nodes: []domain.PipeNode{
					{ID: "n1", IsBoundary: true},
					{ID: "n2"},
					{ID: "n3"},
				},
				Sections: []domain.PipeSection{
					{ID: "s1", From: "n1", To: "n2", DiameterMM: 100, LengthM: 80},
					{ID: "s2", From: "n2", To: "n3", DiameterMM: 75, LengthM: 120, IsBlindEnd: true},
				},
				Valves:     []domain.ValveBoundary{{ID: "v1", SectionID: "s1", Closed: true}},
				Outlets:    []domain.FlushOutlet{{ID: "o1", SectionID: "s2"}},
				Injections: []domain.InjectionPoint{{ID: "i1", SectionID: "s1"}},
				Sampling:   []domain.SamplingPoint{{ID: "sp1", SectionID: "s2", Order: 1}},
			},
			Targets: domain.JobTargets{
				MinFlow:         domain.Quantity{Value: 500, Scale: 0},
				MaxTurbidity:    domain.Quantity{Value: 5, Scale: 0},
				MinWindowCount:  2,
				MinInitialConc:  domain.Quantity{Value: 25, Scale: 0},
				MinTerminalConc: domain.Quantity{Value: 10, Scale: 0},
				MinCT:           domain.Quantity{Value: 960, Scale: 0},
				ContactDuration: 60,
				TurnoverTarget:  1,
				TurnoverScale:   0,
			},
			RuleVer: 1,
		}
	}

	type testCase struct {
		name             string
		jobID            string
		seed             bool
		mutate           func(*domain.CreateJobRequest)
		wantStatus       int
		wantConflictCode string
		assertUnchanged  bool
	}
	cases := []testCase{
		{
			name:       "identical replay returns original job",
			jobID:      "job-identical",
			seed:       true,
			wantStatus: http.StatusCreated,
		},
		{
			name:  "normalized equivalent replay returns original job",
			jobID: "job-normalized",
			seed:  true,
			mutate: func(req *domain.CreateJobRequest) {
				req.Topology.Nodes[0], req.Topology.Nodes[2] = req.Topology.Nodes[2], req.Topology.Nodes[0]
				req.Topology.Sections[0], req.Topology.Sections[1] = req.Topology.Sections[1], req.Topology.Sections[0]
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:  "changed section length is a stable conflict",
			jobID: "job-topology-conflict",
			seed:  true,
			mutate: func(req *domain.CreateJobRequest) {
				req.Topology.Sections[1].LengthM = 121
			},
			wantStatus:       http.StatusConflict,
			wantConflictCode: "content_mismatch",
			assertUnchanged:  true,
		},
		{
			name:  "changed targets are a stable conflict",
			jobID: "job-target-conflict",
			seed:  true,
			mutate: func(req *domain.CreateJobRequest) {
				req.Targets.MinWindowCount = 3
			},
			wantStatus:       http.StatusConflict,
			wantConflictCode: "content_mismatch",
			assertUnchanged:  true,
		},
		{
			name:  "changed rule version is a stable conflict",
			jobID: "job-rule-conflict",
			seed:  true,
			mutate: func(req *domain.CreateJobRequest) {
				req.RuleVer = 0
			},
			wantStatus:       http.StatusConflict,
			wantConflictCode: "content_mismatch",
			assertUnchanged:  true,
		},
		{
			name:       "new job id creates normally",
			jobID:      "job-new",
			wantStatus: http.StatusCreated,
		},
		{
			name:  "new job id still validates topology",
			jobID: "job-invalid-topology",
			mutate: func(req *domain.CreateJobRequest) {
				req.Topology.Valves[0].Closed = false
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:  "new job id still rejects stale rules",
			jobID: "job-stale-rule",
			mutate: func(req *domain.CreateJobRequest) {
				req.RuleVer = 0
			},
			wantStatus:       http.StatusConflict,
			wantConflictCode: "content_mismatch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			post := func(req domain.CreateJobRequest) *httptest.ResponseRecorder {
				t.Helper()
				body, err := json.Marshal(req)
				if err != nil {
					t.Fatal(err)
				}
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/jobs?id="+tc.jobID, bytes.NewReader(body)))
				return rec
			}
			get := func(path string) []byte {
				t.Helper()
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s status = %d, body = %s", path, rec.Code, rec.Body.String())
				}
				return append([]byte(nil), rec.Body.Bytes()...)
			}

			original := baseRequest()
			var originalResponse, jobBefore, topologyBefore []byte
			if tc.seed {
				rec := post(original)
				if rec.Code != http.StatusCreated {
					t.Fatalf("initial create status = %d, body = %s", rec.Code, rec.Body.String())
				}
				originalResponse = append([]byte(nil), rec.Body.Bytes()...)
				jobBefore = get("/api/jobs/" + tc.jobID)
				topologyBefore = get("/api/jobs/" + tc.jobID + "/topology")
			}

			request := baseRequest()
			if tc.mutate != nil {
				tc.mutate(&request)
			}
			rec := post(request)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantConflictCode != "" {
				var got struct {
					Error string `json:"error"`
					Code  string `json:"code"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatalf("decode conflict: %v; body = %s", err, rec.Body.String())
				}
				if got.Error != "conflict" || got.Code != tc.wantConflictCode {
					t.Fatalf("conflict = %+v, want error=conflict code=%q", got, tc.wantConflictCode)
				}
			}

			if tc.seed && tc.wantStatus == http.StatusCreated && !bytes.Equal(rec.Body.Bytes(), originalResponse) {
				t.Fatalf("replay response changed:\nfirst: %s\nreplay: %s", originalResponse, rec.Body.Bytes())
			}
			if tc.assertUnchanged {
				repeated := post(request)
				if repeated.Code != rec.Code || !bytes.Equal(repeated.Body.Bytes(), rec.Body.Bytes()) {
					t.Fatalf("conflict was not stable:\nfirst: status=%d body=%s\nsecond: status=%d body=%s", rec.Code, rec.Body.Bytes(), repeated.Code, repeated.Body.Bytes())
				}
				if got := get("/api/jobs/" + tc.jobID); !bytes.Equal(got, jobBefore) {
					t.Fatalf("locked job state changed:\nbefore: %s\nafter: %s", jobBefore, got)
				}
				if got := get("/api/jobs/" + tc.jobID + "/topology"); !bytes.Equal(got, topologyBefore) {
					t.Fatalf("locked topology changed:\nbefore: %s\nafter: %s", topologyBefore, got)
				}
				replayOriginal := post(original)
				if replayOriginal.Code != http.StatusCreated || !bytes.Equal(replayOriginal.Body.Bytes(), originalResponse) {
					t.Fatalf("original request no longer replays after conflict: status=%d body=%s", replayOriginal.Code, replayOriginal.Body.Bytes())
				}
			}
		})
	}
}
