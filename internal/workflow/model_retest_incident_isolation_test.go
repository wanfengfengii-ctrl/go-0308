package workflow

import (
	"slices"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/retest"
	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/store"
)

func TestModel_StartTreatmentPreservesUnrelatedIncident(t *testing.T) {
	tests := []struct {
		name         string
		treatedIndex int
	}{
		{name: "treat earlier turbidity retest", treatedIndex: 0},
		{name: "treat later chlorine retest", treatedIndex: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := t.TempDir() + "/workflow.db"
			st, err := store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			svc, err := New(st, rules.DefaultCatalog())
			if err != nil {
				t.Fatal(err)
			}

			jobID := domain.JobID("job-selective-treatment")
			if _, err := svc.CreateJob(jobID, validJobRequest()); err != nil {
				t.Fatal(err)
			}
			driveToSampling(t, svc, jobID)

			kinds := []retest.Kind{retest.KindTurbidity, retest.KindChlorine}
			incidents := make([]domain.Incident, 0, len(kinds))
			retests := make([]domain.RetestSet, 0, len(kinds))
			for i, kind := range kinds {
				inc, rs, err := svc.CreateIncident(jobID, retest.IncidentSeed{
					Kind:    kind,
					Section: "s2",
					SameRun: []domain.SamplePointID{"sp3", "sp2", "sp3"},
				}, int64(70+i))
				if err != nil {
					t.Fatalf("create %s incident: %v", kind, err)
				}
				if want := []domain.SamplePointID{"sp1", "sp2", "sp3"}; !slices.Equal(rs.Members, want) {
					t.Fatalf("%s retest members = %v, want stable order %v", kind, rs.Members, want)
				}
				incidents = append(incidents, inc)
				retests = append(retests, rs)
			}

			treated := tt.treatedIndex
			unrelated := 1 - treated
			tr, err := svc.StartTreatment(jobID, retests[treated].ID)
			if err != nil {
				t.Fatal(err)
			}
			if tr.Round != 2 || tr.RetestID != retests[treated].ID {
				t.Fatalf("treatment = %+v, want round 2 for %s", tr, retests[treated].ID)
			}
			job, err := svc.GetJob(jobID)
			if err != nil {
				t.Fatal(err)
			}
			if job.Round != 2 || job.Stage != domain.StageSampling {
				t.Fatalf("job after treatment = %+v, want round 2 sampling", job)
			}

			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			st, err = store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			svc, err = New(st, rules.DefaultCatalog())
			if err != nil {
				t.Fatal(err)
			}

			persisted, err := st.ListIncidents(jobID)
			if err != nil {
				t.Fatal(err)
			}
			if len(persisted) != 2 {
				t.Fatalf("persisted incidents = %+v, want both historical records", persisted)
			}
			closedByID := make(map[string]bool, len(persisted))
			for _, inc := range persisted {
				closedByID[inc.ID] = inc.Closed
			}
			if !closedByID[incidents[treated].ID] {
				t.Errorf("treated incident %s remained open", incidents[treated].ID)
			}
			if closedByID[incidents[unrelated].ID] {
				t.Errorf("unrelated incident %s was closed", incidents[unrelated].ID)
			}

			verdict, err := svc.Terminal(jobID)
			if err == nil {
				t.Fatal("terminal release succeeded with an unrelated open incident")
			}
			openReason := "incident:" + incidents[unrelated].ID + ": open"
			closedReason := "incident:" + incidents[treated].ID + ": open"
			if !slices.Contains(verdict.Reasons, openReason) {
				t.Errorf("terminal reasons %v do not retain %q", verdict.Reasons, openReason)
			}
			if slices.Contains(verdict.Reasons, closedReason) {
				t.Errorf("terminal reasons %v still contain resolved incident %q", verdict.Reasons, closedReason)
			}
		})
	}
}
