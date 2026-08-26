package topology

import "example.com/potable-water-pipeline/internal/domain"

// FlowOrder returns the nodes in a deterministic topological order following
// the directed section flow. It is the canonical downstream-to-upstream view:
// for any section from -> to, "from" appears before "to". When the graph is
// acyclic this order is total; callers rely on it for stable propagation and
// canonical rendering.
func (t Topology) FlowOrder() []domain.NodeID {
	adj := make(map[domain.NodeID][]domain.NodeID, len(t.Nodes))
	indeg := make(map[domain.NodeID]int, len(t.Nodes))
	for _, n := range t.Nodes {
		adj[n.ID] = nil
		indeg[n.ID] = 0
	}
	for _, s := range t.Sections {
		adj[s.From] = append(adj[s.From], s.To)
		indeg[s.To]++
	}
	// Kahn's algorithm with a min-heap keyed by node id for determinism.
	var roots []domain.NodeID
	for id, d := range indeg {
		if d == 0 {
			roots = append(roots, id)
		}
	}
	sortNodeIDs(roots)
	var order []domain.NodeID
	for len(roots) > 0 {
		id := roots[0]
		roots = roots[1:]
		order = append(order, id)
		for _, next := range adj[id] {
			indeg[next]--
			if indeg[next] == 0 {
				roots = append(roots, next)
			}
		}
		sortNodeIDs(roots)
	}
	return order
}

// Upstream returns the set of nodes that can reach node by following the
// directed flow, i.e. the nodes whose water flows toward node. It includes
// node itself. This is the basis for anomaly propagation along the flow.
func (t Topology) Upstream(node domain.NodeID) map[domain.NodeID]bool {
	// Reverse adjacency: pred[x] = nodes with an edge x -> ...
	pred := make(map[domain.NodeID][]domain.NodeID, len(t.Nodes))
	for _, s := range t.Sections {
		pred[s.To] = append(pred[s.To], s.From)
	}
	seen := map[domain.NodeID]bool{node: true}
	queue := []domain.NodeID{node}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, p := range pred[cur] {
			if !seen[p] {
				seen[p] = true
				queue = append(queue, p)
			}
		}
	}
	return seen
}

// Downstream returns the set of nodes reachable from node following the
// directed flow. It includes node itself.
func (t Topology) Downstream(node domain.NodeID) map[domain.NodeID]bool {
	adj := make(map[domain.NodeID][]domain.NodeID, len(t.Nodes))
	for _, s := range t.Sections {
		adj[s.From] = append(adj[s.From], s.To)
	}
	seen := map[domain.NodeID]bool{node: true}
	queue := []domain.NodeID{node}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return seen
}

// SectionForNode returns the section id that leads into node, or "" when node
// has no incoming section. It is used to map anomalies to sections.
func (t Topology) SectionForNode(node domain.NodeID) domain.SectionID {
	for _, s := range t.Sections {
		if s.To == node {
			return s.ID
		}
	}
	return ""
}

// NodesOnSection returns the from and to node of a section, plus a bool
// indicating the section exists.
func (t Topology) NodesOnSection(id domain.SectionID) (domain.NodeID, domain.NodeID, bool) {
	for _, s := range t.Sections {
		if s.ID == id {
			return s.From, s.To, true
		}
	}
	return "", "", false
}

func sortNodeIDs(ids []domain.NodeID) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}
