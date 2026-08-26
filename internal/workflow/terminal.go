package workflow

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/verdict"
)

// SubmitReview records an independent review by a qualified person. Two
// distinct approvals advance the job to the terminal stage.
func (s *Service) SubmitReview(job domain.JobID, person string, approved bool) (domain.ReviewDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.store.LoadProgress(job)
	if err != nil {
		return domain.ReviewDecision{}, err
	}
	if p.Stage != domain.StageReview {
		return domain.ReviewDecision{}, &domain.ConflictError{Code: domain.ConflictStageOrder, Reason: string(p.Stage)}
	}
	if !s.catalog.Qualified(person, "review", p.Clock) {
		return domain.ReviewDecision{}, &domain.ValidationError{Field: "person", Reason: "not qualified for review"}
	}
	r := domain.ReviewDecision{
		JobID:    job,
		PersonID: person,
		Approved: approved,
		Digest: domain.MustDigest(struct {
			Job      domain.JobID `json:"job"`
			Person   string       `json:"person"`
			Approved bool         `json:"approved"`
		}{job, person, approved}),
	}
	if err := s.store.CreateReview(r); err != nil {
		return domain.ReviewDecision{}, err
	}
	return r, nil
}

// Terminal attempts the final release verdict. It assembles the arbitrator
// snapshot from persisted state, decides, and on approval writes the single
// terminal credential via the store's single-writer barrier.
func (s *Service) Terminal(job domain.JobID) (domain.TerminalVerdict, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok, err := s.store.TerminalCredential(job); err != nil {
		return domain.TerminalVerdict{}, err
	} else if ok {
		return existing, nil
	}

	snap, err := s.buildSnapshot(job)
	if err != nil {
		return domain.TerminalVerdict{}, err
	}
	dec := s.arbitrator.Decide(snap)
	if !dec.Approved {
		return domain.TerminalVerdict{
			JobID:   job,
			Reasons: dec.Reasons,
		}, &domain.ConflictError{Code: domain.ConflictStageOrder, Reason: strings.Join(dec.Reasons, "; ")}
	}
	cred, err := newCredential(job)
	if err != nil {
		return domain.TerminalVerdict{}, err
	}
	commit, err := s.store.WriteTerminal(job, cred)
	if err != nil {
		return domain.TerminalVerdict{}, err
	}
	// Advance the stage so the job is visibly terminal.
	if p, err := s.store.LoadProgress(job); err == nil {
		p.Stage = domain.StageTerminal
		_ = s.store.SaveProgress(job, p)
	}
	return domain.TerminalVerdict{JobID: job, Credential: cred, CommitNumber: commit}, nil
}

// buildSnapshot assembles the arbitrator snapshot from persisted state.
func (s *Service) buildSnapshot(job domain.JobID) (verdict.Snapshot, error) {
	topo, err := s.store.GetTopology(job)
	if err != nil {
		return verdict.Snapshot{}, err
	}
	events, err := s.store.ListEvents(job)
	if err != nil {
		return verdict.Snapshot{}, err
	}
	completed := map[domain.Stage]bool{}
	for _, ev := range events {
		completed[ev.Stage] = true
	}

	reviews, err := s.store.ListReviews(job)
	if err != nil {
		return verdict.Snapshot{}, err
	}
	openIncidents, err := s.store.OpenIncidents(job)
	if err != nil {
		return verdict.Snapshot{}, err
	}

	sampleResults := map[domain.SamplePointID]bool{}
	samples, err := s.store.ListSamples(job)
	if err != nil {
		return verdict.Snapshot{}, err
	}
	p, err := s.store.LoadProgress(job)
	if err != nil {
		return verdict.Snapshot{}, err
	}
	// Only current-round samples count toward coverage.
	for _, sm := range samples {
		if sm.Round != p.Round {
			continue
		}
		results, err := s.store.LabResultsForSample(sm.ID)
		if err != nil {
			return verdict.Snapshot{}, err
		}
		passed := false
		for _, r := range results {
			if r.Passed {
				passed = true
			}
		}
		sampleResults[sm.PointID] = passed
	}

	return verdict.Snapshot{
		Job:            domain.LockedJob{ID: job, Stage: p.Stage, Round: p.Round},
		Topology:       topo,
		ValvesVerified: completed[domain.StageIsolation],
		FlushWindowMet: completed[domain.StageFlush],
		ContactPassed:  completed[domain.StageContact],
		SampleResults:  sampleResults,
		OpenIncidents:  openIncidents,
		Reviews:        reviews,
	}, nil
}

// newCredential returns a random, uppercase release credential.
func newCredential(job domain.JobID) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "PW-" + strings.ToUpper(hex.EncodeToString(b)), nil
}
