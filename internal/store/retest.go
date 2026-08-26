package store

import (
	"database/sql"
	"errors"

	"example.com/potable-water-pipeline/internal/domain"
)

// CountIncidents returns the number of incidents recorded for a job.
func (s *Store) CountIncidents(job domain.JobID) (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM incidents WHERE job_id = ?`, string(job)).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// CountRetestSets returns the number of retest sets recorded for a job.
func (s *Store) CountRetestSets(job domain.JobID) (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM retest_sets WHERE job_id = ?`, string(job)).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// CreateIncident records an anomaly seed.
func (s *Store) CreateIncident(inc domain.Incident) error {
	_, err := s.db.Exec(`
INSERT INTO incidents (id, job_id, kind, clock, closed) VALUES (?, ?, ?, ?, ?)`,
		inc.ID, string(inc.JobID), inc.Kind, inc.Clock, boolToInt(inc.Closed))
	return err
}

// ListIncidents returns all incidents for a job ordered by id.
func (s *Store) ListIncidents(job domain.JobID) ([]domain.Incident, error) {
	rows, err := s.db.Query(`SELECT id, job_id, kind, clock, closed FROM incidents WHERE job_id = ? ORDER BY id`, string(job))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Incident
	for rows.Next() {
		var inc domain.Incident
		var closed int
		if err := rows.Scan(&inc.ID, &inc.JobID, &inc.Kind, &inc.Clock, &closed); err != nil {
			return nil, err
		}
		inc.Closed = closed != 0
		out = append(out, inc)
	}
	return out, rows.Err()
}

// OpenIncidents returns the still-open incidents for a job, used by the
// terminal arbitrator.
func (s *Store) OpenIncidents(job domain.JobID) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM incidents WHERE job_id = ? AND closed = 0 ORDER BY id`, string(job))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CloseIncident marks an incident closed.
func (s *Store) CloseIncident(id string) error {
	_, err := s.db.Exec(`UPDATE incidents SET closed = 1 WHERE id = ?`, id)
	return err
}

// CreateRetestSet stores a stable retest set with its members serialized.
func (s *Store) CreateRetestSet(rs domain.RetestSet) error {
	members, err := encodeJSON(rs.Members)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO retest_sets (id, job_id, round, members_json) VALUES (?, ?, ?, ?)`,
		rs.ID, string(rs.JobID), rs.Round, members)
	return err
}

// ListRetestSets returns the retest sets for a job ordered by id.
func (s *Store) ListRetestSets(job domain.JobID) ([]domain.RetestSet, error) {
	rows, err := s.db.Query(`SELECT id, job_id, round, members_json FROM retest_sets WHERE job_id = ? ORDER BY id`, string(job))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RetestSet
	for rows.Next() {
		var rs domain.RetestSet
		var members string
		if err := rows.Scan(&rs.ID, &rs.JobID, &rs.Round, &members); err != nil {
			return nil, err
		}
		if err := decodeJSON(members, &rs.Members); err != nil {
			return nil, err
		}
		out = append(out, rs)
	}
	return out, rows.Err()
}

// GetRetestSet returns a single retest set by id.
func (s *Store) GetRetestSet(id string) (domain.RetestSet, error) {
	var rs domain.RetestSet
	var members string
	err := s.db.QueryRow(`SELECT id, job_id, round, members_json FROM retest_sets WHERE id = ?`, id).
		Scan(&rs.ID, &rs.JobID, &rs.Round, &members)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RetestSet{}, ErrNotFound
	}
	if err != nil {
		return domain.RetestSet{}, err
	}
	if err := decodeJSON(members, &rs.Members); err != nil {
		return domain.RetestSet{}, err
	}
	return rs, nil
}

// CreateTreatmentRound records a new, strictly increasing round.
func (s *Store) CreateTreatmentRound(tr domain.TreatmentRound) error {
	_, err := s.db.Exec(`INSERT INTO treatment_rounds (id, job_id, retest_id, round) VALUES (?, ?, ?, ?)`,
		tr.ID, string(tr.JobID), tr.RetestID, tr.Round)
	return err
}

// MaxRound returns the highest treatment round recorded for a job (0 if none).
func (s *Store) MaxRound(job domain.JobID) (int, error) {
	var max sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(round) FROM treatment_rounds WHERE job_id = ?`, string(job)).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64), nil
}

// CreateReview stores an independent review.
func (s *Store) CreateReview(r domain.ReviewDecision) error {
	_, err := s.db.Exec(`INSERT INTO reviews (job_id, person_id, approved, digest) VALUES (?, ?, ?, ?)`,
		string(r.JobID), r.PersonID, boolToInt(r.Approved), r.Digest)
	return err
}

// ListReviews returns all reviews for a job ordered by id.
func (s *Store) ListReviews(job domain.JobID) ([]domain.ReviewDecision, error) {
	rows, err := s.db.Query(`SELECT job_id, person_id, approved, digest FROM reviews WHERE job_id = ? ORDER BY id`, string(job))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ReviewDecision
	for rows.Next() {
		var r domain.ReviewDecision
		var approved int
		if err := rows.Scan(&r.JobID, &r.PersonID, &approved, &r.Digest); err != nil {
			return nil, err
		}
		r.Approved = approved != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// TerminalCredential returns the persisted terminal verdict, if any.
func (s *Store) TerminalCredential(job domain.JobID) (domain.TerminalVerdict, bool, error) {
	var v domain.TerminalVerdict
	var cred string
	var commit int64
	err := s.db.QueryRow(`SELECT terminal_credential, terminal_commit FROM jobs WHERE id = ?`, string(job)).Scan(&cred, &commit)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TerminalVerdict{}, false, ErrNotFound
	}
	if err != nil {
		return domain.TerminalVerdict{}, false, err
	}
	if cred == "" {
		return domain.TerminalVerdict{}, false, nil
	}
	v.JobID = job
	v.Credential = cred
	v.CommitNumber = commit
	return v, true, nil
}

// WriteTerminal records the single terminal verdict. It is a single-writer
// barrier: the transaction verifies no credential is already present and only
// then writes it. Concurrent callers serialize on the single connection; only
// the first succeeds, the rest observe the persisted result.
func (s *Store) WriteTerminal(job domain.JobID, credential string) (int64, error) {
	var commit int64
	err := s.tx(func(tx *sql.Tx) error {
		var existing string
		if err := tx.QueryRow(`SELECT terminal_credential FROM jobs WHERE id = ?`, string(job)).Scan(&existing); err != nil {
			return err
		}
		if existing != "" {
			return &domain.ConflictError{Code: domain.ConflictTerminalExists, Reason: string(job)}
		}
		c, err := nextCommit(tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE jobs SET terminal_credential = ?, terminal_commit = ? WHERE id = ?`, credential, c, string(job)); err != nil {
			return err
		}
		commit = c
		return nil
	})
	return commit, err
}
