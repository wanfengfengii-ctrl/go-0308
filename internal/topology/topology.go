package topology

import (
	"sort"

	"example.com/potable-water-pipeline/internal/domain"
)

// Topology is a directed pipeline graph with boundary valves, flush outlets,
// and sampling points. Validation checks the documented invariants: the graph
// must be connected, the directed flow must be acyclic, every external
// connection must be guarded by a verified-closed boundary valve, and every
// blind end must carry an executable flush or sampling measure.
type Topology struct {
	Nodes      []domain.PipeNode
	Sections   []domain.PipeSection
	Valves     []domain.ValveBoundary
	Outlets    []domain.FlushOutlet
	Injections []domain.InjectionPoint
	Sampling   []domain.SamplingPoint
}

// Reason is a stable validation finding keyed by a canonical identifier.
type Reason struct {
	Code string
	Key  string
}

func (r Reason) String() string {
	if r.Key == "" {
		return r.Code
	}
	return r.Code + ": " + r.Key
}

// Validate returns the sorted, deterministic list of reasons the topology
// fails. An empty slice means the topology is valid and lockable.
func (t Topology) Validate() []Reason {
	var reasons []Reason
	reasons = append(reasons, t.checkConnectivity()...)
	reasons = append(reasons, t.checkAcyclic()...)
	reasons = append(reasons, t.checkIsolation()...)
	reasons = append(reasons, t.checkBlindEnds()...)
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].Code != reasons[j].Code {
			return reasons[i].Code < reasons[j].Code
		}
		return reasons[i].Key < reasons[j].Key
	})
	return reasons
}

func (t Topology) nodeSet() map[domain.NodeID]bool {
	set := make(map[domain.NodeID]bool, len(t.Nodes))
	for _, n := range t.Nodes {
		set[n.ID] = true
	}
	return set
}

// checkConnectivity verifies all nodes belong to one undirected component.
func (t Topology) checkConnectivity() []Reason {
	nodes := t.nodeSet()
	if len(nodes) == 0 {
		return nil
	}
	adj := make(map[domain.NodeID][]domain.NodeID, len(nodes))
	for id := range nodes {
		adj[id] = nil
	}
	for _, s := range t.Sections {
		adj[s.From] = append(adj[s.From], s.To)
		adj[s.To] = append(adj[s.To], s.From)
	}
	// BFS from a node that actually has an edge, so that isolated nodes are
	// reported as disconnected rather than the connected component being
	// reported as disconnected when iteration happens to start on an isolate.
	var start domain.NodeID
	found := false
	for _, s := range t.Sections {
		start = s.From
		found = true
		break
	}
	if !found {
		for id := range nodes {
			start = id
			break
		}
	}
	visited := map[domain.NodeID]bool{start: true}
	queue := []domain.NodeID{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	var reasons []Reason
	for id := range nodes {
		if !visited[id] {
			reasons = append(reasons, Reason{Code: "topology_disconnected", Key: string(id)})
		}
	}
	return reasons
}

// checkAcyclic verifies the directed flow graph contains no cycle.
func (t Topology) checkAcyclic() []Reason {
	nodes := t.nodeSet()
	color := make(map[domain.NodeID]int, len(nodes)) // 0 white, 1 gray, 2 black
	adj := make(map[domain.NodeID][]domain.NodeID, len(nodes))
	for id := range nodes {
		adj[id] = nil
	}
	for _, s := range t.Sections {
		adj[s.From] = append(adj[s.From], s.To)
	}
	var cycleNode domain.NodeID
	found := false
	var visit func(domain.NodeID)
	visit = func(id domain.NodeID) {
		if found {
			return
		}
		color[id] = 1
		for _, next := range adj[id] {
			switch color[next] {
			case 0:
				visit(next)
			case 1:
				found = true
				cycleNode = next
				return
			}
			if found {
				return
			}
		}
		color[id] = 2
	}
	for id := range nodes {
		if color[id] == 0 {
			visit(id)
			if found {
				break
			}
		}
	}
	if found {
		return []Reason{{Code: "flow_cycle", Key: string(cycleNode)}}
	}
	return nil
}

// checkIsolation verifies every boundary valve is closed.
func (t Topology) checkIsolation() []Reason {
	var reasons []Reason
	for _, v := range t.Valves {
		if !v.Closed {
			reasons = append(reasons, Reason{Code: "external_not_isolated", Key: string(v.ID)})
		}
	}
	return reasons
}

// checkBlindEnds verifies every blind-end section has a flush outlet or a
// sampling point.
func (t Topology) checkBlindEnds() []Reason {
	outlets := make(map[domain.SectionID]bool, len(t.Outlets))
	for _, o := range t.Outlets {
		outlets[o.SectionID] = true
	}
	sampling := make(map[domain.SectionID]bool, len(t.Sampling))
	for _, s := range t.Sampling {
		sampling[s.SectionID] = true
	}
	var reasons []Reason
	for _, s := range t.Sections {
		if s.IsBlindEnd && !outlets[s.ID] && !sampling[s.ID] {
			reasons = append(reasons, Reason{Code: "blind_end_no_measure", Key: string(s.ID)})
		}
	}
	return reasons
}
