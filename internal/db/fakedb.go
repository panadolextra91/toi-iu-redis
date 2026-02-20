package db

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// FakeDB simulates a database with limited connection pool and artificial latency.
// It demonstrates bottleneck behavior when many concurrent requests hit the DB.
type FakeDB struct {
	semaphore    chan struct{} // buffered channel = connection pool
	dbHits       int64        // atomic — total queries executed
	blockedCount int64        // atomic — requests that had to wait for pool slot
	latency      time.Duration
}

// NewFakeDB creates a fake database with a limited connection pool.
// poolSize controls max concurrent "connections", latency simulates query time.
func NewFakeDB(poolSize int, latency time.Duration) *FakeDB {
	return &FakeDB{
		semaphore: make(chan struct{}, poolSize),
		latency:   latency,
	}
}

// Query simulates a database query with connection pool limiting.
// Context-aware at every stage: acquire, sleep, and return.
func (db *FakeDB) Query(ctx context.Context, key string) (string, error) {
	// Track pool exhaustion BEFORE acquire attempt
	if len(db.semaphore) == cap(db.semaphore) {
		atomic.AddInt64(&db.blockedCount, 1)
	}

	// Acquire semaphore — respect context, NEVER block forever
	select {
	case db.semaphore <- struct{}{}:
		defer func() { <-db.semaphore }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Count the hit — this request has acquired a DB "connection"
	atomic.AddInt64(&db.dbHits, 1)

	// Simulate query latency — respect cancel mid-query
	select {
	case <-time.After(db.latency):
	case <-ctx.Done():
		return "", ctx.Err()
	}

	result := fmt.Sprintf(`{"key":"%s","source":"database","ts":"%s"}`,
		key, time.Now().Format(time.RFC3339Nano))
	return result, nil
}

// GetHits returns the total number of DB queries executed (atomic read).
func (db *FakeDB) GetHits() int64 {
	return atomic.LoadInt64(&db.dbHits)
}

// GetBlockedCount returns how many requests had to wait for a pool slot (atomic read).
func (db *FakeDB) GetBlockedCount() int64 {
	return atomic.LoadInt64(&db.blockedCount)
}

// ActiveConnections returns the number of currently active DB "connections".
func (db *FakeDB) ActiveConnections() int {
	return len(db.semaphore)
}

// PoolSize returns the max connection pool size.
func (db *FakeDB) PoolSize() int {
	return cap(db.semaphore)
}

// ResetHits resets both dbHits and blockedCount to zero (atomic writes).
func (db *FakeDB) ResetHits() {
	atomic.StoreInt64(&db.dbHits, 0)
	atomic.StoreInt64(&db.blockedCount, 0)
}

// Latency returns the configured query latency.
func (db *FakeDB) Latency() time.Duration {
	return db.latency
}
