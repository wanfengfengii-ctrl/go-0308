package lease

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"example.com/potable-water-pipeline/internal/domain"
)

// Manager coordinates time-bounded exclusive holds on resource keys (flush
// outlets, injection pumps, instruments, and sampling positions). A resource
// is mutually exclusive within a logical-time window: a concurrent or expired
// holder cannot submit evidence.
type Manager struct {
	mu     sync.Mutex
	seq    int64
	leases map[string]domain.ResourceLease
}

// NewManager returns an empty lease manager.
func NewManager() *Manager {
	return &Manager{leases: make(map[string]domain.ResourceLease)}
}

// Hydrate restores outstanding leases from durable storage (for example, after
// a service restart). It replaces the in-memory lease table with the supplied
// leases and advances the id sequence past the highest numeric suffix among
// them. Using the max suffix — not the outstanding count — keeps newly minted
// lease ids strictly greater than every previously issued id, so an acquire
// after restart never collides with a still-persisted lease that an earlier
// (now released) acquire happened to sit beside. Only outstanding leases are
// recovered (released leases were deleted from durable storage on release), so
// the seq must be derived from their ids rather than their count.
func (m *Manager) Hydrate(leases []domain.ResourceLease) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leases = make(map[string]domain.ResourceLease, len(leases))
	m.seq = 0
	for _, l := range leases {
		m.leases[l.Resource] = l
		if n, ok := parseLeaseSeq(string(l.ID)); ok && n > m.seq {
			m.seq = n
		}
	}
}

// parseLeaseSeq extracts the trailing integer from a "lease-N" id. It reports
// ok=false for any id that does not match the canonical shape, leaving the
// caller to treat such rows as not advancing the sequence.
func parseLeaseSeq(id string) (int64, bool) {
	const prefix = "lease-"
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(id, prefix), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// Acquire attempts to hold resource exclusively for holder until expires. It
// fails when the expiry is not in the future or when an unexpired lease is
// already held by a different holder.
func (m *Manager) Acquire(resource, holder string, now, expires int64) (domain.ResourceLease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if expires <= now {
		return domain.ResourceLease{}, &domain.ConflictError{
			Code:   domain.ConflictLeaseExpired,
			Reason: resource,
		}
	}
	if cur, ok := m.leases[resource]; ok && cur.Expires > now {
		return domain.ResourceLease{}, &domain.ConflictError{
			Code:   domain.ConflictLeaseBusy,
			Reason: resource,
		}
	}
	m.seq++
	l := domain.ResourceLease{
		ID:       domain.LeaseID(fmt.Sprintf("lease-%d", m.seq)),
		Resource: resource,
		Holder:   holder,
		Clock:    now,
		Expires:  expires,
	}
	m.leases[resource] = l
	return l, nil
}

// Release returns the resource to the pool. Only the current, unexpired
// holder may release; expired or mismatched holders are rejected.
func (m *Manager) Release(resource, holder string, now int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.leases[resource]
	if !ok || cur.Holder != holder {
		return &domain.ConflictError{Code: domain.ConflictLeaseBusy, Reason: resource}
	}
	if cur.Expires <= now {
		delete(m.leases, resource)
		return &domain.ConflictError{Code: domain.ConflictLeaseExpired, Reason: resource}
	}
	delete(m.leases, resource)
	return nil
}
