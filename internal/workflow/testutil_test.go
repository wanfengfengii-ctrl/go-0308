package workflow

import (
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/store"
)

// newTestService builds a workflow service backed by an in-memory SQLite store.
func newTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc, err := New(st, rules.DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// validJobRequest returns a minimal, valid locked-topology request.
func validJobRequest() domain.CreateJobRequest {
	return domain.CreateJobRequest{
		Topology: domain.TopologySpec{
			Nodes: []domain.PipeNode{
				{ID: "n1", IsBoundary: true},
				{ID: "n2"},
				{ID: "n3", IsBoundary: true},
			},
			Sections: []domain.PipeSection{
				{ID: "s1", From: "n1", To: "n2", DiameterMM: 100, LengthM: 100},
				{ID: "s2", From: "n2", To: "n3", DiameterMM: 100, LengthM: 100, IsBlindEnd: true},
			},
			Valves: []domain.ValveBoundary{
				{ID: "v1", SectionID: "s1", Closed: true},
				{ID: "v2", SectionID: "s2", Closed: true},
			},
			Outlets:    []domain.FlushOutlet{{ID: "o1", SectionID: "s2"}},
			Injections: []domain.InjectionPoint{{ID: "inj1", SectionID: "s1"}},
			Sampling:   []domain.SamplingPoint{{ID: "sp1", SectionID: "s2", Order: 1}},
		},
		Targets: domain.JobTargets{
			MinFlow:         domain.Quantity{Value: 500, Scale: 0},
			MaxTurbidity:    domain.Quantity{Value: 5, Scale: 0},
			MinWindowCount:  2,
			MinInitialConc:  domain.Quantity{Value: 25, Scale: 0},
			MinTerminalConc: domain.Quantity{Value: 10, Scale: 0},
			MinCT:           domain.Quantity{Value: 960, Scale: 0},
			ContactDuration: 10,
			TurnoverTarget:  10,
			TurnoverScale:   1,
		},
		RuleVer: 1,
	}
}

// createValidJob creates and returns a locked job.
func createValidJob(t *testing.T, svc *Service, id domain.JobID) domain.LockedJob {
	t.Helper()
	job, err := svc.CreateJob(id, validJobRequest())
	if err != nil {
		t.Fatal(err)
	}
	return job
}

// driveToSampling advances a job through isolation, flush, disinfect, contact,
// and reflush so it is ready for sampling.
func driveToSampling(t *testing.T, svc *Service, id domain.JobID) {
	t.Helper()
	// isolation
	if _, err := svc.SubmitEvidence(domain.Evidence{
		JobID: id, Stage: domain.StageIsolation, Kind: domain.EvidenceValve,
		Clock: 2, PersonID: "inspector-1",
		ValveStates: map[string]bool{"v1": true, "v2": true},
	}); err != nil {
		t.Fatal(err)
	}
	// flush: two compliant readings (flow 600 >= 500, turbidity 3 <= 5).
	for i, c := range []int64{3, 4} {
		if _, err := svc.SubmitEvidence(domain.Evidence{
			JobID: id, Stage: domain.StageFlush, Kind: domain.EvidenceFlow, Clock: c,
			Values: []domain.Quantity{{Value: 1000, Scale: 0}, {Value: 3, Scale: 0}},
		}); err != nil {
			t.Fatalf("flush %d: %v", i, err)
		}
	}
	// disinfect under a held injection lease.
	lease, err := svc.AcquireLease(id, "pump-inj1", "operator", 4, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitEvidence(domain.Evidence{
		JobID: id, Stage: domain.StageDisinfect, Kind: domain.EvidenceDose, Clock: 5,
		Values:  []domain.Quantity{{Value: 250000, Scale: 0}},
		LeaseID: string(lease.ID), InstrumentID: "inj1",
	}); err != nil {
		t.Fatal(err)
	}
	// contact: initial 30 mg/L then terminal 20 mg/L after 50 time units.
	if _, err := svc.SubmitEvidence(domain.Evidence{
		JobID: id, Stage: domain.StageContact, Kind: domain.EvidenceChlorine, Clock: 10,
		Values: []domain.Quantity{{Value: 30, Scale: 0}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitEvidence(domain.Evidence{
		JobID: id, Stage: domain.StageContact, Kind: domain.EvidenceChlorine, Clock: 60,
		Values: []domain.Quantity{{Value: 20, Scale: 0}},
	}); err != nil {
		t.Fatal(err)
	}
	// reflush
	if _, err := svc.SubmitEvidence(domain.Evidence{
		JobID: id, Stage: domain.StageReflush, Kind: domain.EvidenceReflush, Clock: 61,
		Values: []domain.Quantity{{Value: 2, Scale: 0}},
	}); err != nil {
		t.Fatal(err)
	}
}
