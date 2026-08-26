package domain

// Stage is a strictly ordered workflow phase. The documented pipeline
// progresses through: topology lock, isolation verification, directional
// flush, disinfectant injection, effective contact, discharge reflush,
// point sampling, review, and terminal verdict. Ordering is enforced by
// Order and Next.
type Stage string

const (
	StageTopologyLock Stage = "topology_lock"
	StageIsolation    Stage = "isolation_verify"
	StageFlush        Stage = "flush"
	StageDisinfect    Stage = "disinfect_inject"
	StageContact      Stage = "contact"
	StageReflush      Stage = "discharge_reflush"
	StageSampling     Stage = "sampling"
	StageReview       Stage = "review"
	StageTerminal     Stage = "terminal_verdict"
)

// stageOrder is the canonical, strict ordering of workflow phases.
var stageOrder = []Stage{
	StageTopologyLock,
	StageIsolation,
	StageFlush,
	StageDisinfect,
	StageContact,
	StageReflush,
	StageSampling,
	StageReview,
	StageTerminal,
}

// Order returns the zero-based position of s, or -1 when s is unknown.
func (s Stage) Order() int {
	for i, st := range stageOrder {
		if st == s {
			return i
		}
	}
	return -1
}

// Valid reports whether s is a recognized workflow stage.
func (s Stage) Valid() bool { return s.Order() >= 0 }

// Next returns the stage immediately following s, or "" when s is last
// or unrecognized.
func (s Stage) Next() Stage {
	i := s.Order()
	if i < 0 || i >= len(stageOrder)-1 {
		return ""
	}
	return stageOrder[i+1]
}
