package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"example.com/potable-water-pipeline/internal/domain"
)

// GetReceipt returns the idempotent receipt for an operation id scoped to a
// job. Operation ids are per-job: the same local id (e.g. "op-1") replayed by
// two independent jobs must not collide, so receipts are partitioned by job.
func (s *Store) GetReceipt(job domain.JobID, operationID string) (domain.OperationReceipt, bool, error) {
	var r domain.OperationReceipt
	var resultJSON string
	err := s.db.QueryRow(`
SELECT operation_id, digest, commit_number, result_json FROM receipts WHERE job_id = ? AND operation_id = ?`, string(job), operationID).
		Scan(&r.OperationID, &r.Digest, &r.CommitNumber, &resultJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.OperationReceipt{}, false, nil
	}
	if err != nil {
		return domain.OperationReceipt{}, false, err
	}
	return r, true, nil
}

// ReceiptResult returns the digest and stored result JSON for an operation id
// scoped to a job.
func (s *Store) ReceiptResult(job domain.JobID, operationID string) (digest, resultJSON string, found bool, err error) {
	err = s.db.QueryRow(`SELECT digest, result_json FROM receipts WHERE job_id = ? AND operation_id = ?`, string(job), operationID).
		Scan(&digest, &resultJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return digest, resultJSON, true, nil
}

// PutReceipt records an idempotent receipt and returns its commit number. The
// (job, operation id) pair is the primary key; a second distinct write for the
// same pair fails with a unique constraint violation, which the workflow maps
// to a stable conflict code. Distinct jobs share operation id space freely.
func (s *Store) PutReceipt(job domain.JobID, operationID, digest string, result any) (int64, error) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return 0, err
	}
	var commit int64
	err = s.tx(func(tx *sql.Tx) error {
		c, err := nextCommit(tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`
INSERT INTO receipts (operation_id, job_id, digest, commit_number, result_json)
VALUES (?, ?, ?, ?, ?)`,
			operationID, string(job), digest, c, string(resultJSON)); err != nil {
			return err
		}
		commit = c
		return nil
	})
	return commit, err
}
