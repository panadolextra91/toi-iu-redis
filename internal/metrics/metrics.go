package metrics

import (
	"math"
	"sort"
	"sync"
)

// LatencyRecorder records request latencies and computes percentiles.
// Thread-safe via mutex — only locked during record and snapshot.
type LatencyRecorder struct {
	mu      sync.Mutex
	samples []float64 // latency in milliseconds
}

// NewLatencyRecorder creates a new latency recorder.
func NewLatencyRecorder() *LatencyRecorder {
	return &LatencyRecorder{
		samples: make([]float64, 0, 1024),
	}
}

// Record adds a latency sample in milliseconds.
func (lr *LatencyRecorder) Record(latencyMs float64) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.samples = append(lr.samples, latencyMs)
}

// Snapshot returns p50, p95, p99 latencies and total count.
// Returns zeros if no samples recorded.
func (lr *LatencyRecorder) Snapshot() LatencySnapshot {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	n := len(lr.samples)
	if n == 0 {
		return LatencySnapshot{}
	}

	// Sort a copy to compute percentiles
	sorted := make([]float64, n)
	copy(sorted, lr.samples)
	sort.Float64s(sorted)

	return LatencySnapshot{
		Count: n,
		P50:   percentile(sorted, 0.50),
		P95:   percentile(sorted, 0.95),
		P99:   percentile(sorted, 0.99),
	}
}

// Reset clears all recorded samples.
func (lr *LatencyRecorder) Reset() {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.samples = lr.samples[:0]
}

// LatencySnapshot holds computed percentile values.
type LatencySnapshot struct {
	Count int     `json:"count"`
	P50   float64 `json:"p50_ms"`
	P95   float64 `json:"p95_ms"`
	P99   float64 `json:"p99_ms"`
}

// percentile computes the p-th percentile from a sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := p * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}
	frac := rank - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
