package store

import (
	"database/sql"
	"errors"
	"sort"

	"example.com/potable-water-pipeline/internal/domain"
	"example.com/potable-water-pipeline/internal/topology"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// Progress is the mutable workflow state that is persisted alongside a job and
// fully recovered on restart.
type Progress struct {
	Clock             int64
	Stage             domain.Stage
	Round             int
	FlushWindow       int64
	ContactStarted    bool
	ContactStartClock int64
	ContactLastClock  int64
	ContactInitial    int64
	ContactLastConc   int64
	ContactCT         int64
	TurnoverCum       int64
}

// CreateJob inserts a locked job, its immutable topology, and its hydraulic
// targets in one transaction.
func (s *Store) CreateJob(job domain.LockedJob, topo topology.LockedTopology, targets domain.JobTargets) error {
	topoJSON, err := encodeJSON(topo)
	if err != nil {
		return err
	}
	targetsJSON, err := encodeJSON(targets)
	if err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
INSERT INTO jobs (id, topology_digest, topology_json, targets_json, rule_digest, rule_version, clock, stage, round)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(job.ID), job.TopologyDigest, topoJSON, targetsJSON, job.RuleDigest, job.RuleVersion, job.Clock, string(job.Stage), job.Round)
		return err
	})
}

// GetTargets returns the hydraulic and disinfection targets for a job.
func (s *Store) GetTargets(id domain.JobID) (domain.JobTargets, error) {
	var data string
	err := s.db.QueryRow(`SELECT targets_json FROM jobs WHERE id = ?`, string(id)).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.JobTargets{}, ErrNotFound
	}
	if err != nil {
		return domain.JobTargets{}, err
	}
	var t domain.JobTargets
	if err := decodeJSON(data, &t); err != nil {
		return domain.JobTargets{}, err
	}
	return t, nil
}

// GetJob returns the basic job summary reconstructed from persisted state.
func (s *Store) GetJob(id domain.JobID) (domain.LockedJob, error) {
	var j domain.LockedJob
	err := s.db.QueryRow(`
SELECT id, topology_digest, rule_digest, rule_version, clock, stage, round
FROM jobs WHERE id = ?`, string(id)).
		Scan(&j.ID, &j.TopologyDigest, &j.RuleDigest, &j.RuleVersion, &j.Clock, &j.Stage, &j.Round)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.LockedJob{}, ErrNotFound
	}
	return j, err
}

// ListJobs returns all jobs sorted by canonical identifier.
func (s *Store) ListJobs() ([]domain.LockedJob, error) {
	rows, err := s.db.Query(`SELECT id, topology_digest, rule_digest, rule_version, clock, stage, round FROM jobs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []domain.LockedJob
	for rows.Next() {
		var j domain.LockedJob
		if err := rows.Scan(&j.ID, &j.TopologyDigest, &j.RuleDigest, &j.RuleVersion, &j.Clock, &j.Stage, &j.Round); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	return jobs, rows.Err()
}

// GetTopology returns the immutable topology for a job.
func (s *Store) GetTopology(id domain.JobID) (topology.LockedTopology, error) {
	var data string
	err := s.db.QueryRow(`SELECT topology_json FROM jobs WHERE id = ?`, string(id)).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return topology.LockedTopology{}, ErrNotFound
	}
	if err != nil {
		return topology.LockedTopology{}, err
	}
	var t topology.LockedTopology
	if err := decodeJSON(data, &t); err != nil {
		return topology.LockedTopology{}, err
	}
	return t, nil
}

// LoadProgress returns the mutable workflow state for a job.
func (s *Store) LoadProgress(id domain.JobID) (Progress, error) {
	var p Progress
	var started int
	err := s.db.QueryRow(`
SELECT clock, stage, round, flush_window, contact_started, contact_start_clock, contact_last_clock, contact_initial, contact_last_conc, contact_ct, turnover_cum
FROM jobs WHERE id = ?`, string(id)).
		Scan(&p.Clock, &p.Stage, &p.Round, &p.FlushWindow, &started, &p.ContactStartClock, &p.ContactLastClock, &p.ContactInitial, &p.ContactLastConc, &p.ContactCT, &p.TurnoverCum)
	if errors.Is(err, sql.ErrNoRows) {
		return Progress{}, ErrNotFound
	}
	p.ContactStarted = started != 0
	return p, err
}

// SaveProgress persists the mutable workflow state for a job.
func (s *Store) SaveProgress(id domain.JobID, p Progress) error {
	started := 0
	if p.ContactStarted {
		started = 1
	}
	_, err := s.db.Exec(`
UPDATE jobs SET clock = ?, stage = ?, round = ?, flush_window = ?, contact_started = ?, contact_start_clock = ?, contact_last_clock = ?, contact_initial = ?, contact_last_conc = ?, contact_ct = ?, turnover_cum = ?
WHERE id = ?`,
		p.Clock, string(p.Stage), p.Round, p.FlushWindow, started, p.ContactStartClock, p.ContactLastClock, p.ContactInitial, p.ContactLastConc, p.ContactCT, p.TurnoverCum, string(id))
	return err
}

// AppendEvent records an append-only workflow event and advances the job
// clock/stage atomically, returning the new commit number.
func (s *Store) AppendEvent(id domain.JobID, ev domain.WorkflowEvent, newClock int64, newStage domain.Stage) (int64, error) {
	var commit int64
	err := s.tx(func(tx *sql.Tx) error {
		c, err := nextCommit(tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`
INSERT INTO events (job_id, stage, clock, operation_id, digest, round)
VALUES (?, ?, ?, ?, ?, ?)`,
			string(id), string(ev.Stage), ev.Clock, ev.OperationID, ev.Digest, ev.Round); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE jobs SET clock = ?, stage = ? WHERE id = ?`, newClock, string(newStage), string(id)); err != nil {
			return err
		}
		commit = c
		return nil
	})
	return commit, err
}

