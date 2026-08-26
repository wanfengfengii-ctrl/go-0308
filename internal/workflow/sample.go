package workflow

import (
	"fmt"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/sample"
	"example.com/potable-water-pipeline/internal/topology"
)

// CreateSample collects a sample at a locked sampling point for the current
// round. It enforces one valid sample per point per round and advances to the
// review stage once every sampling point has a sample.
func (s *Service) CreateSample(job domain.JobID, point domain.SamplePointID, collector, sealer string, clock int64) (domain.Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.store.LoadProgress(job)
	if err != nil {
		return domain.Sample{}, err
	}
	if p.Stage != domain.StageSampling {
		return domain.Sample{}, &domain.ConflictError{Code: domain.ConflictStageOrder, Reason: string(p.Stage)}
	}
	topo, err := s.store.GetTopology(job)
	if err != nil {
		return domain.Sample{}, err
	}
	if !topo.HasSamplingPoint(point) {
		return domain.Sample{}, &domain.ValidationError{Field: "point", Reason: string(point) + " not a locked sampling point"}
	}
	if _, ok, err := s.store.SampleForPoint(job, point, p.Round); err != nil {
		return domain.Sample{}, err
	} else if ok {
		return domain.Sample{}, &domain.ConflictError{Code: domain.ConflictRound, Reason: "sample already exists for point in round"}
	}

	seq, err := s.store.CountSamples(job)
	if err != nil {
		return domain.Sample{}, err
	}
	sm := domain.Sample{
		ID:          domain.SampleID(fmt.Sprintf("s-%d", seq+1)),
		JobID:       job,
		PointID:     point,
		Round:       p.Round,
		Label:       sample.GenerateLabel(job, point, p.Round, seq+1),
		CollectedBy: collector,
		SealedBy:    sealer,
	}
	sm.Digest = sample.SampleDigest(sm)
	if err := s.store.CreateSample(sm); err != nil {
		return domain.Sample{}, err
	}

	// Advance when every sampling point has a current-round sample.
	all, err := s.allSampled(job, topo, p.Round)
	if err != nil {
		return domain.Sample{}, err
	}
	if all {
		p.Clock = clock
		if _, err := s.advance(job, p, "", struct {
			Stage string `json:"stage"`
		}{string(domain.StageSampling)}); err != nil {
			return domain.Sample{}, err
		}
	}
	return sm, nil
}

// AppendCustody appends a link to a sample's custody chain.
func (s *Service) AppendCustody(sampleID domain.SampleID, from, to, action string, clock int64) (domain.CustodyEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := domain.CustodyEvent{SampleID: sampleID, From: from, To: to, Action: action, Clock: clock}
	if err := s.store.AppendCustody(ev); err != nil {
		return domain.CustodyEvent{}, err
	}
	return ev, nil
}

// CreateAttempt creates a pending detection attempt for a sample, recording
// the instrument calibration status derived from the rule catalog.
func (s *Service) CreateAttempt(sampleID domain.SampleID, testItem, instrumentKind string, calibratedAt, clock int64) (domain.LabAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sm, err := s.store.GetSample(sampleID)
	if err != nil {
		return domain.LabAttempt{}, err
	}
	days := s.catalog.CalibrationDays(instrumentKind)
	calibrated := sample.CalibratedWithin(calibratedAt, clock, days)
	status := sample.StatusPending
	cal := "valid"
	if !calibrated {
		cal = "stale"
	}
	a := sample.NewAttempt("la-"+string(sm.ID), sm, testItem)
	a.Calibration = cal
	a.Status = status
	a.TestItem = testItem
	if err := s.store.CreateAttempt(a); err != nil {
		return domain.LabAttempt{}, err
	}
	return a, nil
}

// GetAttempt returns a lab attempt by id.
func (s *Service) GetAttempt(attemptID string) (domain.LabAttempt, error) {
	return s.store.GetAttempt(attemptID)
}

// RetryAttempt increments the retry number of a retryable attempt and marks it
// pending again. It refuses to retry a completed attempt.
func (s *Service) RetryAttempt(attemptID string) (domain.LabAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.store.GetAttempt(attemptID)
	if err != nil {
		return domain.LabAttempt{}, err
	}
	if !sample.Retryable(a, 3) {
		return domain.LabAttempt{}, &domain.ConflictError{Code: domain.ConflictContentMismatch, Reason: "attempt not retryable"}
	}
	a.RetryNumber++
	a.Status = sample.StatusPending
	if err := s.store.UpdateAttempt(a); err != nil {
		return domain.LabAttempt{}, err
	}
	return a, nil
}

// SubmitResult validates a lab receipt against its attempt and calibration
// status, and stores a passed or failed conclusion. A stale or mismatched
// receipt leaves the attempt retryable rather than forming a conclusion.
func (s *Service) SubmitResult(attemptID, testItem string, value domain.Quantity, passed, calibrated bool) (domain.LabResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.store.GetAttempt(attemptID)
	if err != nil {
		return domain.LabResult{}, err
	}
	if a.Status == sample.StatusComplete {
		return domain.LabResult{}, &domain.ConflictError{Code: domain.ConflictContentMismatch, Reason: "attempt already complete"}
	}
	sm, err := s.store.GetSample(a.SampleID)
	if err != nil {
		return domain.LabResult{}, err
	}
	_, err = sample.ValidateResult(a, sm, testItem, calibrated, value, passed)
	if err != nil {
		// Record as retryable; do not form a conclusion.
		a.Status = sample.StatusRetryable
		_ = s.store.UpdateAttempt(a)
		return domain.LabResult{}, &domain.ValidationError{Field: "result", Reason: err.Error()}
	}
	a.Status = sample.StatusComplete
	if err := s.store.UpdateAttempt(a); err != nil {
		return domain.LabResult{}, err
	}
	res := domain.LabResult{
		AttemptID: attemptID,
		SampleID:  a.SampleID,
		TestItem:  testItem,
		Value:     value,
		Passed:    passed,
	}
	if err := s.store.SaveLabResult(res); err != nil {
		return domain.LabResult{}, err
	}
	return res, nil
}

// allSampled reports whether every locked sampling point has a sample in the
// given round.
func (s *Service) allSampled(job domain.JobID, topo topology.LockedTopology, round int) (bool, error) {
	for _, sp := range topo.SamplingPoints() {
		if _, ok, err := s.store.SampleForPoint(job, sp.ID, round); err != nil {
			return false, err
		} else if !ok {
			return false, nil
		}
	}
	return true, nil
}
