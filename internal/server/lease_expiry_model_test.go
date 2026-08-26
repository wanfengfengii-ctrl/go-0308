package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/server"
	"example.com/potable-water-pipeline/internal/store"
	"example.com/potable-water-pipeline/internal/workflow"
)

func TestModel_LeaseExpiryReacquisition(t *testing.T) {
	type acquireResult struct {
		status int
		body   string
		lease  domain.ResourceLease
		code   string
	}
	type fixture struct {
		store   *store.Store
		service *workflow.Service
		acquire func(resource, holder string, clock, expires int64) acquireResult
	}

	cases := []struct {
		name string
		run  func(*testing.T, fixture)
	}{
		{
			name: "expired durable lease is atomically superseded through HTTP",
			run: func(t *testing.T, f fixture) {
				first := f.acquire("pump-field-7", "shift-a", 10, 20)
				if first.status != http.StatusCreated {
					t.Fatalf("first acquire status = %d, body = %s", first.status, first.body)
				}

				second := f.acquire("pump-field-7", "shift-b", 20, 35)
				if second.status != http.StatusCreated {
					t.Fatalf("reacquire at expiry status = %d, want %d; body = %s", second.status, http.StatusCreated, second.body)
				}
				if strings.Contains(strings.ToLower(second.body), "constraint") {
					t.Fatalf("HTTP response leaked a persistence constraint: %s", second.body)
				}
				if second.lease.ID == "" || second.lease.ID == first.lease.ID || second.lease.Holder != "shift-b" {
					t.Fatalf("replacement lease = %+v, first = %+v", second.lease, first.lease)
				}

				persisted, ok, err := f.store.LeaseForResource("pump-field-7")
				if err != nil || !ok {
					t.Fatalf("persisted replacement: found=%v err=%v", ok, err)
				}
				if persisted != second.lease {
					t.Fatalf("persisted lease = %+v, HTTP lease = %+v", persisted, second.lease)
				}
				if _, oldStillExists, err := f.store.GetLease(first.lease.ID); err != nil || oldStillExists {
					t.Fatalf("expired lease still persisted: found=%v err=%v", oldStillExists, err)
				}
			},
		},
		{
			name: "unexpired competitor receives stable lease_busy",
			run: func(t *testing.T, f fixture) {
				first := f.acquire("pump-field-7", "shift-a", 10, 20)
				if first.status != http.StatusCreated {
					t.Fatalf("first acquire status = %d, body = %s", first.status, first.body)
				}
				contender := f.acquire("pump-field-7", "shift-b", 19, 35)
				if contender.status != http.StatusConflict || contender.code != string(domain.ConflictLeaseBusy) {
					t.Fatalf("competing acquire = status %d code %q body %s", contender.status, contender.code, contender.body)
				}
				persisted, ok, err := f.store.LeaseForResource("pump-field-7")
				if err != nil || !ok || persisted != first.lease {
					t.Fatalf("busy conflict changed persistence: lease=%+v found=%v err=%v", persisted, ok, err)
				}
			},
		},
		{
			name: "expiry equal to clock receives stable lease_expired",
			run: func(t *testing.T, f fixture) {
				got := f.acquire("pump-field-7", "shift-a", 20, 20)
				if got.status != http.StatusConflict || got.code != string(domain.ConflictLeaseExpired) {
					t.Fatalf("invalid window = status %d code %q body %s", got.status, got.code, got.body)
				}
				if _, ok, err := f.store.LeaseForResource("pump-field-7"); err != nil || ok {
					t.Fatalf("invalid window was persisted: found=%v err=%v", ok, err)
				}
			},
		},
		{
			name: "expiry before clock receives stable lease_expired",
			run: func(t *testing.T, f fixture) {
				got := f.acquire("pump-field-7", "shift-a", 20, 19)
				if got.status != http.StatusConflict || got.code != string(domain.ConflictLeaseExpired) {
					t.Fatalf("invalid window = status %d code %q body %s", got.status, got.code, got.body)
				}
			},
		},
		{
			name: "failed persistence rolls back in-memory occupancy",
			run: func(t *testing.T, f fixture) {
				if _, err := f.store.DB().Exec(`
CREATE TRIGGER reject_lease_insert
BEFORE INSERT ON leases
BEGIN
  SELECT RAISE(ABORT, 'forced lease persistence failure');
END`); err != nil {
					t.Fatalf("install failure trigger: %v", err)
				}
				if _, err := f.service.AcquireLease("job-field", "pump-field-7", "shift-a", 10, 20); err == nil {
					t.Fatal("acquire succeeded despite forced persistence failure")
				}
				if _, ok, err := f.store.LeaseForResource("pump-field-7"); err != nil || ok {
					t.Fatalf("failed acquire left durable occupancy: found=%v err=%v", ok, err)
				}
				if _, err := f.store.DB().Exec(`DROP TRIGGER reject_lease_insert`); err != nil {
					t.Fatalf("remove failure trigger: %v", err)
				}

				retry, err := f.service.AcquireLease("job-field", "pump-field-7", "shift-b", 11, 21)
				if err != nil {
					t.Fatalf("retry after persistence recovery: %v", err)
				}
				persisted, ok, err := f.store.LeaseForResource("pump-field-7")
				if err != nil || !ok || persisted != retry {
					t.Fatalf("recovered state mismatch: persisted=%+v retry=%+v found=%v err=%v", persisted, retry, ok, err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(":memory:")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			svc, err := workflow.New(st, rules.DefaultCatalog())
			if err != nil {
				t.Fatalf("create service: %v", err)
			}
			handler := server.New(t.TempDir(), svc).Handler()
			acquire := func(resource, holder string, clock, expires int64) acquireResult {
				body, err := json.Marshal(map[string]any{
					"resource": resource,
					"holder":   holder,
					"clock":    clock,
					"expires":  expires,
				})
				if err != nil {
					t.Fatalf("marshal acquire request: %v", err)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/jobs/job-field/leases/acquire", bytes.NewReader(body)))
				result := acquireResult{status: rec.Code, body: rec.Body.String()}
				var response struct {
					domain.ResourceLease
					Code string `json:"code"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode acquire response %q: %v", rec.Body.String(), err)
				}
				result.lease = response.ResourceLease
				result.code = response.Code
				return result
			}
			tc.run(t, fixture{store: st, service: svc, acquire: acquire})
		})
	}
}
