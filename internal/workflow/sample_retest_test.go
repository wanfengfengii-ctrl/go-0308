package workflow

import (
	"errors"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/retest"
	"example.com/potable-water-pipeline/internal/sample"
)

func TestSampleLabelUniquenessAndChain(t *testing.T) {
	svc := newTestService(t)
	id := domain.JobID("job-chain")
	createValidJob(t, svc, id)
	driveToSampling(t, svc, id)

	sm, err := svc.CreateSample(id, "sp1", "collector-a", "sealer-b", 62)
	if err != nil {
		t.Fatal(err)
	}
	// A second sample for the same point in the same round must be rejected.
	if _, err := svc.CreateSample(id, "sp1", "collector-c", "sealer-d", 63); err == nil {
		t.Fatal("second sample for point in round should be rejected")
	}

	// Complete custody chain.
	chain := []struct {
		from, to, action string
		clock            int64
	}{
		{"collector-a", "sealer-b", sample.ActionCollect, 63},
		{"sealer-b", "lab", sample.ActionSeal, 64},
		{"lab", "courier", sample.ActionHandoff, 65},
		{"courier", "lab", sample.ActionReceipt, 66},
	}
	for _, c := range chain {
		if _, err := svc.AppendCustody(sm.ID, c.from, c.to, c.action, c.clock); err != nil {
			t.Fatal(err)
		}
	}
	if sm.Label == "" {
		t.Fatal("empty label")
	}
}

func TestLabAttemptRetryAndResult(t *testing.T) {
	svc := newTestService(t)
	id := domain.JobID("job-lab")
	createValidJob(t, svc, id)
	driveToSampling(t, svc, id)
	sm, err := svc.CreateSample(id, "sp1", "collector-a", "sealer-b", 62)
	if err != nil {
		t.Fatal(err)
	}

	a, err := svc.CreateAttempt(sm.ID, "turbidity", "turbidity_meter", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != sample.StatusPending {
		t.Fatalf("attempt status = %q, want pending", a.Status)
	}
	// Stale calibration rejects the result and leaves it retryable.
	if _, err := svc.SubmitResult(a.ID, "turbidity", domain.Quantity{Value: 2, Scale: 0}, true, false); err == nil {
		t.Fatal("stale calibration result should be rejected")
	}
	got, err := svc.GetAttempt(a.ID)
	if err == nil && got.Status != sample.StatusRetryable {
		t.Fatalf("attempt status = %q, want retryable", got.Status)
	}
	// Retry then submit a calibrated, passed result.
	if _, err := svc.RetryAttempt(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitResult(a.ID, "turbidity", domain.Quantity{Value: 2, Scale: 0}, true, true); err != nil {
		t.Fatal(err)
	}
}

func TestIncidentPropagationAndTreatmentRound(t *testing.T) {
	svc := newTestService(t)
	id := domain.JobID("job-retest")
	createValidJob(t, svc, id)
	driveToSampling(t, svc, id)
	if _, err := svc.CreateSample(id, "sp1", "collector-a", "sealer-b", 62); err != nil {
		t.Fatal(err)
	}

	inc, rs, err := svc.CreateIncident(id, retest.IncidentSeed{
		Kind:    retest.KindTurbidity,
		Section: "s2",
	}, 70)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Members) == 0 {
		t.Fatal("retest set should include the sampling point on s2")
	}
	found := false
	for _, m := range rs.Members {
		if m == "sp1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("retest members %v should include sp1", rs.Members)
	}

	tr, err := svc.StartTreatment(id, rs.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Round != 2 {
		t.Fatalf("treatment round = %d, want 2", tr.Round)
	}
	job, err := svc.GetJob(id)
	if err != nil {
		t.Fatal(err)
	}
	if job.Round != 2 || job.Stage != domain.StageSampling {
		t.Fatalf("after treatment job = %+v, want round 2 sampling", job)
	}
	_ = inc
}

func TestInvalidIncidentKindRejected(t *testing.T) {
	svc := newTestService(t)
	id := domain.JobID("job-inc-kind")
	createValidJob(t, svc, id)
	if _, _, err := svc.CreateIncident(id, retest.IncidentSeed{Kind: retest.Kind("bogus")}, 10); err == nil {
		t.Fatal("invalid incident kind should be rejected")
	}
	var cf *domain.ValidationError
	if _, _, err := svc.CreateIncident(id, retest.IncidentSeed{Kind: retest.Kind("bogus")}, 10); !errors.As(err, &cf) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestEvidenceIdempotentReplay(t *testing.T) {
	svc := newTestService(t)
	id := domain.JobID("job-idem")
	createValidJob(t, svc, id)
	ev := domain.Evidence{
		JobID: id, Stage: domain.StageIsolation, Kind: domain.EvidenceValve,
		Clock: 2, PersonID: "inspector-1", OperationID: "op-1",
		ValveStates: map[string]bool{"v1": true, "v2": true},
	}
	if _, err := svc.SubmitEvidence(ev); err != nil {
		t.Fatal(err)
	}
	// Replay same content: must succeed idempotently.
	if _, err := svc.SubmitEvidence(ev); err != nil {
		t.Fatalf("idempotent replay failed: %v", err)
	}
	// Different content with the same operation id must conflict.
	ev2 := ev
	ev2.Clock = 3
	if _, err := svc.SubmitEvidence(ev2); err == nil {
		t.Fatal("conflicting replay should be rejected")
	}
}
