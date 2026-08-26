package lease

import (
	"errors"
	"sync"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
)

func TestAcquireAndRelease(t *testing.T) {
	m := NewManager()
	lease, err := m.Acquire("outlet-1", "alice", 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Resource != "outlet-1" || lease.Holder != "alice" {
		t.Fatalf("lease = %+v", lease)
	}
	if err := m.Release("outlet-1", "alice", 15); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestAcquireBusy(t *testing.T) {
	m := NewManager()
	if _, err := m.Acquire("outlet-1", "alice", 10, 20); err != nil {
		t.Fatal(err)
	}
	_, err := m.Acquire("outlet-1", "bob", 10, 20)
	var cf *domain.ConflictError
	if !errors.As(err, &cf) || cf.Code != domain.ConflictLeaseBusy {
		t.Fatalf("busy acquire err = %v, want ConflictLeaseBusy", err)
	}
}

func TestAcquireExpiredWindow(t *testing.T) {
	m := NewManager()
	_, err := m.Acquire("outlet-1", "alice", 10, 10)
	var cf *domain.ConflictError
	if !errors.As(err, &cf) || cf.Code != domain.ConflictLeaseExpired {
		t.Fatalf("expired acquire err = %v, want ConflictLeaseExpired", err)
	}
}

func TestReleaseWrongHolder(t *testing.T) {
	m := NewManager()
	if _, err := m.Acquire("outlet-1", "alice", 10, 20); err != nil {
		t.Fatal(err)
	}
	err := m.Release("outlet-1", "mallory", 15)
	var cf *domain.ConflictError
	if !errors.As(err, &cf) || cf.Code != domain.ConflictLeaseBusy {
		t.Fatalf("wrong holder err = %v, want ConflictLeaseBusy", err)
	}
}

func TestReleaseExpired(t *testing.T) {
	m := NewManager()
	if _, err := m.Acquire("outlet-1", "alice", 10, 20); err != nil {
		t.Fatal(err)
	}
	err := m.Release("outlet-1", "alice", 25)
	var cf *domain.ConflictError
	if !errors.As(err, &cf) || cf.Code != domain.ConflictLeaseExpired {
		t.Fatalf("expired release err = %v, want ConflictLeaseExpired", err)
	}
}

func TestConcurrentAcquireSingleWinner(t *testing.T) {
	m := NewManager()
	const n = 32
	var wg sync.WaitGroup
	wins := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := m.Acquire("pump-1", string(rune('a'+i)), 1, 100); err == nil {
				wins <- struct{}{}
			}
		}(i)
	}
	wg.Wait()
	close(wins)
	count := 0
	for range wins {
		count++
	}
	if count != 1 {
		t.Fatalf("concurrent acquire winners = %d, want 1", count)
	}
}
