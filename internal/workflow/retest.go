package workflow

import (
	"encoding/json"
	"fmt"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/retest"
)

// CreateIncident records an anomaly, derives the stable retest set by
// propagating along the flow, and stores both. It returns the incident and the
// retest set for the caller to inspect.
func (s *Service) CreateIncident(job domain.JobID, seed retest.IncidentSeed, clock int64) (domain.Incident, domain.RetestSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !seed.Kind.Valid() {
		return domain.Incident{}, domain.RetestSet{}, &domain.ValidationError{Field: "kind", Reason: string(seed.Kind)}
	}
	p, err := s.store.LoadProgress(job)
	if err != nil {
		return domain.Incident{}, domain.RetestSet{}, err
	}
	topo, err := s.store.GetTopology(job)
	if err != nil {
		return domain.Incident{}, domain.RetestSet{}, err
	}
	members := retest.ComputeMembers(topo.ToTopology(), seed)

	incSeq, err := s.store.CountIncidents(job)
	if err != nil {
		return domain.Incident{}, domain.RetestSet{}, err
	}
	inc := domain.Incident{
		ID:    fmt.Sprintf("inc-%d", incSeq+1),
		JobID: job,
		Kind:  string(seed.Kind),
		Clock: clock,
	}
	if err := s.store.CreateIncident(inc); err != nil {
		return domain.Incident{}, domain.RetestSet{}, err
	}

	retSeq, err := s.store.CountRetestSets(job)
	if err != nil {
		return domain.Incident{}, domain.RetestSet{}, err
	}
	rs := retest.RetestSet(fmt.Sprintf("rs-%d", retSeq+1), job, p.Round, members)
	if err := s.store.CreateRetestSet(rs); err != nil {
		return domain.Incident{}, domain.RetestSet{}, err
	}
	return inc, rs, nil
}

// StartTreatment creates a strictly increasing treatment round for a retest
// set, resets the job to the sampling stage for re-sampling, and closes the
// incident that triggered it. Old-round evidence is preserved as history.
//
// Starting treatment is idempotent per retest set: a retried "start treatment"
// request (frontend or gateway replay) returns the round that was already
// created instead of minting a strictly increasing new one, so the in-progress
// round is not squeezed into history by a duplicate start.
func (s *Service) StartTreatment(job domain.JobID, retestID string) (domain.TreatmentRound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// The retest set is the idempotency key for "start treatment": each retest
	// set starts at most one treatment round. The operation id is derived from
	// the (job, retest set) pair and the digest is the canonical summary of
	// the same pair, so a replay with identical content returns the original
	// round. The key is stable across retries even when the caller does not
	// echo a client operation id.
	operationID := fmt.Sprintf("start-treatment:%s:%s", job, retestID)
	digest := domain.MustDigest(struct {
		JobID    domain.JobID `json:"job_id"`
		RetestID string       `json:"retest_id"`
	}{job, retestID})

	result, err := s.idempotent(operationID, digest, job, func() (any, error) {
		p, err := s.store.LoadProgress(job)
		if err != nil {
			return nil, err
		}
		rs, err := s.store.GetRetestSet(retestID)
		if err != nil {
			return nil, err
		}
		if rs.JobID != job {
			return nil, &domain.ConflictError{Code: domain.ConflictRound, Reason: "retest set belongs to another job"}
		}
		maxRound, err := s.store.MaxRound(job)
		if err != nil {
			return nil, err
		}
		newRound := maxRound + 1
		if newRound <= p.Round {
			newRound = p.Round + 1
		}
		tr := domain.TreatmentRound{
			ID:       domain.RoundID(fmt.Sprintf("tr-%d", newRound)),
			JobID:    job,
			RetestID: retestID,
			Round:    newRound,
		}
		if err := s.store.CreateTreatmentRound(tr); err != nil {
			return nil, err
		}
		// Reset to sampling for the new round and close the triggering incident.
		p.Round = newRound
		p.Stage = domain.StageSampling
		p.Clock++
		if err := s.store.SaveProgress(job, p); err != nil {
			return nil, err
		}
		incs, err := s.store.ListIncidents(job)
		if err != nil {
			return nil, err
		}
		for _, inc := range incs {
			if !inc.Closed {
				_ = s.store.CloseIncident(inc.ID)
			}
		}
		return tr, nil
	})
	if err != nil {
		return domain.TreatmentRound{}, err
	}
	// Unmarshal the persisted (or freshly produced) round into the concrete
	// type so callers keep getting a TreatmentRound, whether the idempotent
	// receipt returned the original result or this call created it.
	b, err := json.Marshal(result)
	if err != nil {
		return domain.TreatmentRound{}, err
	}
	var tr domain.TreatmentRound
	if err := json.Unmarshal(b, &tr); err != nil {
		return domain.TreatmentRound{}, err
	}
	return tr, nil
}
