package workflow

import (
	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/hydraulic"
	"example.com/potable-water-pipeline/internal/store"
)

// AcquireLease coordinates a time-bounded exclusive hold on a resource and
// persists it so occupancy survives restart.
func (s *Service) AcquireLease(job domain.JobID, resource, holder string, clock, expires int64) (domain.ResourceLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, err := s.leases.Acquire(resource, holder, clock, expires)
	if err != nil {
		return domain.ResourceLease{}, err
	}
	if err := s.store.SaveLease(l); err != nil {
		_ = s.leases.Release(resource, holder, clock)
		return domain.ResourceLease{}, err
	}
	return l, nil
}

// ReleaseLease returns a resource to the pool. Only the current, unexpired
// holder may release.
func (s *Service) ReleaseLease(job domain.JobID, resource, holder string, clock int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.leases.Release(resource, holder, clock); err != nil {
		return err
	}
	if l, ok, err := s.store.LeaseForResource(resource); err != nil {
		return err
	} else if ok {
		if err := s.store.DeleteLease(l.ID); err != nil {
			return err
		}
	}
	return nil
}

// SubmitEvidence validates and applies a stage-bound evidence submission. It
// enforces stage order, logical-clock monotonicity, and idempotency before
// dispatching to the stage-specific handler.
func (s *Service) SubmitEvidence(ev domain.Evidence) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Idempotency is checked before stage/clock validation so a replay returns
	// the original result even after the job has advanced.
	digest := domain.MustDigest(ev)
	return s.idempotent(ev.OperationID, digest, ev.JobID, func() (any, error) {
		p, err := s.store.LoadProgress(ev.JobID)
		if err != nil {
			return nil, err
		}
		if p.Stage != ev.Stage {
			return nil, &domain.ConflictError{Code: domain.ConflictStageOrder, Reason: string(p.Stage)}
		}
		if !validKindForStage(ev.Stage, ev.Kind) {
			return nil, &domain.ValidationError{Field: "kind", Reason: string(ev.Kind)}
		}
		if ev.Clock <= p.Clock {
			return nil, &domain.ConflictError{Code: domain.ConflictClockRegression, Reason: "clock"}
		}
		next, err := s.applyEvidence(ev, p)
		if err != nil {
			return nil, err
		}
		return map[string]any{"job": string(ev.JobID), "stage": string(next.Stage), "clock": next.Clock}, nil
	})
}

// applyEvidence dispatches to the stage-specific logic and persists progress.
func (s *Service) applyEvidence(ev domain.Evidence, p store.Progress) (store.Progress, error) {
	targets, err := s.store.GetTargets(ev.JobID)
	if err != nil {
		return p, err
	}

	switch ev.Stage {
	case domain.StageIsolation:
		return s.applyIsolation(ev, p, targets)
	case domain.StageFlush:
		return s.applyFlush(ev, p, targets)
	case domain.StageDisinfect:
		return s.applyDisinfect(ev, p, targets)
	case domain.StageContact:
		return s.applyContact(ev, p, targets)
	case domain.StageReflush:
		return s.applyReflush(ev, p, targets)
	default:
		return p, &domain.ConflictError{Code: domain.ConflictStageOrder, Reason: string(ev.Stage)}
	}
}

// applyIsolation verifies every boundary valve is closed and the reviewer is
// qualified before allowing flushing.
func (s *Service) applyIsolation(ev domain.Evidence, p store.Progress, targets domain.JobTargets) (store.Progress, error) {
	topo, err := s.store.GetTopology(ev.JobID)
	if err != nil {
		return p, err
	}
	for _, v := range topo.Valves {
		closed, ok := ev.ValveStates[string(v.ID)]
		if !ok || !closed {
			return p, &domain.ValidationError{Field: "valve", Reason: string(v.ID) + " not verified closed"}
		}
	}
	if !s.catalog.Qualified(ev.PersonID, "isolation", ev.Clock) {
		return p, &domain.ValidationError{Field: "person", Reason: "reviewer not qualified for isolation"}
	}
	p.Clock = ev.Clock
	// Record the pressure-stability reading if supplied.
	if len(ev.Values) > 0 {
		if err := s.store.RecordMeasurement(ev.JobID, domain.MeasurementSeries{
			JobID: ev.JobID, InstrumentID: ev.InstrumentID, Clock: ev.Clock, Readings: ev.Values,
		}, "pressure"); err != nil {
			return p, err
		}
	}
	return s.advance(ev.JobID, p, ev.OperationID, struct {
		Stage  string          `json:"stage"`
		Valves map[string]bool `json:"valves"`
	}{string(ev.Stage), ev.ValveStates})
}

