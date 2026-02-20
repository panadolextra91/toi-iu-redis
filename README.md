# 🔥 Redis Under Fire

> **"Caching improves performance. But uncontrolled caching creates new bottlenecks. Intelligent request coalescing stabilizes the system."**

Dự án demo cho môn **Net Centric Programming** — chứng minh bằng số liệu thật rằng:

1. Database **là bottleneck** khi không cache
2. Cache-Aside **tăng throughput** đáng kể
3. Thundering Herd **là vấn đề thật** khi TTL hết hạn
4. `singleflight` **giải quyết stampede** chỉ với vài dòng code

Tech stack: **Go** · **Redis** · `net/http` · `go-redis/v9` · `x/sync/singleflight`

---

## 📊 Benchmark Results (200 concurrent requests)

```
              ┌─────────────────────────────────────────────────┐
  DB Hits     │                                                 │
              │  200 ████████████████████████████████████ HERD   │
              │    1 █ PROTECTED                                │
              │                                                 │
  DB Blocked  │  190 ██████████████████████████████████ HERD     │
              │    0  PROTECTED                                 │
              │                                                 │
  P99 Latency │  4018ms ████████████████████████████████ HERD   │
              │   201ms ███ PROTECTED                           │
              │                                                 │
  SF Shared   │    0  HERD                                      │
              │  200 ████████████████████████████████████ PROT.  │
              └─────────────────────────────────────────────────┘
```

| Metric | 🌪 cache-herd | 🛡 cache-protected | Improvement |
|---|:---:|:---:|:---:|
| **DB Hits** | 200 | **1** | **200× fewer** |
| **DB Blocked** | 190 | **0** | Pool never exhausted |
| **P99 Latency** | 4,018 ms | **201 ms** | **20× faster** |
| **Singleflight Shared** | 0 | **200** | All requests coalesced |

---

## 🏗 Architecture

```
               200 Concurrent Requests
                       │
                       ▼
              ┌─────────────────┐
              │   HTTP Server   │  :8080
              │   (net/http)    │
              └────────┬────────┘
                       │
              ┌────────▼────────┐
              │  Service Layer  │  4 modes
              │                 │
              │  ┌────────────┐ │
              │  │ NoCache    │─┼──────────────────────┐
              │  │ CacheAside │─┼──┐                   │
              │  │ CacheHerd  │─┼──┤                   │
              │  │ CacheProt. │─┼──┤ + singleflight    │
              │  └────────────┘ │  │                   │
              └─────────────────┘  │                   │
                                   ▼                   ▼
                          ┌──────────────┐    ┌──────────────┐
                          │    Redis     │    │   Fake DB    │
                          │  (go-redis)  │    │  pool=10     │
                          │  TTL: 3-30s  │    │  latency=200ms│
                          └──────────────┘    └──────────────┘
```

### Timeout Hierarchy

```
Redis ops: 2s  <  Singleflight DB: 5s  <  HTTP handler: 10s
```

---

## 🧩 4 Cache Modes

### 1. `/nocache` — No Cache (Baseline)

```
Request → DB.Query() → Response
```

Mọi request đều hit DB. Với pool=10 và latency=200ms, 100 concurrent requests gây **queuing nặng** (~1.5–2s avg latency). Đây là baseline để so sánh.

### 2. `/cache` — Cache-Aside (TTL = 30s)

```
Request → Redis GET
            ├─ HIT  → return cached
            └─ MISS → DB.Query() → Redis SET (30s) → return
```

Chỉ request đầu tiên hit DB. Tất cả request sau đó hit cache → **latency < 5ms**.

### 3. `/cache-herd` — Thundering Herd 🌪

```
Request → Redis GET
            ├─ HIT  → return cached
            └─ MISS → DB.Query() → Redis SET (3s!) → return
                       ↑ NO singleflight protection!
```

TTL = 3s. Khi hết hạn, 200 request ập vào cùng lúc → **tất cả đều miss** → **tất cả đều query DB** → pool exhaustion, latency spike ~4s. Đây là **thundering herd**.

### 4. `/cache-protected` — Singleflight Fix 🛡

