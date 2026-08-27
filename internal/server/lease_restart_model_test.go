package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/store"
	"example.com/potable-water-pipeline/internal/workflow"
)

func TestModel_LeaseRecoveryAcrossRestart(t *testing.T) {
	type operation struct {
		kind       string
		holder     string
		clock      int64
		expires    int64
		wantStatus int
		wantCode   string
	}
	tests := []struct {
		name          string
		beforeRestart []operation
		afterRestart  []operation
		wantHolder    string
	}{
		{
			name: "new service permits initial acquisition",
			afterRestart: []operation{
				{kind: "acquire", holder: "alice", clock: 10, expires: 100, wantStatus: http.StatusCreated},
			},
			wantHolder: "alice",
		},
		{
			name: "live lease rejects a competing holder after restart",
			beforeRestart: []operation{
				{kind: "acquire", holder: "alice", clock: 10, expires: 100, wantStatus: http.StatusCreated},
			},
			afterRestart: []operation{
				{kind: "acquire", holder: "bob", clock: 20, expires: 120, wantStatus: http.StatusConflict, wantCode: "lease_busy"},
			},
			wantHolder: "alice",
		},
		{
			name: "expired lease permits acquisition after restart",
			beforeRestart: []operation{
				{kind: "acquire", holder: "alice", clock: 10, expires: 20, wantStatus: http.StatusCreated},
			},
			afterRestart: []operation{
				{kind: "acquire", holder: "bob", clock: 20, expires: 120, wantStatus: http.StatusCreated},
			},
			wantHolder: "bob",
		},
		{
			name: "owner release after restart permits acquisition",
			beforeRestart: []operation{
				{kind: "acquire", holder: "alice", clock: 10, expires: 100, wantStatus: http.StatusCreated},
			},
			afterRestart: []operation{
				{kind: "release", holder: "alice", clock: 20, wantStatus: http.StatusOK},
				{kind: "acquire", holder: "bob", clock: 21, expires: 120, wantStatus: http.StatusCreated},
			},
			wantHolder: "bob",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "leases.db")
			open := func() (*store.Store, http.Handler) {
				t.Helper()
				st, err := store.Open(dbPath)
				if err != nil {
					t.Fatalf("open store: %v", err)
				}
				svc, err := workflow.New(st, rules.DefaultCatalog())
				if err != nil {
					_ = st.Close()
					t.Fatalf("new service: %v", err)
				}
				return st, New(t.TempDir(), svc).Handler()
			}
			run := func(handler http.Handler, op operation) {
				t.Helper()
				body, err := json.Marshal(map[string]any{
					"resource": "instrument-turbidity",
					"holder":   op.holder,
					"clock":    op.clock,
					"expires":  op.expires,
				})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				url := "/api/jobs/disinfection-1/leases/" + op.kind
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body)))
				if rec.Code != op.wantStatus {
					t.Fatalf("%s %s at clock %d: status = %d, want %d; body=%s", op.kind, op.holder, op.clock, rec.Code, op.wantStatus, rec.Body.String())
				}
				if op.wantCode != "" {
					var response struct {
						Error string `json:"error"`
						Code  string `json:"code"`
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
						t.Fatalf("decode conflict response: %v", err)
					}
					if response.Error != "conflict" || response.Code != op.wantCode {
						t.Fatalf("conflict response = %+v, want error=conflict code=%s", response, op.wantCode)
					}
				}
			}

			st, handler := open()
			for _, op := range tc.beforeRestart {
				run(handler, op)
			}
			if err := st.Close(); err != nil {
				t.Fatalf("close store for restart: %v", err)
			}

			st, handler = open()
			defer st.Close()
			for _, op := range tc.afterRestart {
				run(handler, op)
			}
			lease, ok, err := st.LeaseForResource("instrument-turbidity")
			if err != nil {
				t.Fatalf("load final lease: %v", err)
			}
			if !ok || lease.Holder != tc.wantHolder {
				t.Fatalf("final lease = %+v, found=%v; want holder %q", lease, ok, tc.wantHolder)
			}
		})
	}
}
