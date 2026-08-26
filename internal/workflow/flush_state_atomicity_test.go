package workflow

import (
	"reflect"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/store"
)

func TestModel_FlushEvidenceStateAtomicity(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "failed turnover calculation and replay leave all state unchanged",
			run: func(t *testing.T) {
				svc := newTestService(t)
				jobID := domain.JobID("flush-invalid-turnover")
				req := validJobRequest()
				req.Targets.MinWindowCount = 1
				req.Targets.TurnoverTarget = 0
				if _, err := svc.CreateJob(jobID, req); err != nil {
					t.Fatal(err)
				}

				isolationOp := "isolate-invalid-turnover"
				if _, err := svc.SubmitEvidence(domain.Evidence{
					JobID: jobID, Stage: domain.StageIsolation, Kind: domain.EvidenceValve,
					OperationID: isolationOp, Clock: 2, PersonID: "inspector-1",
					ValveStates: map[string]bool{"v1": true, "v2": true},
				}); err != nil {
					t.Fatal(err)
				}

				type state struct {
					progress          store.Progress
					measurements      []domain.MeasurementSeries
					events            []domain.WorkflowEvent
					isolationReceipt  domain.OperationReceipt
					isolationRecorded bool
					failedReceipt     domain.OperationReceipt
					failedRecorded    bool
				}
				readState := func() state {
					t.Helper()
					p, err := svc.store.LoadProgress(jobID)
					if err != nil {
						t.Fatal(err)
					}
					measurements, err := svc.Measurements(jobID)
					if err != nil {
						t.Fatal(err)
					}
					events, err := svc.Timeline(jobID)
					if err != nil {
						t.Fatal(err)
					}
					isolationReceipt, isolationRecorded, err := svc.store.GetReceipt(isolationOp)
					if err != nil {
						t.Fatal(err)
					}
					failedReceipt, failedRecorded, err := svc.store.GetReceipt("flush-invalid-op")
					if err != nil {
						t.Fatal(err)
					}
					return state{p, measurements, events, isolationReceipt, isolationRecorded, failedReceipt, failedRecorded}
				}

				before := readState()
				if !before.isolationRecorded || before.failedRecorded {
					t.Fatalf("unexpected receipt baseline: isolation=%v failed=%v", before.isolationRecorded, before.failedRecorded)
				}
				failed := domain.Evidence{
					JobID: jobID, Stage: domain.StageFlush, Kind: domain.EvidenceFlow,
					OperationID: "flush-invalid-op", InstrumentID: "meter-1", Clock: 3,
					Values: []domain.Quantity{{Value: 1000, Scale: 0}, {Value: 3, Scale: 0}},
				}
				for attempt := 1; attempt <= 2; attempt++ {
					if _, err := svc.SubmitEvidence(failed); err == nil {
						t.Fatalf("attempt %d: expected invalid turnover target to reject evidence", attempt)
					}
					after := readState()
					if !reflect.DeepEqual(after, before) {
						t.Fatalf("attempt %d changed durable state\nbefore: %#v\nafter:  %#v", attempt, before, after)
					}
				}
			},
		},
		{
			name: "valid readings accumulate in order and advance the flush stage",
			run: func(t *testing.T) {
				svc := newTestService(t)
				jobID := domain.JobID("flush-valid-window")
				createValidJob(t, svc, jobID)
				if _, err := svc.SubmitEvidence(domain.Evidence{
					JobID: jobID, Stage: domain.StageIsolation, Kind: domain.EvidenceValve,
					Clock: 2, PersonID: "inspector-1",
					ValveStates: map[string]bool{"v1": true, "v2": true},
				}); err != nil {
					t.Fatal(err)
				}

				readings := []domain.Evidence{
					{JobID: jobID, Stage: domain.StageFlush, Kind: domain.EvidenceFlow, OperationID: "flush-valid-1", InstrumentID: "meter-1", Clock: 3, Values: []domain.Quantity{{Value: 1000, Scale: 0}, {Value: 4, Scale: 0}}},
					{JobID: jobID, Stage: domain.StageFlush, Kind: domain.EvidenceFlow, OperationID: "flush-valid-2", InstrumentID: "meter-1", Clock: 4, Values: []domain.Quantity{{Value: 1000, Scale: 0}, {Value: 3, Scale: 0}}},
				}
				if _, err := svc.SubmitEvidence(readings[0]); err != nil {
					t.Fatal(err)
				}
				progress, err := svc.store.LoadProgress(jobID)
				if err != nil {
					t.Fatal(err)
				}
				if progress.FlushWindow != 1 || progress.TurnoverCum != 1000 || progress.Clock != 3 {
					t.Fatalf("first reading was not accumulated: %#v", progress)
				}
				if _, err := svc.SubmitEvidence(readings[1]); err != nil {
					t.Fatal(err)
				}

				got, err := svc.Measurements(jobID)
				if err != nil {
					t.Fatal(err)
				}
				want := []domain.MeasurementSeries{
					{JobID: jobID, InstrumentID: "meter-1", Clock: 3, Readings: readings[0].Values},
					{JobID: jobID, InstrumentID: "meter-1", Clock: 4, Readings: readings[1].Values},
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("measurement order mismatch\nwant: %#v\ngot:  %#v", want, got)
				}
				job, err := svc.GetJob(jobID)
				if err != nil {
					t.Fatal(err)
				}
				if job.Stage != domain.StageDisinfect || job.Clock != 4 {
					t.Fatalf("flush did not advance normally: %#v", job)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