```
Request → Redis GET
            ├─ HIT  → return cached
            └─ MISS → singleflight.Do(key)
                         ├─ LEADER  → DB.Query() → Redis SET (3s) → return
                         └─ WAITERS → wait for leader's result → return
```

Cùng TTL = 3s, nhưng `singleflight` gom 200 request thành **1 DB query duy nhất**. 199 request còn lại "ké" kết quả → **DB hits = 1, latency ổn định ~200ms**.

---

## 🚀 Quick Start

### Prerequisites

- **Go** 1.22+
- **Redis** chạy trên `localhost:6379`

```bash
# Redis bằng Docker (nếu chưa có)
docker run -d --name redis-demo -p 6379:6379 redis:7
```

### Run

```bash
git clone <repo>
cd toi-yeu-redis
go run ./cmd/server
```

Output:

```
═══════════════════════════════════════════
🔥 Redis Under Fire — Server starting
📡 Listening on :8080
📊 DB Pool: 10 | Latency: 200ms
🗄  Redis: localhost:6379 | Default TTL: 30s
🧠 Herd TTL: 3s | Singleflight DB Timeout: 5s
═══════════════════════════════════════════
```

### Race Detector (recommended)

```bash
go run -race ./cmd/server
```

---

## 🧪 Demo Script

### ⚡ One-Click Storm Tests

```bash
# 🌪 Thundering Herd — expect db_hits ≈ 200
curl -s "localhost:8080/storm?mode=cache-herd" | jq

# 🛡 Singleflight Fix — expect db_hits = 1
curl -s "localhost:8080/storm?mode=cache-protected" | jq
```

`/storm` tự động: reset metrics → xoá cache → spawn 200 goroutines → trả kết quả. **Deterministic, reproducible 100%.**

### 📈 Full Benchmark (dùng `hey`)

```bash
# Cài hey
go install github.com/rakyll/hey@latest

# 1. NoCache — baseline (expect db_hits = 5000, latency ~1.5-2s)
curl -sX POST localhost:8080/reset
hey -n 5000 -c 100 http://localhost:8080/nocache
curl -s localhost:8080/stats | jq

# 2. CacheAside — expect db_hits = 1, latency < 5ms
curl -sX POST localhost:8080/reset
hey -n 5000 -c 100 http://localhost:8080/cache
curl -s localhost:8080/stats | jq

# 3. CacheHerd — warm → expire → stampede
curl -sX POST localhost:8080/reset
curl -s localhost:8080/cache-herd > /dev/null  # warm cache
sleep 4                                         # wait TTL (3s) to expire
hey -n 200 -c 200 http://localhost:8080/cache-herd
curl -s localhost:8080/stats | jq               # expect db_hits ≈ 200

# 4. CacheProtected — singleflight saves the day
curl -sX POST localhost:8080/reset
curl -s localhost:8080/cache-protected > /dev/null
sleep 4
hey -n 200 -c 200 http://localhost:8080/cache-protected
curl -s localhost:8080/stats | jq               # expect db_hits = 1
```

### Expected Results Summary

| Endpoint | Requests | Concurrency | DB Hits | Blocked | Avg Latency | SF Shared |
|---|:---:|:---:|:---:|:---:|:---:|:---:|
| `/nocache` | 5000 | 100 | 5,000 | ~4,900+ | ~1.5–2s | 0 |
| `/cache` | 5000 | 100 | 1 | 0 | < 5ms | 0 |
| `/cache-herd` | 200 | 200 | ~200 | ~190+ | ~200ms+ | 0 |
| `/cache-protected` | 200 | 200 | 1 | 0 | ~200ms* | ~199 |

> \* CacheProtected avg ≈ 200ms vì 199 requests chờ singleflight completion. Chúng **không hit DB** nhưng vẫn phải đợi kết quả từ leader request.

---

## 📡 API Reference

