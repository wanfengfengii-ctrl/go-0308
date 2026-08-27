package workflow

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/store"
)

func TestModel_ReplayReceiptScopedByJob(t *testing.T) {
	type receiptResult struct {
		Job   string `json:"job"`
		Stage string `json:"stage"`
		Clock int64  `json:"clock"`
	}

	decode := func(t *testing.T, got any) receiptResult {
		t.Helper()
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal receipt result: %v", err)
		}
		var result receiptResult
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("decode receipt result: %v", err)
		}
		return result
	}
	isolation := func(job domain.JobID, clock int64) domain.Evidence {
		return domain.Evidence{
			JobID: job, Stage: domain.StageIsolation, Kind: domain.EvidenceValve,
			OperationID: "op-1", Clock: clock, PersonID: "inspector-1",
			ValveStates: map[string]bool{"v1": true, "v2": true},
		}
	}
	wantConflict := func(t *testing.T, err error) {
		t.Helper()
		var conflict *domain.ConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("got error %v, want content-mismatch conflict", err)
		}
		if conflict.Code != domain.ConflictContentMismatch {
			t.Fatalf("conflict code = %q, want %q", conflict.Code, domain.ConflictContentMismatch)
		}
	}

	cases := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "same operation id is independent across jobs",
			run: func(t *testing.T) {
				svc := newTestService(t)
				jobA, jobB := domain.JobID("job-a"), domain.JobID("job-b")
				createValidJob(t, svc, jobA)
				createValidJob(t, svc, jobB)

				gotA, err := svc.SubmitEvidence(isolation(jobA, 2))
				if err != nil {
					t.Fatalf("first job submit: %v", err)
				}
				gotB, err := svc.SubmitEvidence(isolation(jobB, 3))
				if err != nil {
					t.Fatalf("second job submit with same operation id: %v", err)
				}
				if result := decode(t, gotA); result.Job != string(jobA) || result.Stage != string(domain.StageFlush) || result.Clock != 2 {
					t.Fatalf("first job result = %+v, want its own flush receipt", result)
				}
				if result := decode(t, gotB); result.Job != string(jobB) || result.Stage != string(domain.StageFlush) || result.Clock != 3 {
					t.Fatalf("second job result = %+v, want its own flush receipt", result)
				}
			},
		},
		{
			name: "same job and digest returns original result",
			run: func(t *testing.T) {
				svc := newTestService(t)
				job := domain.JobID("job-replay")
				createValidJob(t, svc, job)
				ev := isolation(job, 2)
				if _, err := svc.SubmitEvidence(ev); err != nil {
					t.Fatalf("initial submit: %v", err)
				}
				got, err := svc.SubmitEvidence(ev)
				if err != nil {
					t.Fatalf("identical replay: %v", err)
				}
				if result := decode(t, got); result.Job != string(job) || result.Stage != string(domain.StageFlush) || result.Clock != 2 {
					t.Fatalf("replayed result = %+v, want original result", result)
				}
				events, err := svc.Timeline(job)
				if err != nil {
					t.Fatal(err)
				}
				if len(events) != 1 {
					t.Fatalf("timeline has %d events after replay, want 1", len(events))
				}
			},
		},
		{
			name: "same job and operation id with another digest stays conflicting",
			run: func(t *testing.T) {
				svc := newTestService(t)
				job := domain.JobID("job-conflict")
				createValidJob(t, svc, job)
				if _, err := svc.SubmitEvidence(isolation(job, 2)); err != nil {
					t.Fatalf("initial submit: %v", err)
				}
				changed := isolation(job, 3)
				for attempt := 1; attempt <= 2; attempt++ {
					_, err := svc.SubmitEvidence(changed)
					if err == nil {
						t.Fatalf("conflicting replay attempt %d succeeded", attempt)
					}
					wantConflict(t, err)
				}
			},
		},
		{
			name: "recovered receipts remain owned by their job",
			run: func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "receipts.db")
				st, err := store.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = st.Close() })
				svc, err := New(st, rules.DefaultCatalog())
				if err != nil {
					t.Fatal(err)
				}
				jobA, jobB := domain.JobID("job-recovered-a"), domain.JobID("job-recovered-b")
				createValidJob(t, svc, jobA)
				createValidJob(t, svc, jobB)
				evA := isolation(jobA, 2)
				if _, err := svc.SubmitEvidence(evA); err != nil {
					t.Fatalf("initial submit: %v", err)
				}
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
				st, err = store.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				svc, err = New(st, rules.DefaultCatalog())
				if err != nil {
					t.Fatal(err)
				}

				replayed, err := svc.SubmitEvidence(evA)
				if err != nil {
					t.Fatalf("replay after recovery: %v", err)
				}
				if result := decode(t, replayed); result.Job != string(jobA) || result.Clock != 2 {
					t.Fatalf("recovered result = %+v, want receipt for %s", result, jobA)
				}
				gotB, err := svc.SubmitEvidence(isolation(jobB, 4))
				if err != nil {
					t.Fatalf("second job submit after recovery: %v", err)
				}
				if result := decode(t, gotB); result.Job != string(jobB) || result.Clock != 4 {
					t.Fatalf("second job result = %+v, want independent receipt", result)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
