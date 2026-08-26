package domain

// Canonical identifiers. Producers normalize these strings; consumers treat
// them as opaque, orderable keys.
type (
	JobID         string
	NodeID        string
	SectionID     string
	ValveID       string
	OutletID      string
	InjectionID   string
	SamplePointID string
	SampleID      string
	LeaseID       string
	RoundID       string
)

// RuleVersion identifies a versioned catalog of thresholds, scales, and
// instrument calibration rules. Locking a job records an immutable digest
// of the version so stale summaries can never lock a topology.
type RuleVersion struct {
	Version    int                 `json:"version"`
	Thresholds map[string]Quantity `json:"thresholds"`
	Scale      map[string]int      `json:"scale"`
	Digest     string              `json:"digest"`
}

// Qualification records a person's eligibility for a role up to a logical
// time limit. Independent review and custody require distinct qualified
// persons.
type Qualification struct {
	PersonID string `json:"person_id"`
	Role     string `json:"role"`
	ValidTo  int64  `json:"valid_to"` // logical time, inclusive
}

// InstrumentRule records calibration requirements for an instrument kind.
type InstrumentRule struct {
	InstrumentKind  string `json:"instrument_kind"`
	CalibrationDays int    `json:"calibration_days"`
}

// LockedJob is the immutable summary of a locked topology plus the mutable
// progress state reconstructed deterministically from appended events.
type LockedJob struct {
	ID             JobID  `json:"id"`
	TopologyDigest string `json:"topology_digest"`
	RuleDigest     string `json:"rule_digest"`
	RuleVersion    int    `json:"rule_version"`
	Clock          int64  `json:"clock"`
	Stage          Stage  `json:"stage"`
	Round          int    `json:"round"`
}

// PipeNode is a normalized node in the pipeline graph. Boundary nodes mark
// connections to the existing supply network.
type PipeNode struct {
	ID         NodeID `json:"id"`
	IsBoundary bool   `json:"is_boundary"`
}

// PipeSection is a directed section between two nodes with integer
// dimensions (millimetres and metres) and a canonical flow direction.
type PipeSection struct {
	ID         SectionID `json:"id"`
	From       NodeID    `json:"from"`
	To         NodeID    `json:"to"`
	DiameterMM int       `json:"diameter_mm"` // integer millimetres
	LengthM    int       `json:"length_m"`    // integer metres
	IsBlindEnd bool      `json:"is_blind_end"`
}

// ValveBoundary is an external connection that must be verified closed
// before flushing may begin.
type ValveBoundary struct {
	ID        ValveID   `json:"id"`
	SectionID SectionID `json:"section_id"`
	Closed    bool      `json:"closed"`
}

// FlushOutlet is a discharge outlet for directional flushing.
type FlushOutlet struct {
	ID        OutletID  `json:"id"`
	SectionID SectionID `json:"section_id"`
}

// InjectionPoint is where disinfectant is introduced into the pipeline.
type InjectionPoint struct {
	ID        InjectionID `json:"id"`
	SectionID SectionID   `json:"section_id"`
}

// SamplingPoint is where a sample is collected, in the locked order.
type SamplingPoint struct {
	ID        SamplePointID `json:"id"`
	SectionID SectionID     `json:"section_id"`
	Order     int           `json:"order"`
}

// WorkflowEvent is an append-only record of a stage transition or evidence
// submission. Events are never modified after persistence.
type WorkflowEvent struct {
	JobID       JobID  `json:"job_id"`
	Stage       Stage  `json:"stage"`
	Clock       int64  `json:"clock"`
	OperationID string `json:"operation_id"`
	Digest      string `json:"digest"`
	Round       int    `json:"round"`
}

// MeasurementSeries is a fixed-point reading (flow, turbidity, chlorine,
// pressure) captured at a logical time.
type MeasurementSeries struct {
	JobID        JobID      `json:"job_id"`
	InstrumentID string     `json:"instrument_id"`
	Clock        int64      `json:"clock"`
	Readings     []Quantity `json:"readings"`
}

