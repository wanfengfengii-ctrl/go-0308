package workflow

import (
	"encoding/json"
	"sync"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/lease"
	"example.com/potable-water-pipeline/internal/rules"
	"example.com/potable-water-pipeline/internal/store"
	"example.com/potable-water-pipeline/internal/topology"
	"example.com/potable-water-pipeline/internal/verdict"
)

// Service orchestrates the full disinfection-release workflow. It is the
// single entry point used by the HTTP layer and coordinates the rules locker,
// hydraulic calculations, resource leases, sample chain, incident retest, and
// terminal arbitration, persisting every transition through the store.
type Service struct {
	mu         sync.Mutex
	store      *store.Store
	catalog    *rules.Catalog
	locker     *rules.Locker
	arbitrator *verdict.Arbitrator
	leases     *lease.Manager
}

// New builds a Service backed by the given store and rule catalog.
func New(st *store.Store, catalog *rules.Catalog) (*Service, error) {
	s := &Service{
		store:      st,
		catalog:    catalog,
		locker:     rules.NewLocker(catalog),
		arbitrator: verdict.NewArbitrator(),
		leases:     lease.NewManager(),
	}
	return s, nil
}

// CreateJob locks a topology and persists the new job. It is idempotent on the
// job id: replaying the same request returns the already-created job.
func (s *Service) CreateJob(id domain.JobID, req domain.CreateJobRequest) (domain.LockedJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, err := s.store.GetJob(id); err == nil {
		return existing, nil
	} else if err != store.ErrNotFound {
		return domain.LockedJob{}, err
	}

	t := topology.Topology{
		Nodes:      req.Topology.Nodes,
		Sections:   req.Topology.Sections,
		Valves:     req.Topology.Valves,
		Outlets:    req.Topology.Outlets,
		Injections: req.Topology.Injections,
		Sampling:   req.Topology.Sampling,
	}
	res, err := s.locker.Lock(id, t, req.RuleVer)
	if err != nil {
		return domain.LockedJob{}, err
	}
	if err := s.store.CreateJob(res.Job, res.Topology, req.Targets); err != nil {
		return domain.LockedJob{}, err
	}
	return res.Job, nil
}

// GetJob returns the current job summary.
func (s *Service) GetJob(id domain.JobID) (domain.LockedJob, error) {
	return s.store.GetJob(id)
}

// ListJobs returns all jobs in canonical order.
func (s *Service) ListJobs() ([]domain.LockedJob, error) {
	return s.store.ListJobs()
}

// GetTopology returns the immutable topology for a job.
func (s *Service) GetTopology(id domain.JobID) (topology.LockedTopology, error) {
	return s.store.GetTopology(id)
}

// Timeline returns the append-only workflow events for a job.
func (s *Service) Timeline(id domain.JobID) ([]domain.WorkflowEvent, error) {
	return s.store.ListEvents(id)
}

// Measurements returns the recorded readings for a job, used by the frontend
// timeline.
func (s *Service) Measurements(id domain.JobID) ([]domain.MeasurementSeries, error) {
	return s.store.ListMeasurements(id)
}

// Samples returns the recorded samples for a job.
func (s *Service) Samples(id domain.JobID) ([]domain.Sample, error) {
	return s.store.ListSamples(id)
}

// Retests returns the retest sets for a job.
func (s *Service) Retests(id domain.JobID) ([]domain.RetestSet, error) {
	return s.store.ListRetestSets(id)
}

// idempotent guards a write operation with an operation id and canonical
// digest. Replaying the same content returns the original result; different
// content for the same operation id yields a stable conflict.
func (s *Service) idempotent(operationID, digest string, job domain.JobID, fn func() (any, error)) (any, error) {
	if operationID != "" {
		gotDigest, resultJSON, found, err := s.store.ReceiptResult(operationID)
		if err != nil {
			return nil, err
		}
		if found {
			if gotDigest != digest {
				return nil, &domain.ConflictError{Code: domain.ConflictContentMismatch, Reason: operationID}
			}
			var result any
			if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
				return nil, err
			}
			return result, nil
		}
	}
	result, err := fn()
	if err != nil {
		return nil, err
	}
	if operationID != "" {
		if _, err := s.store.PutReceipt(job, operationID, digest, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// advance appends a workflow event and moves the job to the next stage,
// persisting both atomically.
func (s *Service) advance(id domain.JobID, p store.Progress, operationID string, payload any) (store.Progress, error) {
	digest := domain.MustDigest(payload)
	ev := domain.WorkflowEvent{
		JobID:       id,
		Stage:       p.Stage,
		Clock:       p.Clock,
		OperationID: operationID,
		Digest:      digest,
		Round:       p.Round,
	}
	nextStage := p.Stage.Next()
	if !nextStage.Valid() {
		return p, &domain.ConflictError{Code: domain.ConflictStageOrder, Reason: string(p.Stage)}
	}
	if _, err := s.store.AppendEvent(id, ev, p.Clock, nextStage); err != nil {
		return p, err
	}
	p.Stage = nextStage
	return p, nil
}
