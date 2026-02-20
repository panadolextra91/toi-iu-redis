package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/huynhngocanhthu/toi-yeu-redis/internal/service"
)

// Handler holds HTTP handlers for all endpoints.
type Handler struct {
	svc              *service.Service
	startTime        time.Time
	inflightRequests int64 // atomic — current in-flight request count
}

// NewHandler creates a new handler with service dependency and start time for uptime.
func NewHandler(svc *service.Service, startTime time.Time) *Handler {
	return &Handler{
		svc:       svc,
		startTime: startTime,
	}
}

// WithInflight wraps a handler with inflight request counting middleware.
func (h *Handler) WithInflight(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&h.inflightRequests, 1)
		defer atomic.AddInt64(&h.inflightRequests, -1)
		next(w, r)
	}
}

// RegisterRoutes registers all HTTP routes to the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/nocache", h.WithInflight(h.handleNoCache))
	mux.HandleFunc("/cache", h.WithInflight(h.handleCache))
	mux.HandleFunc("/cache-herd", h.WithInflight(h.handleCacheHerd))
	mux.HandleFunc("/cache-protected", h.WithInflight(h.handleCacheProtected))
	mux.HandleFunc("/stats", h.WithInflight(h.handleStats))
	mux.HandleFunc("/reset", h.handleReset)
	mux.HandleFunc("/storm", h.WithInflight(h.handleStorm))
}

// ---------------------------------------------------------------------------
// Mode Handlers
// ---------------------------------------------------------------------------

func (h *Handler) handleNoCache(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.NoCache(r.Context(), "benchmark-key")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

func (h *Handler) handleCache(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.CacheAside(r.Context(), "benchmark-key")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

func (h *Handler) handleCacheHerd(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.CacheHerd(r.Context(), "benchmark-key")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

func (h *Handler) handleCacheProtected(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.CacheProtected(r.Context(), "benchmark-key")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	latency := h.svc.LatencySnapshot()

	stats := map[string]interface{}{
		"db_hits":               h.svc.DB().GetHits(),
		"db_blocked_requests":   h.svc.DB().GetBlockedCount(),
		"active_db_connections": h.svc.DB().ActiveConnections(),
		"db_pool_size":          h.svc.DB().PoolSize(),
		"cache_hits":            h.svc.GetCacheHits(),
		"cache_misses":          h.svc.GetCacheMisses(),
		"singleflight_shared":   h.svc.GetSharedCount(),
		"inflight_requests":     atomic.LoadInt64(&h.inflightRequests),
		"goroutines":            runtime.NumGoroutine(),
		"uptime_seconds":        time.Since(h.startTime).Seconds(),
		"latency": map[string]interface{}{
			"samples": latency.Count,
			"p50_ms":  fmt.Sprintf("%.2f", latency.P50),
			"p95_ms":  fmt.Sprintf("%.2f", latency.P95),
			"p99_ms":  fmt.Sprintf("%.2f", latency.P99),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

func (h *Handler) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}

	h.svc.DB().ResetHits()
	h.svc.ResetStats()
	if err := h.svc.Cache().Delete(r.Context(), "benchmark-key"); err != nil {
		// Not fatal — cache might already be empty
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"reset_ok"}`)
}

// ---------------------------------------------------------------------------
// Storm — deterministic herd simulation via direct service calls
// ---------------------------------------------------------------------------

func (h *Handler) handleStorm(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")

	// Whitelist validation — map mode to service method
	type modeFn func(context.Context, string) (string, error)
	validModes := map[string]modeFn{
		"nocache":         h.svc.NoCache,
		"cache":           h.svc.CacheAside,
		"cache-herd":      h.svc.CacheHerd,
		"cache-protected": h.svc.CacheProtected,
	}
	fn, ok := validModes[mode]
	if !ok {
		http.Error(w, `{"error":"invalid mode","valid":["nocache","cache","cache-herd","cache-protected"]}`,
			http.StatusBadRequest)
		return
	}

	// Deterministic reset — clean state before storm
	h.svc.DB().ResetHits()
	h.svc.ResetStats()
	h.svc.Cache().Delete(r.Context(), "benchmark-key") // force cache miss

	// Spawn 200 goroutines — direct service calls, not HTTP roundtrip
	const stormSize = 200
	var wg sync.WaitGroup

	for i := 0; i < stormSize; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Use baseCtx (server-level), NOT r.Context()
			// If the HTTP request cancels mid-storm, goroutines still complete
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			fn(ctx, "benchmark-key")
		}()
	}
	wg.Wait()

	// Collect results
	result := map[string]interface{}{
		"mode":                mode,
		"storm_size":          stormSize,
		"db_hits":             h.svc.DB().GetHits(),
		"db_blocked":          h.svc.DB().GetBlockedCount(),
		"cache_hits":          h.svc.GetCacheHits(),
		"cache_misses":        h.svc.GetCacheMisses(),
		"singleflight_shared": h.svc.GetSharedCount(),
		"latency":             h.svc.LatencySnapshot(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// writeJSON writes a raw JSON string response.
func writeJSON(w http.ResponseWriter, data string) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, data)
}