// applyFlush accumulates the continuous compliance window over flow and
// turbidity readings, and the cumulative turnover volume. The reading is
// persisted only after every constraint that can fail has been checked, so a
// failing submission (for example a turnover target of zero) leaves no partial
// reading in the timeline and a replay of the same operation does not append
// duplicate failed records.
func (s *Service) applyFlush(ev domain.Evidence, p store.Progress, targets domain.JobTargets) (store.Progress, error) {
	if len(ev.Values) < 2 {
		return p, &domain.ValidationError{Field: "values", Reason: "flow and turbidity required"}
	}
	flow := ev.Values[0]
	turbidity := ev.Values[1]

	dt := ev.Clock - p.Clock
	flowVol := flow.Value * dt
	if flowVol > 0 {
		p.TurnoverCum += flowVol
	}

	compliant := ge(flow, targets.MinFlow) && le(turbidity, targets.MaxTurbidity)
	if compliant {
		p.FlushWindow++
	} else {
		p.FlushWindow = 0
	}
	p.Clock = ev.Clock

	// Resolve the turnover requirement before persisting anything: a window
	// that cannot close because of an invalid target must fail without writing
	// a reading, so failed evidence never pollutes the historical timeline.
	canAdvance := false
	if p.FlushWindow >= targets.MinWindowCount {
		// The flush may only close once the cumulative flow has replaced the
		// required pipeline volume (the turnover factor).
		topo, err := s.store.GetTopology(ev.JobID)
		if err != nil {
			return p, err
		}
		totalVol, err := topo.TotalVolume(hydraulic.VolumeLitres)
		if err != nil {
			return p, err
		}
		required, err := hydraulic.RequiredVolume(targets.TurnoverTarget, targets.TurnoverScale, totalVol)
		if err != nil {
			return p, err
		}
		canAdvance = hydraulic.MetTurnover(int(p.TurnoverCum), required)
	}

	// All checks passed; the reading is now safe to persist. A submission that
	// failed validation above returns before this point, leaving no trace.
	if err := s.store.RecordMeasurement(ev.JobID, domain.MeasurementSeries{
		JobID: ev.JobID, InstrumentID: ev.InstrumentID, Clock: ev.Clock, Readings: ev.Values,
	}, "flush"); err != nil {
		return p, err
	}

	if canAdvance {
		return s.advance(ev.JobID, p, ev.OperationID, struct {
			Stage  string `json:"stage"`
			Window int64  `json:"window"`
		}{string(ev.Stage), p.FlushWindow})
	}
	if err := s.store.SaveProgress(ev.JobID, p); err != nil {
		return p, err
	}
	return p, nil
}

// applyDisinfect records the exact disinfectant dose under a held injection
// lease and advances to the contact stage.
func (s *Service) applyDisinfect(ev domain.Evidence, p store.Progress, targets domain.JobTargets) (store.Progress, error) {
	if ev.LeaseID == "" {
		return p, &domain.ValidationError{Field: "lease", Reason: "injection lease required"}
	}
	l, ok, err := s.store.GetLease(domain.LeaseID(ev.LeaseID))
	if err != nil {
		return p, err
	}
	if !ok || l.Expires <= ev.Clock {
		return p, &domain.ConflictError{Code: domain.ConflictLeaseExpired, Reason: ev.LeaseID}
	}
	if len(ev.Values) < 1 || ev.Values[0].Value <= 0 {
		return p, &domain.ValidationError{Field: "values", Reason: "positive dose amount required"}
	}
	dose := domain.ChemicalDose{
		JobID:       ev.JobID,
		InjectionID: domain.InjectionID(ev.InstrumentID),
		Clock:       ev.Clock,
		Amount:      ev.Values[0],
	}
	if err := s.store.RecordDose(ev.JobID, dose); err != nil {
		return p, err
	}
	p.Clock = ev.Clock
	return s.advance(ev.JobID, p, ev.OperationID, struct {
		Stage  string          `json:"stage"`
		Amount domain.Quantity `json:"amount"`
	}{string(ev.Stage), dose.Amount})
}

