package verdict

import (
	"fmt"
	"sort"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/topology"
)

// Arbitrator decides whether a job may receive the single terminal release
// credential. It checks topology isolation, the stage prefix, the continuous
// flush window, contact conditions, full sampling coverage, closed incidents,
// and two distinct qualified reviews. All missing evidence is reported as a
// stable, sorted list of reasons.
type Arbitrator struct{}

// NewArbitrator returns an empty arbitrator.
func NewArbitrator() *Arbitrator { return &Arbitrator{} }

// Snapshot is the complete, read-only state the arbitrator inspects. It is
// assembled from the persisted store before a verdict is attempted.
type Snapshot struct {
	Job            domain.LockedJob
	Topology       topology.LockedTopology
	ValvesVerified bool
	FlushWindowMet bool
	ContactPassed  bool
	SampleResults  map[domain.SamplePointID]bool // point -> result passed
	OpenIncidents  []string                      // incident ids still open
	Reviews        []domain.ReviewDecision
}

// Decision is the outcome of arbitration.
type Decision struct {
	Approved bool     `json:"approved"`
	Reasons  []string `json:"reasons,omitempty"`
}

// Decide evaluates the snapshot. It returns Approved=true with no reasons when
// every precondition is satisfied; otherwise it returns Approved=false with a
// sorted, deterministic list of missing evidence.
func (a *Arbitrator) Decide(s Snapshot) Decision {
	var reasons []string

	if !s.ValvesVerified {
		reasons = append(reasons, "isolation: boundary valves not verified closed")
	}
	if !s.FlushWindowMet {
		reasons = append(reasons, "flush: continuous compliance window not met")
	}
	if !s.ContactPassed {
		reasons = append(reasons, "contact: concentration-time criteria not met")
	}
	for _, sp := range s.Topology.SamplingOrder() {
		passed, ok := s.SampleResults[sp.ID]
		if !ok {
			reasons = append(reasons, fmt.Sprintf("sampling:%s: no valid sample", sp.ID))
			continue
		}
		if !passed {
			reasons = append(reasons, fmt.Sprintf("sampling:%s: result not passed", sp.ID))
		}
	}
	for _, id := range s.OpenIncidents {
		reasons = append(reasons, "incident:"+id+": open")
	}

	reviewers := map[string]bool{}
	approved := 0
	for _, r := range s.Reviews {
		if !r.Approved {
			continue
		}
		if !reviewers[r.PersonID] {
			reviewers[r.PersonID] = true
			approved++
		}
	}
	if approved < 2 {
		reasons = append(reasons, "review: need two distinct qualified approvals")
	}

	if s.Job.Stage != domain.StageReview {
		reasons = append(reasons, fmt.Sprintf("stage: expected %s, got %s", domain.StageReview, s.Job.Stage))
	}

	sort.Strings(reasons)
	if len(reasons) == 0 {
		return Decision{Approved: true}
	}
	return Decision{Approved: false, Reasons: reasons}
}
