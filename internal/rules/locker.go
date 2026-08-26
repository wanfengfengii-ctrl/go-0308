package rules

import (
	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/topology"
)

// Locker validates and locks a job topology against a rule catalog. A lock
// records the immutable topology digest, the current rule digest, and the
// first round number; it rejects stale rule versions and invalid topologies.
type Locker struct {
	catalog *Catalog
}

// NewLocker returns a Locker bound to the given catalog.
func NewLocker(c *Catalog) *Locker { return &Locker{catalog: c} }

// LockResult is the outcome of a successful lock.
type LockResult struct {
	Job      domain.LockedJob
	Topology topology.LockedTopology
}

// Lock validates the topology, checks that the requested rule version is the
// current one, and returns the locked job plus the immutable topology. A stale
// rule version yields a ConflictError with ConflictContentMismatch so clients
// can distinguish it from a topology failure.
func (l *Locker) Lock(id domain.JobID, t topology.Topology, ruleVer int) (LockResult, error) {
	if reasons := t.Validate(); len(reasons) > 0 {
		// Deterministically sort reasons (Validate already sorts) and surface
		// the first as the primary reason.
		return LockResult{}, &domain.ValidationError{Field: "topology", Reason: reasons[0].String()}
	}
	if !l.catalog.IsCurrent(ruleVer) {
		return LockResult{}, &domain.ConflictError{
			Code:   domain.ConflictContentMismatch,
			Reason: "stale rule version",
		}
	}
	rv, _ := l.catalog.Current()
	locked, err := topology.FromTopology(t)
	if err != nil {
		return LockResult{}, err
	}
	job := domain.LockedJob{
		ID:             id,
		TopologyDigest: locked.Digest,
		RuleDigest:     rv.Digest,
		RuleVersion:    rv.Version,
		Clock:          1,
		// Locking completes the topology-lock phase, so the job begins at the
		// isolation-verification stage.
		Stage: domain.StageIsolation,
		Round: 1,
	}
	return LockResult{Job: job, Topology: locked}, nil
}