// applyContact integrates chlorine concentration over logical time and checks
// the initial, terminal, and concentration-time criteria.
func (s *Service) applyContact(ev domain.Evidence, p store.Progress, targets domain.JobTargets) (store.Progress, error) {
	if len(ev.Values) < 1 {
		return p, &domain.ValidationError{Field: "values", Reason: "chlorine concentration required"}
	}
	conc := ev.Values[0]
	if conc.Value < 0 {
		return p, &domain.ValidationError{Field: "values", Reason: "negative concentration"}
	}
	if err := s.store.RecordMeasurement(ev.JobID, domain.MeasurementSeries{
		JobID: ev.JobID, InstrumentID: ev.InstrumentID, Clock: ev.Clock, Readings: ev.Values,
	}, "chlorine"); err != nil {
		return p, err
	}

	if !p.ContactStarted {
		p.ContactStarted = true
		p.ContactStartClock = ev.Clock
		p.ContactLastClock = ev.Clock
		p.ContactInitial = conc.Value
		p.ContactLastConc = conc.Value
		p.ContactCT = 0
	} else {
		dt := ev.Clock - p.ContactLastClock
		p.ContactCT += dt * conc.Value
		p.ContactLastClock = ev.Clock
		p.ContactLastConc = conc.Value
	}
	p.Clock = ev.Clock

	initialOK := p.ContactInitial >= targets.MinInitialConc.Value
	terminalOK := p.ContactLastConc >= targets.MinTerminalConc.Value
	ctOK := p.ContactCT >= targets.MinCT.Value
	durationOK := (p.ContactLastClock - p.ContactStartClock) >= targets.ContactDuration

	if initialOK && terminalOK && ctOK && durationOK {
		return s.advance(ev.JobID, p, ev.OperationID, struct {
			Stage string `json:"stage"`
			CT    int64  `json:"ct"`
		}{string(ev.Stage), p.ContactCT})
	}
	if err := s.store.SaveProgress(ev.JobID, p); err != nil {
		return p, err
	}
	return p, nil
}

// applyReflush records discharge reflush evidence and advances to sampling.
func (s *Service) applyReflush(ev domain.Evidence, p store.Progress, targets domain.JobTargets) (store.Progress, error) {
	if err := s.store.RecordMeasurement(ev.JobID, domain.MeasurementSeries{
		JobID: ev.JobID, InstrumentID: ev.InstrumentID, Clock: ev.Clock, Readings: ev.Values,
	}, "reflush"); err != nil {
		return p, err
	}
	p.Clock = ev.Clock
	return s.advance(ev.JobID, p, ev.OperationID, struct {
		Stage string `json:"stage"`
	}{string(ev.Stage)})
}

// ge and le compare fixed-point quantities, treating mismatched scales via
// hydraulic.Cmp. A comparison error is treated as not-greater/not-less.
func ge(a, b domain.Quantity) bool {
	c, err := hydraulic.Cmp(a, b)
	return err == nil && c >= 0
}

func le(a, b domain.Quantity) bool {
	c, err := hydraulic.Cmp(a, b)
	return err == nil && c <= 0
}

// validKindForStage reports whether an evidence kind is accepted by the given
// stage. It is the single place that ties evidence kinds to workflow stages,
// rejecting mismatched submissions before any state is written.
func validKindForStage(stage domain.Stage, kind domain.EvidenceKind) bool {
	switch stage {
	case domain.StageIsolation:
		return kind == domain.EvidenceValve || kind == domain.EvidencePressure
	case domain.StageFlush:
		return kind == domain.EvidenceFlow || kind == domain.EvidenceTurbidity
	case domain.StageDisinfect:
		return kind == domain.EvidenceDose
	case domain.StageContact:
		return kind == domain.EvidenceChlorine || kind == domain.EvidenceContact
	case domain.StageReflush:
		return kind == domain.EvidenceReflush
	default:
		return false
	}
}
