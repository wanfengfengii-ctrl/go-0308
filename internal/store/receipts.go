package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"example.com/potable-water-pipeline/internal/domain"
)

// GetReceipt returns the idempotent receipt for an operation id, if present.
func (s *Store) GetReceipt(operationID string) (domain.OperationReceipt, bool, error) {
	var r domain.OperationReceipt
	var resultJSON string
	err := s.db.QueryRow(`
SELECT operation_id, digest, commit_number, result_json FROM receipts WHERE operation_id = ?`, operationID).
		Scan(&r.OperationID, &r.Digest, &r.CommitNumber, &resultJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.OperationReceipt{}, false, nil
	}
	if err != nil {
		return domain.OperationReceipt{}, false, err
	}
	return r, true, nil
}

// ReceiptResult returns the digest and stored result JSON for an operation id.
func (s *Store) ReceiptResult(operationID string) (digest, resultJSON string, found bool, err error) {
	err = s.db.QueryRow(`SELECT digest, result_json FROM receipts WHERE operation_id = ?`, operationID).
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
// operation id is the primary key; a second distinct write for the same id
// fails with a unique constraint violation, which the workflow maps to a
// stable conflict code.
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
