package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// Store is the relational persistence layer backed by SQLite. It owns a single
// connection so that writes are serialized, giving a single-writer guarantee
// for the terminal verdict and resource leases. All state survives restart.
type Store struct {
	db *sql.DB
	mu sync.Mutex // guards non-transactional read-modify-write sequences
}

// Open opens (creating if necessary) the SQLite database at path and applies
// the schema. A path of ":memory:" yields an ephemeral database for tests.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		// WAL improves durability and concurrent read behavior; busy timeout
		// avoids spurious locks on a single process.
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Single connection serializes writes; sufficient for the single-writer
	// terminal barrier and lease mutual exclusion.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the raw database for advanced operations. Prefer the typed
// methods; this is used by the smoke probe and tests.
func (s *Store) DB() *sql.DB { return s.db }

// tx runs fn inside a transaction, committing on success and rolling back on
// error so no partial state is ever left behind.
func (s *Store) tx(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// nextCommit atomically advances the monotonic commit counter and returns the
// new value. It must be called inside a transaction.
func nextCommit(tx *sql.Tx) (int64, error) {
	var v int64
	if err := tx.QueryRow(`UPDATE commit_counter SET value = value + 1 WHERE id = 1 RETURNING value`).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// migrate applies the schema idempotently and records the schema version.
func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS commit_counter (id INTEGER PRIMARY KEY CHECK (id = 1), value INTEGER NOT NULL);
INSERT OR IGNORE INTO commit_counter (id, value) VALUES (1, 0);

CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  topology_digest TEXT NOT NULL,
  topology_json TEXT NOT NULL,
  targets_json TEXT NOT NULL DEFAULT '{}',
  rule_digest TEXT NOT NULL,
  rule_version INTEGER NOT NULL,
  clock INTEGER NOT NULL,
  stage TEXT NOT NULL,
  round INTEGER NOT NULL,
  flush_window INTEGER NOT NULL DEFAULT 0,
  contact_ct INTEGER NOT NULL DEFAULT 0,
  contact_started INTEGER NOT NULL DEFAULT 0,
  contact_start_clock INTEGER NOT NULL DEFAULT 0,
  contact_last_clock INTEGER NOT NULL DEFAULT 0,
  contact_initial INTEGER NOT NULL DEFAULT 0,
  contact_last_conc INTEGER NOT NULL DEFAULT 0,
  turnover_cum INTEGER NOT NULL DEFAULT 0,
  terminal_credential TEXT NOT NULL DEFAULT '',
  terminal_commit INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL,
  stage TEXT NOT NULL,
  clock INTEGER NOT NULL,
  operation_id TEXT NOT NULL,
  digest TEXT NOT NULL,
  round INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_job ON events(job_id, id);

CREATE TABLE IF NOT EXISTS measurements (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL,
  instrument_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  clock INTEGER NOT NULL,
  value INTEGER NOT NULL,
  scale INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_measurements_job ON measurements(job_id, id);

CREATE TABLE IF NOT EXISTS doses (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL,
  injection_id TEXT NOT NULL,
  clock INTEGER NOT NULL,
  value INTEGER NOT NULL,
  scale INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS leases (
  id TEXT PRIMARY KEY,
  resource TEXT NOT NULL,
  holder TEXT NOT NULL,
  clock INTEGER NOT NULL,
  expires INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_leases_resource ON leases(resource);

CREATE TABLE IF NOT EXISTS samples (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL,
  point_id TEXT NOT NULL,
  round INTEGER NOT NULL,
  label TEXT NOT NULL UNIQUE,
  digest TEXT NOT NULL,
  collected_by TEXT NOT NULL,
  sealed_by TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_samples_point_round ON samples(job_id, point_id, round);

CREATE TABLE IF NOT EXISTS custody (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  sample_id TEXT NOT NULL,
  from_party TEXT NOT NULL,
  to_party TEXT NOT NULL,
  clock INTEGER NOT NULL,
  action TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS lab_attempts (
  id TEXT PRIMARY KEY,
  sample_id TEXT NOT NULL,
  retry_number INTEGER NOT NULL,
  status TEXT NOT NULL,
  calibration TEXT NOT NULL,
  digest TEXT NOT NULL,
  test_item TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS lab_results (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  attempt_id TEXT NOT NULL,
  sample_id TEXT NOT NULL,
  test_item TEXT NOT NULL,
  value INTEGER NOT NULL,
  scale INTEGER NOT NULL,
  passed INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS incidents (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  clock INTEGER NOT NULL,
  closed INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS retest_sets (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL,
  incident_id TEXT NOT NULL DEFAULT '',
  round INTEGER NOT NULL,
  members_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS treatment_rounds (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL,
  retest_id TEXT NOT NULL,
  round INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS reviews (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL,
  person_id TEXT NOT NULL,
  approved INTEGER NOT NULL,
  digest TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_reviews_person ON reviews(job_id, person_id);

CREATE TABLE IF NOT EXISTS receipts (
  operation_id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL,
  digest TEXT NOT NULL,
  commit_number INTEGER NOT NULL,
  result_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL);
INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (1, strftime('%s','now'));
`
	_, err := s.db.Exec(schema)
	return err
}

// encodeJSON and decodeJSON centralize JSON column handling.
func encodeJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeJSON(data string, v any) error {
	if data == "" {
		return fmt.Errorf("empty json")
	}
	return json.Unmarshal([]byte(data), v)
}
