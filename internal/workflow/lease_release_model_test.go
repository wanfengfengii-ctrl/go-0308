package workflow_test

import (
	"errors"
	"path/filepath"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/store"
	"example.com/potable-water-pipeline/internal/workflow"
)

func TestModel_WrongHolderReleasePreservesLeaseAcrossRestart(t *testing.T) {
	cases := []struct {
		name    string
		restart bool
	}{
		{name: "current service remains occupied", restart: false},
		{name: "restarted service restores occupancy", restart: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "leases.db")
			st, err := store.Open(dbPath)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			closed := false
			t.Cleanup(func() {
				if !closed {
					_ = st.Close()
				}
			})

			svc, err := workflow.New(st, rules.DefaultCatalog())
			if err != nil {
				t.Fatalf("new service: %v", err)
			}
			job := domain.JobID("job-lease-release")
			original, err := svc.AcquireLease(job, "pump-dose-1", "operator-a", 10, 100)
			if err != nil {
				t.Fatalf("operator-a acquire: %v", err)
			}

			for _, clock := range []int64{20, 21} {
				err := svc.ReleaseLease(job, "pump-dose-1", "operator-b", clock)
				var conflict *domain.ConflictError
				if !errors.As(err, &conflict) || conflict.Code != domain.ConflictLeaseBusy {
					t.Fatalf("wrong-holder release at clock %d = %v, want lease_busy", clock, err)
				}
				persisted, ok, err := st.LeaseForResource("pump-dose-1")
				if err != nil {
					t.Fatalf("read persisted lease: %v", err)
				}
				if !ok || persisted != original {
					t.Fatalf("persisted lease after rejected release = (%+v, %t), want unchanged %+v", persisted, ok, original)
				}
			}

			if tc.restart {
				if err := st.Close(); err != nil {
					t.Fatalf("close store for restart: %v", err)
				}
				closed = true
				st, err = store.Open(dbPath)
				if err != nil {
					t.Fatalf("reopen store: %v", err)
				}
				closed = false
				svc, err = workflow.New(st, rules.DefaultCatalog())
				if err != nil {
					t.Fatalf("restart service: %v", err)
				}
			}

			_, err = svc.AcquireLease(job, "pump-dose-1", "operator-c", 30, 110)
			var conflict *domain.ConflictError
			if !errors.As(err, &conflict) || conflict.Code != domain.ConflictLeaseBusy {
				t.Fatalf("operator-c acquire before original expiry = %v, want lease_busy", err)
			}
			persisted, ok, err := st.LeaseForResource("pump-dose-1")
			if err != nil {
				t.Fatalf("read lease after blocked acquire: %v", err)
			}
			if !ok || persisted != original {
				t.Fatalf("lease after blocked acquire = (%+v, %t), want unchanged %+v", persisted, ok, original)
			}

			if _, err := svc.AcquireLease(job, "pump-dose-2", "operator-c", 30, 110); err != nil {
				t.Fatalf("independent resource acquire: %v", err)
			}
			if err := svc.ReleaseLease(job, "pump-dose-1", "operator-a", 40); err != nil {
				t.Fatalf("valid holder release: %v", err)
			}
			if _, err := svc.AcquireLease(job, "pump-dose-1", "operator-c", 41, 120); err != nil {
				t.Fatalf("acquire after valid release: %v", err)
			}
		})
	}
}
