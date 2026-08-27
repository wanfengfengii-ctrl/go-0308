package store

import (
	"database/sql"
	"errors"

	"example.com/potable-water-pipeline/internal/domain"
)

// SaveLease persists a lease, replacing any prior row for the same resource.
// The unique index on resource enforces a single outstanding lease per
// resource across restarts; the upsert lets a freshly acquired lease overwrite
// a recovered-but-expired row without tripping the index.
func (s *Store) SaveLease(l domain.ResourceLease) error {
	_, err := s.db.Exec(`
INSERT INTO leases (id, resource, holder, clock, expires) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(resource) DO UPDATE SET
  id = excluded.id,
  holder = excluded.holder,
  clock = excluded.clock,
  expires = excluded.expires`,
		string(l.ID), l.Resource, l.Holder, l.Clock, l.Expires)
	return err
}

// DeleteLease removes a lease by id.
func (s *Store) DeleteLease(id domain.LeaseID) error {
	_, err := s.db.Exec(`DELETE FROM leases WHERE id = ?`, string(id))
	return err
}

// GetLease returns a lease by id.
func (s *Store) GetLease(id domain.LeaseID) (domain.ResourceLease, bool, error) {
	var l domain.ResourceLease
	err := s.db.QueryRow(`SELECT id, resource, holder, clock, expires FROM leases WHERE id = ?`, string(id)).
		Scan(&l.ID, &l.Resource, &l.Holder, &l.Clock, &l.Expires)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ResourceLease{}, false, nil
	}
	if err != nil {
		return domain.ResourceLease{}, false, err
	}
	return l, true, nil
}

// LeaseForResource returns the outstanding lease for a resource, if any.
func (s *Store) LeaseForResource(resource string) (domain.ResourceLease, bool, error) {
	var l domain.ResourceLease
	err := s.db.QueryRow(`SELECT id, resource, holder, clock, expires FROM leases WHERE resource = ?`, resource).
		Scan(&l.ID, &l.Resource, &l.Holder, &l.Clock, &l.Expires)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ResourceLease{}, false, nil
	}
	if err != nil {
		return domain.ResourceLease{}, false, err
	}
	return l, true, nil
}

// ListLeases returns all persisted leases for restart recovery.
func (s *Store) ListLeases() ([]domain.ResourceLease, error) {
	rows, err := s.db.Query(`SELECT id, resource, holder, clock, expires FROM leases`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ResourceLease
	for rows.Next() {
		var l domain.ResourceLease
		if err := rows.Scan(&l.ID, &l.Resource, &l.Holder, &l.Clock, &l.Expires); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