### Cache Mode Endpoints

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/nocache` | Luôn query DB, bypass cache |
| `GET` | `/cache` | Cache-aside, TTL = 30s |
| `GET` | `/cache-herd` | Cache-aside, TTL = 3s, **no** singleflight |
| `GET` | `/cache-protected` | Cache-aside, TTL = 3s, **with** singleflight |

### Observability

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/stats` | JSON metrics dashboard |
| `POST` | `/reset` | Reset tất cả counters + xoá cache key |
| `GET` | `/storm?mode=X` | Spawn 200 goroutines, trả kết quả tổng hợp |

### `/stats` response

```json
{
  "db_hits": 200,
  "db_blocked_requests": 190,
  "active_db_connections": 0,
  "db_pool_size": 10,
  "cache_hits": 0,
  "cache_misses": 200,
  "singleflight_shared": 0,
  "inflight_requests": 1,
  "goroutines": 6,
  "uptime_seconds": 42.5,
  "latency": {
    "samples": 200,
    "p50_ms": "2106.82",
    "p95_ms": "3829.10",
    "p99_ms": "4018.54"
  }
}
```

---

## 🧱 Project Structure

```
toi-yeu-redis/
├── cmd/server/
│   └── main.go              ← Entrypoint, wiring, graceful shutdown
├── internal/
│   ├── db/
│   │   └── fakedb.go        ← Semaphore pool, atomic counters, context-aware
│   ├── cache/
│   │   └── redis.go         ← Redis wrapper, timeout hierarchy, IsNil helper
│   ├── service/
│   │   └── service.go       ← 4 modes, singleflight, cache hit/miss tracking
│   ├── handler/
│   │   └── handler.go       ← HTTP endpoints, inflight middleware, storm
│   └── metrics/
│       └── metrics.go       ← Latency recorder (p50/p95/p99)
├── go.mod
└── go.sum
```

---

## 🧠 Implementation Highlights

### Context-Aware Database Pool

```go
// Semaphore acquire — NEVER blocks forever
select {
case db.semaphore <- struct{}{}:
    defer func() { <-db.semaphore }()
case <-ctx.Done():
    return "", ctx.Err()  // respect timeout
}
```

### Singleflight with Detached Context

```go
// Inside singleflight: use server baseCtx, NOT request ctx
// If leader request cancels → DB query still completes for waiters
dbCtx, cancel := context.WithTimeout(s.baseCtx, 5*time.Second)
defer cancel()

// Double-check cache before DB query
if cached, err := s.cache.Get(dbCtx, key); err == nil {
    return cached, nil
}

result, _ := s.db.Query(dbCtx, key)
s.cache.Set(dbCtx, key, result, 3*time.Second)
```

### Deterministic Storm (No Network Dependency)

```go
// Direct service calls — not HTTP roundtrip
// No localhost/port/IPv6 dependency
fn := validModes[mode]  // e.g. svc.CacheHerd
for i := 0; i < 200; i++ {
    go fn(ctx, "benchmark-key")
}
```

---

## 🎯 Presentation Flow

```
Step 1: /nocache          → "DB là bottleneck" (5000 hits, ~2s latency)
     │
Step 2: /cache            → "Cache giảm 5000× DB hits" (1 hit, <5ms)
     │
Step 3: /cache-herd       → "Nhưng herd phá cache" (200 hits đồng thời!)
     │
Step 4: /cache-protected  → "Singleflight fix herd" (1 hit, 199 shared)
     │
     ▼
  Kết luận: Caching tốt, nhưng cần bảo vệ.
            Singleflight = request coalescing = ổn định hệ thống.
```

---

## 📋 Concurrency Safety Checklist

| Component | Mechanism | Race-free? |
|---|---|:---:|
| `dbHits` | `sync/atomic` | ✅ |
| `blockedCount` | `sync/atomic` | ✅ |
| `cacheHits/Misses` | `sync/atomic` | ✅ |
| `sfSharedCount` | `sync/atomic` | ✅ |
| `inflightRequests` | `sync/atomic` | ✅ |
| Latency samples | `sync.Mutex` | ✅ |
| DB pool | `chan struct{}` (semaphore) | ✅ |
| `uptime` | `time.Since(immutable)` | ✅ |

```bash
# Verify: zero race warnings
go run -race ./cmd/server
```

---

*Built with 🫀 by a toi-yeu-redis enthusiast for **Net Centric Programming**.*
