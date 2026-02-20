package service

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/huynhngocanhthu/toi-yeu-redis/internal/cache"
	"github.com/huynhngocanhthu/toi-yeu-redis/internal/db"
	"github.com/huynhngocanhthu/toi-yeu-redis/internal/metrics"
	"golang.org/x/sync/singleflight"
)

const (
	cacheTTLDefault = 30 * time.Second // CacheAside — long TTL
	cacheTTLHerd    = 3 * time.Second  // CacheHerd & CacheProtected — short TTL
	sfDBTimeout     = 5 * time.Second  // singleflight DB query timeout
)

// Service implements 4 cache modes: NoCache, CacheAside, CacheHerd, CacheProtected.
type Service struct {
	db      *db.FakeDB
	cache   *cache.RedisCache
	group   singleflight.Group
	baseCtx context.Context // server-level context — cancel on shutdown

	sfSharedCount int64 // atomic — singleflight coalesced requests
	cacheHits     int64 // atomic
	cacheMisses   int64 // atomic

	latencyRecorder *metrics.LatencyRecorder
}

// NewService creates a new service with all dependencies.
// baseCtx should be derived from signal.NotifyContext for graceful shutdown.
func NewService(fakeDB *db.FakeDB, redisCache *cache.RedisCache, baseCtx context.Context) *Service {
	return &Service{
		db:              fakeDB,
		cache:           redisCache,
		baseCtx:         baseCtx,
		latencyRecorder: metrics.NewLatencyRecorder(),
	}
}

// DB returns the underlying FakeDB for metrics access.
func (s *Service) DB() *db.FakeDB { return s.db }

// Cache returns the underlying RedisCache for direct operations (reset, etc).
func (s *Service) Cache() *cache.RedisCache { return s.cache }

// GetSharedCount returns the singleflight coalesced request count (atomic read).
func (s *Service) GetSharedCount() int64 { return atomic.LoadInt64(&s.sfSharedCount) }

// GetCacheHits returns total cache hits (atomic read).
func (s *Service) GetCacheHits() int64 { return atomic.LoadInt64(&s.cacheHits) }

// GetCacheMisses returns total cache misses (atomic read).
func (s *Service) GetCacheMisses() int64 { return atomic.LoadInt64(&s.cacheMisses) }

// LatencySnapshot returns the current latency percentiles.
func (s *Service) LatencySnapshot() metrics.LatencySnapshot { return s.latencyRecorder.Snapshot() }

// ResetStats resets all service-level counters.
func (s *Service) ResetStats() {
	atomic.StoreInt64(&s.sfSharedCount, 0)
	atomic.StoreInt64(&s.cacheHits, 0)
	atomic.StoreInt64(&s.cacheMisses, 0)
	s.latencyRecorder.Reset()
}

// recordLatency records the duration of an operation in milliseconds.
func (s *Service) recordLatency(start time.Time) {
	s.latencyRecorder.Record(float64(time.Since(start).Microseconds()) / 1000.0)
}

// ---------------------------------------------------------------------------
// Mode 1: NoCache — always query DB
// ---------------------------------------------------------------------------

// NoCache always queries the database, bypassing cache entirely.
// Expected: dbHits == total requests
func (s *Service) NoCache(ctx context.Context, key string) (string, error) {
	start := time.Now()
	defer func() { s.recordLatency(start) }()

	return s.db.Query(ctx, key)
}

// ---------------------------------------------------------------------------
// Mode 2: CacheAside — standard cache-aside with long TTL
// ---------------------------------------------------------------------------

