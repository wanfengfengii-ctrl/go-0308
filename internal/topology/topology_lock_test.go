package topology

import (
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
)

func validTopology() Topology {
	return Topology{
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
		Outlets: []domain.FlushOutlet{
			{ID: "o1", SectionID: "s2"},
		},
		Sampling: []domain.SamplingPoint{
			{ID: "sp1", SectionID: "s2", Order: 1},
		},
	}
}

func TestTopologyValid(t *testing.T) {
	if reasons := validTopology().Validate(); len(reasons) != 0 {
		t.Fatalf("valid topology produced reasons: %v", reasons)
	}
}

func TestTopologyDisconnected(t *testing.T) {
	topo := validTopology()
	topo.Nodes = append(topo.Nodes, domain.PipeNode{ID: "n4"})
	reasons := topo.Validate()
	if len(reasons) != 1 || reasons[0].Code != "topology_disconnected" {
		t.Fatalf("disconnected reasons = %v, want one topology_disconnected", reasons)
	}
	if reasons[0].Key != "n4" {
		t.Fatalf("disconnected key = %q, want n4", reasons[0].Key)
	}
}

func TestTopologyFlowCycle(t *testing.T) {
	topo := validTopology()
	// Add a back edge n3 -> n1 forming a cycle.
	topo.Sections = append(topo.Sections, domain.PipeSection{
		ID: "s3", From: "n3", To: "n1", DiameterMM: 100, LengthM: 100,
	})
	reasons := topo.Validate()
	if len(reasons) != 1 || reasons[0].Code != "flow_cycle" {
		t.Fatalf("cycle reasons = %v, want one flow_cycle", reasons)
	}
}

func TestTopologyUnisolated(t *testing.T) {
	topo := validTopology()
	topo.Valves[0].Closed = false
	reasons := topo.Validate()
	if len(reasons) != 1 || reasons[0].Code != "external_not_isolated" {
		t.Fatalf("unisolated reasons = %v, want one external_not_isolated", reasons)
	}
	if reasons[0].Key != "v1" {
		t.Fatalf("unisolated key = %q, want v1", reasons[0].Key)
	}
}

func TestTopologyBlindEndNoMeasure(t *testing.T) {
	topo := Topology{
		Nodes: []domain.PipeNode{{ID: "n1"}, {ID: "n2"}},
		Sections: []domain.PipeSection{
			{ID: "s1", From: "n1", To: "n2", DiameterMM: 100, LengthM: 100, IsBlindEnd: true},
		},
	}
	reasons := topo.Validate()
	if len(reasons) != 1 || reasons[0].Code != "blind_end_no_measure" {
		t.Fatalf("blind end reasons = %v, want one blind_end_no_measure", reasons)
	}
}

func TestTopologyReasonsSorted(t *testing.T) {
	topo := validTopology()
	topo.Valves[0].Closed = false
	topo.Nodes = append(topo.Nodes, domain.PipeNode{ID: "n9"})
	reasons := topo.Validate()
	if len(reasons) != 2 {
		t.Fatalf("want 2 reasons, got %v", reasons)
	}
	// Codes sort lexicographically: external_not_isolated < topology_disconnected.
	if reasons[0].Code != "external_not_isolated" || reasons[1].Code != "topology_disconnected" {
		t.Fatalf("reasons not sorted: %v", reasons)
	}
}

func TestLockDigestDeterministic(t *testing.T) {
	a := validTopology()
	b := Topology{
		Nodes:    []domain.PipeNode{{ID: "n3", IsBoundary: true}, {ID: "n1", IsBoundary: true}, {ID: "n2"}},
		Sections: []domain.PipeSection{{ID: "s2", From: "n2", To: "n3", DiameterMM: 100, LengthM: 100, IsBlindEnd: true}, {ID: "s1", From: "n1", To: "n2", DiameterMM: 100, LengthM: 100}},
		Valves:   []domain.ValveBoundary{{ID: "v2", SectionID: "s2", Closed: true}, {ID: "v1", SectionID: "s1", Closed: true}},
		Outlets:  []domain.FlushOutlet{{ID: "o1", SectionID: "s2"}},
		Sampling: []domain.SamplingPoint{{ID: "sp1", SectionID: "s2", Order: 1}},
	}
	la, err := FromTopology(a)
	if err != nil {
		t.Fatal(err)
	}
	lb, err := FromTopology(b)
	if err != nil {
		t.Fatal(err)
	}
	if la.Digest != lb.Digest {
		t.Fatalf("digests differ for reordered topology: %s vs %s", la.Digest, lb.Digest)
	}
}

func TestLockedTopologyRoundTrip(t *testing.T) {
	lt, err := FromTopology(validTopology())
	if err != nil {
		t.Fatal(err)
	}
	if lt.Digest == "" {
		t.Fatal("empty digest")
	}
	back := lt.ToTopology()
	if reasons := back.Validate(); len(reasons) != 0 {
		t.Fatalf("round-trip topology invalid: %v", reasons)
	}
	if !lt.HasSamplingPoint("sp1") {
		t.Fatal("missing sampling point sp1")
	}
	if lt.HasSamplingPoint("nope") {
		t.Fatal("unexpected sampling point")
	}
}

func TestFlowOrderAndPropagation(t *testing.T) {
	topo := validTopology()
	order := topo.FlowOrder()
	if len(order) != 3 {
		t.Fatalf("flow order length = %d, want 3", len(order))
	}
	// n1 -> n2 -> n3 along sections s1, s2.
	if order[0] != "n1" || order[1] != "n2" || order[2] != "n3" {
		t.Fatalf("flow order = %v, want [n1 n2 n3]", order)
	}
	up := topo.Upstream("n3")
	if !up["n1"] || !up["n2"] || !up["n3"] {
		t.Fatalf("upstream of n3 = %v, want n1,n2,n3", up)
	}
	down := topo.Downstream("n1")
	if !down["n2"] || !down["n3"] {
		t.Fatalf("downstream of n1 = %v, want n2,n3", down)
	}
}
