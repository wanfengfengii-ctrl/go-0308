package topology

import (
	"sort"

	"example.com/potable-water-pipeline/internal/domain"
)

// LockedTopology is the immutable, canonical form of a validated topology. It
// is produced once at lock time and its digest is recorded in the job so that
// stale topologies can never be accepted after locking.
type LockedTopology struct {
	Digest     string                  `json:"digest"`
	Nodes      []domain.PipeNode       `json:"nodes"`
	Sections   []domain.PipeSection    `json:"sections"`
	Valves     []domain.ValveBoundary  `json:"valves"`
	Outlets    []domain.FlushOutlet    `json:"outlets"`
	Injections []domain.InjectionPoint `json:"injections"`
	Sampling   []domain.SamplingPoint  `json:"sampling"`
}

// FromTopology normalizes and digests a topology. The normalization sorts all
// collections by canonical identifier so that semantically equal topologies
// submitted in different orders produce the same digest.
func FromTopology(t Topology) (LockedTopology, error) {
	nodes := append([]domain.PipeNode(nil), t.Nodes...)
	sections := append([]domain.PipeSection(nil), t.Sections...)
	valves := append([]domain.ValveBoundary(nil), t.Valves...)
	outlets := append([]domain.FlushOutlet(nil), t.Outlets...)
	injections := append([]domain.InjectionPoint(nil), t.Injections...)
	sampling := append([]domain.SamplingPoint(nil), t.Sampling...)

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(sections, func(i, j int) bool { return sections[i].ID < sections[j].ID })
	sort.Slice(valves, func(i, j int) bool { return valves[i].ID < valves[j].ID })
	sort.Slice(outlets, func(i, j int) bool { return outlets[i].ID < outlets[j].ID })
	sort.Slice(injections, func(i, j int) bool { return injections[i].ID < injections[j].ID })
	sort.Slice(sampling, func(i, j int) bool { return sampling[i].ID < sampling[j].ID })

	lt := LockedTopology{
		Nodes:      nodes,
		Sections:   sections,
		Valves:     valves,
		Outlets:    outlets,
		Injections: injections,
		Sampling:   sampling,
	}
	digest, err := domain.Digest(lt)
	if err != nil {
		return LockedTopology{}, err
	}
	lt.Digest = digest
	return lt, nil
}

// ToTopology reconstructs the mutable validation view from a locked topology.
func (l LockedTopology) ToTopology() Topology {
	return Topology{
		Nodes:      l.Nodes,
		Sections:   l.Sections,
		Valves:     l.Valves,
		Outlets:    l.Outlets,
		Injections: l.Injections,
		Sampling:   l.Sampling,
	}
}

// SamplingOrder returns the sampling points sorted by their locked Order.
func (l LockedTopology) SamplingOrder() []domain.SamplingPoint {
	sp := append([]domain.SamplingPoint(nil), l.Sampling...)
	sort.Slice(sp, func(i, j int) bool { return sp[i].Order < sp[j].Order })
	return sp
}

// SamplingPoints returns the sampling points (in locked order).
func (l LockedTopology) SamplingPoints() []domain.SamplingPoint {
	return l.SamplingOrder()
}

// HasSamplingPoint reports whether id is a locked sampling point.
func (l LockedTopology) HasSamplingPoint(id domain.SamplePointID) bool {
	for _, sp := range l.Sampling {
		if sp.ID == id {
			return true
		}
	}
	return false
}

// VolumeLitres computes the total internal volume across all sections using
// the hydraulic fixed-point rule. It is exposed here so the workflow can
// compute turnover without reaching into hydraulic details.
func (l LockedTopology) TotalVolume(vol func(diamMM, lengthM int) (int, error)) (int, error) {
	total := 0
	for _, s := range l.Sections {
		v, err := vol(s.DiameterMM, s.LengthM)
		if err != nil {
			return 0, err
		}
		total += v
	}
	return total, nil
}
