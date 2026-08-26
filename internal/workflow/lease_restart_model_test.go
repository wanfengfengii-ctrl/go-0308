package workflow_test

import (
	"errors"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/store"
	"example.com/potable-water-pipeline/internal/workflow"
)

func TestModel_RestartLeaseSequenceAndConflicts(t *testing.T) {
	path := t.TempDir() + "/leases.db"
	job := domain.JobID("job-lease-restart")

	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := workflow.New(st, rules.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.AcquireLease(job, "resource-a", "alice", 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReleaseLease(job, "resource-a", "alice", 20); err != nil {
		t.Fatal(err)
	}
	second, err := svc.AcquireLease(job, "resource-b", "bob", 20, 100)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "lease-1" || second.ID != "lease-2" {
		t.Fatalf("setup lease ids = %q, %q; want lease-1, lease-2", first.ID, second.ID)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := workflow.New(reopened, rules.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		apply    func() (domain.ResourceLease, error)
		wantID   domain.LeaseID
		wantCode domain.ConflictCode
	}{
		{
			name: "free resource receives an unused id",
			apply: func() (domain.ResourceLease, error) {
				return recovered.AcquireLease(job, "resource-c", "carol", 30, 100)
			},
			wantID: "lease-3",
		},
		{
			name: "recovered busy resource remains busy",
			apply: func() (domain.ResourceLease, error) {
				return recovered.AcquireLease(job, "resource-b", "carol", 30, 100)
			},
			wantCode: domain.ConflictLeaseBusy,
		},
		{
			name: "nonfuture expiry remains rejected",
			apply: func() (domain.ResourceLease, error) {
				return recovered.AcquireLease(job, "resource-d", "dora", 30, 30)
			},
			wantCode: domain.ConflictLeaseExpired,
		},
		{
			name: "wrong holder cannot release recovered lease",
			apply: func() (domain.ResourceLease, error) {
				return domain.ResourceLease{}, recovered.ReleaseLease(job, "resource-b", "mallory", 30)
			},
			wantCode: domain.ConflictLeaseBusy,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.apply()
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("operation failed: %v", err)
				}
				if got.ID != tc.wantID {
					t.Fatalf("lease id = %q, want %q", got.ID, tc.wantID)
				}
				return
			}
			var conflict *domain.ConflictError
			if !errors.As(err, &conflict) || conflict.Code != tc.wantCode {
				t.Fatalf("error = %v, want conflict code %q", err, tc.wantCode)
			}
		})
	}
}