// ChemicalDose records an exact disinfectant injection amount.
type ChemicalDose struct {
	JobID       JobID       `json:"job_id"`
	InjectionID InjectionID `json:"injection_id"`
	Clock       int64       `json:"clock"`
	Amount      Quantity    `json:"amount"`
}

// ResourceLease is a time-bounded exclusive hold on a resource key.
type ResourceLease struct {
	ID       LeaseID `json:"id"`
	Resource string  `json:"resource"`
	Holder   string  `json:"holder"`
	Clock    int64   `json:"clock"`   // acquired at logical time
	Expires  int64   `json:"expires"` // exclusive until this logical time
}

// Sample is a unique, non-reusable labelled sample collected at a locked
// sampling point for the current round.
type Sample struct {
	ID          SampleID      `json:"id"`
	JobID       JobID         `json:"job_id"`
	PointID     SamplePointID `json:"point_id"`
	Round       int           `json:"round"`
	Label       string        `json:"label"`
	Digest      string        `json:"digest"`
	CollectedBy string        `json:"collected_by"`
	SealedBy    string        `json:"sealed_by"`
}

// CustodyEvent is a link in the collection/sealing/handoff/receipt chain.
type CustodyEvent struct {
	SampleID SampleID `json:"sample_id"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Clock    int64    `json:"clock"`
	Action   string   `json:"action"`
}

// LabAttempt is a scripted, deterministic detection call with retry state.
type LabAttempt struct {
	ID          string   `json:"id"`
	SampleID    SampleID `json:"sample_id"`
	RetryNumber int      `json:"retry_number"`
	Status      string   `json:"status"` // pending, retryable, complete
	Calibration string   `json:"calibration"`
	Digest      string   `json:"digest"`
	TestItem    string   `json:"test_item"`
}

// LabResult is a calibration-checked detection result.
type LabResult struct {
	AttemptID string   `json:"attempt_id"`
	SampleID  SampleID `json:"sample_id"`
	TestItem  string   `json:"test_item"`
	Value     Quantity `json:"value"`
	Passed    bool     `json:"passed"`
}

// Incident is the seed of an anomaly (turbidity, chlorine, microbial,
// isolation, or sample-chain failure).
type Incident struct {
	ID     string `json:"id"`
	JobID  JobID  `json:"job_id"`
	Kind   string `json:"kind"`
	Clock  int64  `json:"clock"`
	Closed bool   `json:"closed"`
}

// RetestSet is the stable, deterministically ordered set of affected
// sampling points derived from an incident's propagation.
type RetestSet struct {
	ID      string          `json:"id"`
	JobID   JobID           `json:"job_id"`
	Members []SamplePointID `json:"members"`
	Round   int             `json:"round"`
}

// TreatmentRound links a new round to the retest set that triggered it.
type TreatmentRound struct {
	ID       RoundID `json:"id"`
	JobID    JobID   `json:"job_id"`
	RetestID string  `json:"retest_id"`
	Round    int     `json:"round"`
}

// ReviewDecision is an independent review by a qualified person.
type ReviewDecision struct {
	JobID    JobID  `json:"job_id"`
	PersonID string `json:"person_id"`
	Approved bool   `json:"approved"`
	Digest   string `json:"digest"`
}

// TerminalVerdict is the immutable, single-writer release credential.
type TerminalVerdict struct {
	JobID        JobID    `json:"job_id"`
	Credential   string   `json:"credential"`
	CommitNumber int64    `json:"commit_number"`
	Reasons      []string `json:"reasons,omitempty"`
}

// OperationReceipt is the idempotent response for an operation number.
type OperationReceipt struct {
	OperationID  string `json:"operation_id"`
	Digest       string `json:"digest"`
	CommitNumber int64  `json:"commit_number"`
}
