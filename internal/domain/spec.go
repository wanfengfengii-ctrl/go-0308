package domain

// TopologySpec is the full, normalized input for a pipeline topology. It is
// the request shape submitted when creating a job and is digested into the
// immutable topology summary on lock.
type TopologySpec struct {
	Nodes      []PipeNode       `json:"nodes"`
	Sections   []PipeSection    `json:"sections"`
	Valves     []ValveBoundary  `json:"valves"`
	Outlets    []FlushOutlet    `json:"outlets"`
	Injections []InjectionPoint `json:"injections"`
	Sampling   []SamplingPoint  `json:"sampling"`
}

// JobTargets captures the hydraulic and disinfection objectives that govern
// a job. All values are fixed-point quantities with explicit scales; the
// workflow engine compares submitted readings against these targets.
type JobTargets struct {
	// MinFlow is the minimum directional flow required during flushing.
	MinFlow Quantity `json:"min_flow"`
	// MaxTurbidity is the highest allowed turbidity during flushing.
	MaxTurbidity Quantity `json:"max_turbidity"`
	// MinWindowCount is the number of consecutive compliant readings needed
	// to close the flush window.
	MinWindowCount int64 `json:"min_window_count"`
	// MinInitialConc is the minimum disinfectant concentration immediately
	// after injection.
	MinInitialConc Quantity `json:"min_initial_conc"`
	// MinTerminalConc is the minimum disinfectant concentration at the end of
	// the contact period.
	MinTerminalConc Quantity `json:"min_terminal_conc"`
	// MinCT is the minimum concentration-time integral over the contact
	// period.
	MinCT Quantity `json:"min_ct"`
	// ContactDuration is the required effective contact duration in logical
	// time units.
	ContactDuration int64 `json:"contact_duration"`
	// TurnoverTarget is the minimum pipeline volume replacement factor.
	TurnoverTarget int64 `json:"turnover_target"`
	// TurnoverScale is the scale at which TurnoverTarget is expressed.
	TurnoverScale int `json:"turnover_scale"`
}

// CreateJobRequest is the public request payload for creating and locking a
// new job in a single logical step.
type CreateJobRequest struct {
	Topology TopologySpec `json:"topology"`
	Targets  JobTargets   `json:"targets"`
	RuleVer  int          `json:"rule_version"`
}

// EvidenceKind discriminates the typed evidence a stage accepts.
type EvidenceKind string

const (
	EvidenceValve     EvidenceKind = "valve_position"
	EvidencePressure  EvidenceKind = "pressure"
	EvidenceFlow      EvidenceKind = "flow"
	EvidenceTurbidity EvidenceKind = "turbidity"
	EvidenceChlorine  EvidenceKind = "chlorine"
	EvidenceDose      EvidenceKind = "dose"
	EvidenceReflush   EvidenceKind = "reflush"
	EvidenceContact   EvidenceKind = "contact"
)

// Evidence is a single submitted reading or action bound to a stage, an
// operation id (for idempotency), and a logical clock.
type Evidence struct {
	JobID       JobID        `json:"job_id"`
	Stage       Stage        `json:"stage"`
	Kind        EvidenceKind `json:"kind"`
	OperationID string       `json:"operation_id"`
	Clock       int64        `json:"clock"`
	// Values carries the fixed-point reading(s); interpretation depends on
	// Kind.
	Values []Quantity `json:"values,omitempty"`
	// InstrumentID identifies the instrument that produced the reading.
	InstrumentID string `json:"instrument_id,omitempty"`
	// LeaseID ties a dose or action to an active resource lease.
	LeaseID string `json:"lease_id,omitempty"`
	// PersonID records the qualified person for isolation and review actions.
	PersonID string `json:"person_id,omitempty"`
	// ValveStates maps valve ids to their submitted closed state for the
	// isolation stage.
	ValveStates map[string]bool `json:"valve_states,omitempty"`
}
