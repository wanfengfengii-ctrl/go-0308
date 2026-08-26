package lease

import (
	"fmt"
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
// leases and advances the id sequence past their highest numeric suffix.
func (m *Manager) Hydrate(leases []domain.ResourceLease) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leases = make(map[string]domain.ResourceLease, len(leases))
	m.seq = int64(len(leases))
	for _, l := range leases {
		m.leases[l.Resource] = l
	}
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
