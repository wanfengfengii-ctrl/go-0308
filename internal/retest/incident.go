package retest

import (
	"sort"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/topology"
)

// Kind identifies the class of anomaly that seeds a retest set.
type Kind string

const (
	KindTurbidity Kind = "turbidity"
	KindChlorine  Kind = "chlorine"
	KindMicrobial Kind = "microbial"
	KindIsolation Kind = "isolation"
	KindChain     Kind = "chain"
)

// Valid reports whether k is a recognized incident kind.
func (k Kind) Valid() bool {
	switch k {
	case KindTurbidity, KindChlorine, KindMicrobial, KindIsolation, KindChain:
		return true
	default:
		return false
	}
}

// IncidentSeed is the request shape for creating an incident: a kind plus the
// section where the anomaly was observed and any sampling points that shared
// the same detection run.
type IncidentSeed struct {
	Kind    Kind                   `json:"kind"`
	Section domain.SectionID       `json:"section"`
	SameRun []domain.SamplePointID `json:"same_run,omitempty"`
}

// ComputeMembers deterministically derives the set of sampling points affected
// by an incident. It includes:
//   - sampling points on the seed section (common section),
//   - sampling points on every section downstream of the seed along the flow,
//   - sampling points on reachable blind-end branches,
//   - sampling points from the same detection run.
//
// The result is unique and sorted by canonical identifier.
func ComputeMembers(t topology.Topology, seed IncidentSeed) []domain.SamplePointID {
	_, seedTo, ok := t.NodesOnSection(seed.Section)
	if !ok {
		seedTo = ""
	}
	down := t.Downstream(seedTo)

	members := map[domain.SamplePointID]bool{}
	for _, s := range t.Sections {
		inSeed := s.ID == seed.Section
		reachable := down[s.From]
		if s.IsBlindEnd {
			// Blind ends reachable from the seed are always re-sampled.
			reachable = reachable || down[s.To] || inSeed
		}
		if inSeed || reachable {
			for _, sp := range t.Sampling {
				if sp.SectionID == s.ID {
					members[sp.ID] = true
				}
			}
		}
	}
	for _, id := range seed.SameRun {
		members[id] = true
	}

	out := make([]domain.SamplePointID, 0, len(members))
	for id := range members {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// RetestSet builds a stable retest set for the given job, round, and members,
// linked to the incident that seeded it.
func RetestSet(id string, job domain.JobID, incidentID string, round int, members []domain.SamplePointID) domain.RetestSet {
	sorted := append([]domain.SamplePointID(nil), members...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return domain.RetestSet{ID: id, IncidentID: incidentID, JobID: job, Round: round, Members: sorted}
}
