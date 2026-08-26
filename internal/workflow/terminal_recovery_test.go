package workflow

import (
	"sync"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/store"
)

// driveToReview advances a job fully through sampling, sample collection, a
// passing lab result, and leaves it at the review stage.
func driveToReview(t *testing.T, svc *Service, id domain.JobID) {
	t.Helper()
	driveToSampling(t, svc, id)
	sm, err := svc.CreateSample(id, "sp1", "collector-a", "sealer-b", 62)
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.CreateAttempt(sm.ID, "turbidity", "turbidity_meter", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitResult(a.ID, "turbidity", domain.Quantity{Value: 2, Scale: 0}, true, true); err != nil {
		t.Fatal(err)
	}
	job, err := svc.GetJob(id)
	if err != nil {
		t.Fatal(err)
	}
	if job.Stage != domain.StageReview {
		t.Fatalf("stage after sampling = %q, want review", job.Stage)
	}
}

func TestTerminalHappyPath(t *testing.T) {
	svc := newTestService(t)
	id := domain.JobID("job-happy")
	createValidJob(t, svc, id)
	driveToReview(t, svc, id)

	if _, err := svc.SubmitReview(id, "reviewer-a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitReview(id, "reviewer-b", true); err != nil {
		t.Fatal(err)
	}
	v, err := svc.Terminal(id)
	if err != nil {
		t.Fatal(err)
	}
	if v.Credential == "" || v.CommitNumber == 0 {
		t.Fatalf("terminal verdict = %+v, want non-empty credential and commit", v)
	}
	// The credential is stable on re-read.
	again, err := svc.Terminal(id)
	if err != nil {
		t.Fatal(err)
	}
	if again.Credential != v.Credential || again.CommitNumber != v.CommitNumber {
		t.Fatalf("credential changed on re-read: %+v vs %+v", v, again)
	}
}

func TestTerminalMissingEvidence(t *testing.T) {
	svc := newTestService(t)
	id := domain.JobID("job-missing")
	createValidJob(t, svc, id)
	// No evidence at all: terminal must fail with stable reasons.
	_, err := svc.Terminal(id)
	if err == nil {
		t.Fatal("terminal should fail without evidence")
	}
	var cf *domain.ConflictError
	if !asConflict(err, &cf) {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestTerminalConcurrentSingleCredential(t *testing.T) {
	svc := newTestService(t)
	id := domain.JobID("job-concurrent")
	createValidJob(t, svc, id)
	driveToReview(t, svc, id)
	if _, err := svc.SubmitReview(id, "reviewer-a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitReview(id, "reviewer-b", true); err != nil {
		t.Fatal(err)
	}

	const n = 16
	creds := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := svc.Terminal(id)
			if err == nil {
				creds <- v.Credential
			}
		}()
	}
	wg.Wait()
	close(creds)
	unique := map[string]bool{}
	for c := range creds {
		unique[c] = true
	}
	if len(unique) != 1 {
		t.Fatalf("concurrent terminal produced %d distinct credentials, want 1", len(unique))
	}
}

func TestRestartRecovery(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/service.db"

	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(st, rules.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}
	id := domain.JobID("job-recovery")
	if _, err := svc.CreateJob(id, validJobRequest()); err != nil {
		t.Fatal(err)
	}
	driveToSampling(t, svc, id)
	// Hold a lease that must survive restart.
	if _, err := svc.AcquireLease(id, "instrument-turbidity", "operator", 4, 1000); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen and verify state is recovered.
	st2, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	svc2, err := New(st2, rules.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}
	job, err := svc2.GetJob(id)
	if err != nil {
		t.Fatal(err)
	}
	if job.Stage != domain.StageSampling || job.Clock == 0 {
		t.Fatalf("recovered job = %+v, want sampling with non-zero clock", job)
	}
	// The outstanding lease is recovered (occupancy survives restart).
	if _, ok, err := st2.LeaseForResource("instrument-turbidity"); err != nil || !ok {
		t.Fatalf("lease not recovered: ok=%v err=%v", ok, err)
	}
}

func asConflict(err error, target **domain.ConflictError) bool {
	for err != nil {
		if cf, ok := err.(*domain.ConflictError); ok {
			*target = cf
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
