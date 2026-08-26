package workflow

import (
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
	rs := retest.RetestSet(fmt.Sprintf("rs-%d", retSeq+1), job, inc.ID, p.Round, members)
	if err := s.store.CreateRetestSet(rs); err != nil {
		return domain.Incident{}, domain.RetestSet{}, err
	}
	return inc, rs, nil
}

// StartTreatment creates a strictly increasing treatment round for a retest
// set, resets the job to the sampling stage for re-sampling, and closes the
// incident that triggered it. Old-round evidence is preserved as history.
func (s *Service) StartTreatment(job domain.JobID, retestID string) (domain.TreatmentRound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.store.LoadProgress(job)
	if err != nil {
		return domain.TreatmentRound{}, err
	}
	rs, err := s.store.GetRetestSet(retestID)
	if err != nil {
		return domain.TreatmentRound{}, err
	}
	if rs.JobID != job {
		return domain.TreatmentRound{}, &domain.ConflictError{Code: domain.ConflictRound, Reason: "retest set belongs to another job"}
	}
	maxRound, err := s.store.MaxRound(job)
	if err != nil {
		return domain.TreatmentRound{}, err
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
		return domain.TreatmentRound{}, err
	}
	// Reset to sampling for the new round and close the triggering incident.
	p.Round = newRound
	p.Stage = domain.StageSampling
	p.Clock++
	if err := s.store.SaveProgress(job, p); err != nil {
		return domain.TreatmentRound{}, err
	}
	// Only the incident that seeded this retest set is resolved by the
	// treatment; other open incidents must remain open so they still block
	// release until their own retest set is treated.
	if rs.IncidentID != "" {
		if err := s.store.CloseIncident(rs.IncidentID); err != nil {
			return domain.TreatmentRound{}, err
		}
	}
	return tr, nil
}
