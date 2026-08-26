package store

import (
	"database/sql"
	"errors"

	"example.com/potable-water-pipeline/internal/domain"
)

// CreateSample inserts a sample. The unique index on (job, point, round) plus
// the unique label guarantee one valid sample per sampling point per round.
func (s *Store) CreateSample(sm domain.Sample) error {
	_, err := s.db.Exec(`
INSERT INTO samples (id, job_id, point_id, round, label, digest, collected_by, sealed_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(sm.ID), string(sm.JobID), string(sm.PointID), sm.Round, sm.Label, sm.Digest, sm.CollectedBy, sm.SealedBy)
	return err
}

// GetSample returns a sample by id.
func (s *Store) GetSample(id domain.SampleID) (domain.Sample, error) {
	var sm domain.Sample
	err := s.db.QueryRow(`
SELECT id, job_id, point_id, round, label, digest, collected_by, sealed_by
FROM samples WHERE id = ?`, string(id)).
		Scan(&sm.ID, &sm.JobID, &sm.PointID, &sm.Round, &sm.Label, &sm.Digest, &sm.CollectedBy, &sm.SealedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Sample{}, ErrNotFound
	}
	return sm, err
}

// SampleForPoint returns the current-round sample for a point, if present.
func (s *Store) SampleForPoint(job domain.JobID, point domain.SamplePointID, round int) (domain.Sample, bool, error) {
	var sm domain.Sample
	err := s.db.QueryRow(`
SELECT id, job_id, point_id, round, label, digest, collected_by, sealed_by
FROM samples WHERE job_id = ? AND point_id = ? AND round = ?`, string(job), string(point), round).
		Scan(&sm.ID, &sm.JobID, &sm.PointID, &sm.Round, &sm.Label, &sm.Digest, &sm.CollectedBy, &sm.SealedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Sample{}, false, nil
	}
	if err != nil {
		return domain.Sample{}, false, err
	}
	return sm, true, nil
}

// ListSamples returns all samples for a job ordered by point id.
func (s *Store) ListSamples(job domain.JobID) ([]domain.Sample, error) {
	rows, err := s.db.Query(`
SELECT id, job_id, point_id, round, label, digest, collected_by, sealed_by
FROM samples WHERE job_id = ? ORDER BY point_id, round`, string(job))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Sample
	for rows.Next() {
		var sm domain.Sample
		if err := rows.Scan(&sm.ID, &sm.JobID, &sm.PointID, &sm.Round, &sm.Label, &sm.Digest, &sm.CollectedBy, &sm.SealedBy); err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

// CountSamples returns the number of samples recorded for a job. It is used
// to derive the next unique sample label sequence number.
func (s *Store) CountSamples(job domain.JobID) (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM samples WHERE job_id = ?`, string(job)).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// AppendCustody records a custody chain link.
func (s *Store) AppendCustody(ev domain.CustodyEvent) error {
	_, err := s.db.Exec(`
INSERT INTO custody (sample_id, from_party, to_party, clock, action)
VALUES (?, ?, ?, ?, ?)`,
		string(ev.SampleID), ev.From, ev.To, ev.Clock, ev.Action)
	return err
}

// CustodyForSample returns the custody trail for a sample in append order.
func (s *Store) CustodyForSample(id domain.SampleID) ([]domain.CustodyEvent, error) {
	rows, err := s.db.Query(`
SELECT sample_id, from_party, to_party, clock, action FROM custody WHERE sample_id = ? ORDER BY id`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CustodyEvent
	for rows.Next() {
		var ev domain.CustodyEvent
		if err := rows.Scan(&ev.SampleID, &ev.From, &ev.To, &ev.Clock, &ev.Action); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// CreateAttempt inserts a lab attempt.
func (s *Store) CreateAttempt(a domain.LabAttempt) error {
	_, err := s.db.Exec(`
INSERT INTO lab_attempts (id, sample_id, retry_number, status, calibration, digest, test_item)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, string(a.SampleID), a.RetryNumber, a.Status, a.Calibration, a.Digest, a.TestItem)
	return err
}

// GetAttempt returns a lab attempt by id.
func (s *Store) GetAttempt(id string) (domain.LabAttempt, error) {
	var a domain.LabAttempt
	err := s.db.QueryRow(`
SELECT id, sample_id, retry_number, status, calibration, digest, test_item
FROM lab_attempts WHERE id = ?`, id).
		Scan(&a.ID, &a.SampleID, &a.RetryNumber, &a.Status, &a.Calibration, &a.Digest, &a.TestItem)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.LabAttempt{}, ErrNotFound
	}
	return a, err
}

// UpdateAttempt persists changed attempt state (retry number, status).
func (s *Store) UpdateAttempt(a domain.LabAttempt) error {
	_, err := s.db.Exec(`
UPDATE lab_attempts SET retry_number = ?, status = ?, calibration = ? WHERE id = ?`,
		a.RetryNumber, a.Status, a.Calibration, a.ID)
	return err
}

// PendingAttempts returns lab attempts for a job that are not yet complete.
// These are the retryable detection calls recovered on restart.
func (s *Store) PendingAttempts(job domain.JobID) ([]domain.LabAttempt, error) {
	rows, err := s.db.Query(`
SELECT la.id, la.sample_id, la.retry_number, la.status, la.calibration, la.digest, la.test_item
FROM lab_attempts la JOIN samples s ON la.sample_id = s.id
WHERE s.job_id = ? AND la.status != ? ORDER BY la.id`, string(job), "complete")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LabAttempt
	for rows.Next() {
		var a domain.LabAttempt
		if err := rows.Scan(&a.ID, &a.SampleID, &a.RetryNumber, &a.Status, &a.Calibration, &a.Digest, &a.TestItem); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveLabResult stores a calibration-checked detection result.
func (s *Store) SaveLabResult(r domain.LabResult) error {
	_, err := s.db.Exec(`
INSERT INTO lab_results (attempt_id, sample_id, test_item, value, scale, passed)
VALUES (?, ?, ?, ?, ?, ?)`,
		r.AttemptID, string(r.SampleID), r.TestItem, r.Value.Value, r.Value.Scale, boolToInt(r.Passed))
	return err
}

// LabResultsForSample returns all results for a sample.
func (s *Store) LabResultsForSample(id domain.SampleID) ([]domain.LabResult, error) {
	rows, err := s.db.Query(`
SELECT attempt_id, sample_id, test_item, value, scale, passed FROM lab_results WHERE sample_id = ? ORDER BY id`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LabResult
	for rows.Next() {
		var r domain.LabResult
		var passed int
		if err := rows.Scan(&r.AttemptID, &r.SampleID, &r.TestItem, &r.Value.Value, &r.Value.Scale, &passed); err != nil {
			return nil, err
		}
		r.Passed = passed != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
