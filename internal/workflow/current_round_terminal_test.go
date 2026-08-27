package workflow

import (
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/retest"
)

func TestModel_TerminalUsesOnlyCurrentTreatmentRound(t *testing.T) {
	cases := []struct {
		name          string
		currentResult string
		wantRelease   bool
	}{
		{name: "current round result missing", currentResult: "missing", wantRelease: false},
		{name: "current round result failed", currentResult: "failed", wantRelease: false},
		{name: "current round result passed", currentResult: "passed", wantRelease: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			jobID := domain.JobID("job-current-round-" + tc.currentResult)
			createValidJob(t, svc, jobID)
			driveToSampling(t, svc, jobID)

			oldSample, err := svc.CreateSample(jobID, "sp1", "collector-a", "sealer-b", 62)
			if err != nil {
				t.Fatal(err)
			}
			oldAttempt, err := svc.CreateAttempt(oldSample.ID, "turbidity", "turbidity_meter", 20, 100)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SubmitResult(oldAttempt.ID, "turbidity", domain.Quantity{Value: 2}, true, true); err != nil {
				t.Fatal(err)
			}

			_, retestSet, err := svc.CreateIncident(jobID, retest.IncidentSeed{
				Kind:    retest.KindTurbidity,
				Section: "s2",
			}, 70)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.StartTreatment(jobID, retestSet.ID); err != nil {
				t.Fatal(err)
			}

			currentSample, err := svc.CreateSample(jobID, "sp1", "collector-c", "sealer-d", 80)
			if err != nil {
				t.Fatal(err)
			}
			currentAttempt, err := svc.CreateAttempt(currentSample.ID, "turbidity", "turbidity_meter", 20, 100)
			if err != nil {
				t.Fatal(err)
			}
			if tc.currentResult != "missing" {
				passed := tc.currentResult == "passed"
				if _, err := svc.SubmitResult(currentAttempt.ID, "turbidity", domain.Quantity{Value: 2}, passed, true); err != nil {
					t.Fatal(err)
				}
			}

			samples, err := svc.Samples(jobID)
			if err != nil {
				t.Fatal(err)
			}
			if len(samples) != 2 || samples[0].Round != 1 || samples[1].Round != 2 {
				t.Fatalf("sample history = %+v, want retained rounds 1 and 2", samples)
			}
			oldResults, err := svc.store.LabResultsForSample(oldSample.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(oldResults) != 1 || !oldResults[0].Passed {
				t.Fatalf("old-round results = %+v, want retained passing result", oldResults)
			}

			if _, err := svc.SubmitReview(jobID, "reviewer-a", true); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SubmitReview(jobID, "reviewer-b", true); err != nil {
				t.Fatal(err)
			}

			verdict, err := svc.Terminal(jobID)
			if !tc.wantRelease {
				if err == nil || verdict.Credential != "" {
					t.Fatalf("Terminal() = %+v, %v; want release blocked", verdict, err)
				}
				return
			}
			if err != nil || verdict.Credential == "" {
				t.Fatalf("Terminal() = %+v, %v; want release credential", verdict, err)
			}
			again, err := svc.Terminal(jobID)
			if err != nil {
				t.Fatal(err)
			}
			if again.Credential != verdict.Credential || again.CommitNumber != verdict.CommitNumber {
				t.Fatalf("second Terminal() = %+v, want unique persisted verdict %+v", again, verdict)
			}
		})
	}
}