// ListEvents returns the job timeline in append order.
func (s *Store) ListEvents(id domain.JobID) ([]domain.WorkflowEvent, error) {
	rows, err := s.db.Query(`SELECT job_id, stage, clock, operation_id, digest, round FROM events WHERE job_id = ? ORDER BY id`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.WorkflowEvent
	for rows.Next() {
		var ev domain.WorkflowEvent
		if err := rows.Scan(&ev.JobID, &ev.Stage, &ev.Clock, &ev.OperationID, &ev.Digest, &ev.Round); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// RecordMeasurement stores a fixed-point reading for the timeline.
func (s *Store) RecordMeasurement(job domain.JobID, m domain.MeasurementSeries, kind string) error {
	for _, r := range m.Readings {
		if _, err := s.db.Exec(`
INSERT INTO measurements (job_id, instrument_id, kind, clock, value, scale) VALUES (?, ?, ?, ?, ?, ?)`,
			string(job), m.InstrumentID, kind, m.Clock, r.Value, r.Scale); err != nil {
			return err
		}
	}
	return nil
}

// ListMeasurements returns the job's readings in append order.
func (s *Store) ListMeasurements(job domain.JobID) ([]domain.MeasurementSeries, error) {
	rows, err := s.db.Query(`SELECT job_id, instrument_id, kind, clock, value, scale FROM measurements WHERE job_id = ? ORDER BY id`, string(job))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type row struct {
		instrument string
		kind       string
		clock      int64
		q          domain.Quantity
	}
	var rows2 []row
	for rows.Next() {
		var r row
		var j domain.JobID
		if err := rows.Scan(&j, &r.instrument, &r.kind, &r.clock, &r.q.Value, &r.q.Scale); err != nil {
			return nil, err
		}
		rows2 = append(rows2, r)
	}
	// Group consecutive same (instrument, clock) entries into series.
	var out []domain.MeasurementSeries
	for _, r := range rows2 {
		n := len(out)
		if n > 0 && out[n-1].InstrumentID == r.instrument && out[n-1].Clock == r.clock {
			out[n-1].Readings = append(out[n-1].Readings, r.q)
			continue
		}
		out = append(out, domain.MeasurementSeries{
			JobID:        job,
			InstrumentID: r.instrument,
			Clock:        r.clock,
			Readings:     []domain.Quantity{r.q},
		})
	}
	return out, rows.Err()
}

// RecordDose stores an exact disinfectant injection amount.
func (s *Store) RecordDose(job domain.JobID, d domain.ChemicalDose) error {
	_, err := s.db.Exec(`INSERT INTO doses (job_id, injection_id, clock, value, scale) VALUES (?, ?, ?, ?, ?)`,
		string(job), string(d.InjectionID), d.Clock, d.Amount.Value, d.Amount.Scale)
	return err
}

// ListDoses returns the job's recorded doses in append order.
func (s *Store) ListDoses(job domain.JobID) ([]domain.ChemicalDose, error) {
	rows, err := s.db.Query(`SELECT job_id, injection_id, clock, value, scale FROM doses WHERE job_id = ? ORDER BY id`, string(job))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ChemicalDose
	for rows.Next() {
		var d domain.ChemicalDose
		if err := rows.Scan(&d.JobID, &d.InjectionID, &d.Clock, &d.Amount.Value, &d.Amount.Scale); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
