package workflow

import (
	"errors"
	"reflect"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/retest"
)

func TestModel_StartTreatmentReplayAndRoundInvariants(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "same retest replay returns its existing round and preserves sampling history",
			run: func(t *testing.T) {
				svc := newTestService(t)
				jobID := domain.JobID("job-replay")
				createValidJob(t, svc, jobID)
				driveToSampling(t, svc, jobID)

				oldSample, err := svc.CreateSample(jobID, "sp1", "collector-old", "sealer-old", 62)
				if err != nil {
					t.Fatal(err)
				}
				_, rs, err := svc.CreateIncident(jobID, retest.IncidentSeed{
					Kind:    retest.KindTurbidity,
					Section: "s2",
				}, 70)
				if err != nil {
					t.Fatal(err)
				}

				first, err := svc.StartTreatment(jobID, rs.ID)
				if err != nil {
					t.Fatalf("first start: %v", err)
				}
				replayed, err := svc.StartTreatment(jobID, rs.ID)
				if err != nil {
					t.Fatalf("replayed start: %v", err)
				}
				if !reflect.DeepEqual(replayed, first) {
					t.Fatalf("replayed round = %+v, want original %+v", replayed, first)
				}
				if first.Round != 2 {
					t.Fatalf("first treatment round = %d, want 2", first.Round)
				}

				job, err := svc.GetJob(jobID)
				if err != nil {
					t.Fatal(err)
				}
				if job.Round != 2 || job.Stage != domain.StageSampling {
					t.Fatalf("job after replay = round %d stage %q, want round 2 sampling", job.Round, job.Stage)
				}

				newSample, err := svc.CreateSample(jobID, "sp1", "collector-new", "sealer-new", 71)
				if err != nil {
					t.Fatalf("collecting replacement sample after replay: %v", err)
				}
				if newSample.Round != 2 {
					t.Fatalf("replacement sample round = %d, want 2", newSample.Round)
				}
				samples, err := svc.Samples(jobID)
				if err != nil {
					t.Fatal(err)
				}
				if len(samples) != 2 || samples[0].ID != oldSample.ID || samples[0].Round != 1 || samples[1].ID != newSample.ID {
					t.Fatalf("samples after replay = %+v, want preserved round-1 sample and new round-2 sample", samples)
				}
			},
		},
		{
			name: "different retest sets create strictly increasing rounds",
			run: func(t *testing.T) {
				svc := newTestService(t)
				jobID := domain.JobID("job-distinct-retests")
				createValidJob(t, svc, jobID)
				driveToSampling(t, svc, jobID)

				_, firstSet, err := svc.CreateIncident(jobID, retest.IncidentSeed{Kind: retest.KindTurbidity, Section: "s2"}, 70)
				if err != nil {
					t.Fatal(err)
				}
				first, err := svc.StartTreatment(jobID, firstSet.ID)
				if err != nil {
					t.Fatal(err)
				}
				_, secondSet, err := svc.CreateIncident(jobID, retest.IncidentSeed{Kind: retest.KindChlorine, Section: "s2"}, 71)
				if err != nil {
					t.Fatal(err)
				}
				second, err := svc.StartTreatment(jobID, secondSet.ID)
				if err != nil {
					t.Fatal(err)
				}
				if firstSet.ID == secondSet.ID || first.Round != 2 || second.Round != 3 {
					t.Fatalf("sets/rounds = %q:%d then %q:%d, want distinct sets with rounds 2 then 3", firstSet.ID, first.Round, secondSet.ID, second.Round)
				}
				job, err := svc.GetJob(jobID)
				if err != nil {
					t.Fatal(err)
				}
				if job.Round != 3 || job.Stage != domain.StageSampling {
					t.Fatalf("job after second retest = round %d stage %q, want round 3 sampling", job.Round, job.Stage)
				}
			},
		},
		{
			name: "retest set cannot be started for another job",
			run: func(t *testing.T) {
				svc := newTestService(t)
				ownerID := domain.JobID("job-retest-owner")
				otherID := domain.JobID("job-retest-other")
				createValidJob(t, svc, ownerID)
				createValidJob(t, svc, otherID)
				driveToSampling(t, svc, otherID)

				_, rs, err := svc.CreateIncident(ownerID, retest.IncidentSeed{Kind: retest.KindTurbidity, Section: "s2"}, 70)
				if err != nil {
					t.Fatal(err)
				}
				_, err = svc.StartTreatment(otherID, rs.ID)
				var conflict *domain.ConflictError
				if !errors.As(err, &conflict) || conflict.Code != domain.ConflictRound {
					t.Fatalf("cross-job start error = %v, want round conflict", err)
				}
				other, err := svc.GetJob(otherID)
				if err != nil {
					t.Fatal(err)
				}
				if other.Round != 1 || other.Stage != domain.StageSampling {
					t.Fatalf("other job after rejected start = round %d stage %q, want unchanged round 1 sampling", other.Round, other.Stage)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