// CacheAside implements standard cache-aside pattern with TTL=30s.
// Expected: dbHits == 1 (first request only, then all cache hits)
func (s *Service) CacheAside(ctx context.Context, key string) (string, error) {
	start := time.Now()
	defer func() { s.recordLatency(start) }()

	// Try cache first
	val, err := s.cache.Get(ctx, key)
	if err == nil {
		atomic.AddInt64(&s.cacheHits, 1)
		return val, nil
	}
	if !cache.IsNil(err) {
		log.Printf("⚠️  Redis error (CacheAside): %v — falling back to DB", err)
	}
	atomic.AddInt64(&s.cacheMisses, 1)

	// Cache miss — query DB
	result, err := s.db.Query(ctx, key)
	if err != nil {
		return "", err
	}

	// Populate cache — explicit TTL
	if setErr := s.cache.Set(ctx, key, result, cacheTTLDefault); setErr != nil {
		log.Printf("⚠️  Redis SET failed (CacheAside): %v", setErr)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Mode 3: CacheHerd — short TTL, NO singleflight (demonstrates thundering herd)
// ---------------------------------------------------------------------------

// CacheHerd uses a short TTL (3s) without singleflight protection.
// When TTL expires and many requests arrive simultaneously → thundering herd.
// Expected after storm: dbHits ≈ 200
func (s *Service) CacheHerd(ctx context.Context, key string) (string, error) {
	start := time.Now()
	defer func() { s.recordLatency(start) }()

	// Try cache first
	val, err := s.cache.Get(ctx, key)
	if err == nil {
		atomic.AddInt64(&s.cacheHits, 1)
		return val, nil
	}
	if !cache.IsNil(err) {
		log.Printf("⚠️  Redis error (CacheHerd): %v — falling back to DB", err)
	}
	atomic.AddInt64(&s.cacheMisses, 1)

	// Cache miss — query DB (NO singleflight → every request hits DB!)
	result, err := s.db.Query(ctx, key)
	if err != nil {
		return "", err
	}

	// Populate cache — SHORT TTL (3s)
	if setErr := s.cache.Set(ctx, key, result, cacheTTLHerd); setErr != nil {
		log.Printf("⚠️  Redis SET failed (CacheHerd): %v", setErr)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Mode 4: CacheProtected — singleflight wraps ONLY the DB slow path
// ---------------------------------------------------------------------------

// CacheProtected uses singleflight to coalesce concurrent DB queries.
// Even after TTL expires, only 1 request hits DB; rest wait for shared result.
// Expected after storm: dbHits == 1, sfShared ≈ 199
func (s *Service) CacheProtected(ctx context.Context, key string) (string, error) {
	start := time.Now()
	defer func() { s.recordLatency(start) }()

	// Try cache first — OUTSIDE singleflight
	val, err := s.cache.Get(ctx, key)
	if err == nil {
		atomic.AddInt64(&s.cacheHits, 1)
		return val, nil
	}
	if !cache.IsNil(err) {
		log.Printf("⚠️  Redis error (CacheProtected): %v — falling back to DB", err)
	}
	atomic.AddInt64(&s.cacheMisses, 1)

	// Singleflight — wraps ONLY the DB slow path
	v, err, shared := s.group.Do(key, func() (interface{}, error) {
		// DETACHED context from baseCtx — not from request ctx!
		// If the "leader" request cancels, DB query still completes for waiters.
		// baseCtx cancels on server shutdown → no goroutine leak.
		dbCtx, cancel := context.WithTimeout(s.baseCtx, sfDBTimeout)
		defer cancel()

		// Double-check cache — another goroutine may have just set it
		if cached, err := s.cache.Get(dbCtx, key); err == nil {
			return cached, nil
		}

		result, err := s.db.Query(dbCtx, key)
		if err != nil {
			return nil, err
		}

		// Populate cache — SHORT TTL (same as CacheHerd for comparison)
		if setErr := s.cache.Set(dbCtx, key, result, cacheTTLHerd); setErr != nil {
			log.Printf("⚠️  Redis SET failed (CacheProtected): %v", setErr)
		}

		return result, nil
	})

	if shared {
		atomic.AddInt64(&s.sfSharedCount, 1)
	}

	if err != nil {
		return "", err
	}
	return v.(string), nil
}
