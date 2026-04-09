# 🎯 Defense Notes (Explain Code)

## 1. Thundering Herd xảy ra ở đâu

Mở `service.go`, function:

```go
func (s *Service) CacheHerd(...)
```

Chỉ vào:

```go
val, err := s.cache.Get(ctx, key)
...
result, err := s.db.Query(ctx, key)
```

Nói:

> “Here, every request checks the cache. If the cache misses, all requests directly call the database.”

Chỉ tiếp:

```go
s.cache.Set(ctx, key, result, cacheTTLHerd)
```

> “There is no coordination between requests, so when TTL expires, all requests miss at the same time and hit the database together. This is where thundering herd happens.”

---

## 2. Fix herd ở đâu

Mở `service.go`, chỉ:

```go
v, err, shared := s.group.Do(key, func() (interface{}, error) {
```

Nói:

> “This line is the fix. Singleflight ensures only one execution per key.”

Chỉ tiếp:

```go
result, err := s.db.Query(dbCtx, key)
```

> “Only one goroutine hits the database.”

Chỉ tiếp:

```go
if shared {
    atomic.AddInt64(&s.sfSharedCount, 1)
}
```

> “Other requests reuse the result instead of querying the database, so we reduce 200 database hits to just 1.”

---

## 3. Tại sao check cache ngoài singleflight

```go
val, err := s.cache.Get(ctx, key)
if err == nil {
    return val, nil
}
```

> “This is the fast path. Cache hits should not be blocked by singleflight. We only coalesce the slow path — the database.”

---

## 4. Tại sao dùng baseCtx

```go
dbCtx, cancel := context.WithTimeout(s.baseCtx, sfDBTimeout)
```

> “This context is detached from the request. If we use request context, the leader request might cancel the DB query. Using baseCtx ensures the query completes for all waiting requests.”

---

## 5. Tại sao double-check cache

```go
if cached, err := s.cache.Get(dbCtx, key); err == nil {
    return cached, nil
}
```

> “Another goroutine might have already populated the cache, so we check again to avoid unnecessary DB queries.”

---

😤🔥 ok giờ là **level “bị hỏi xoáy → trả lời như senior”** rồi
→ t expand hết phần **BONUS Q&A**, nhưng vẫn giữ dạng **nói được trên lớp (không lan man)**

---

# 💣 BONUS Q&A — EXPANDED (DETAILED BUT SPEAKABLE)

## ❓ Why not just use mutex?

> “Mutex only provides mutual exclusion — it ensures only one goroutine executes a critical section at a time.”

> “But it does not share the result.”

> “So if multiple requests are waiting on a mutex, once it is released, each of them will still execute the same database query again.”

> “Singleflight is different — it not only ensures one execution, but also shares the result with all waiting requests.”

> “So instead of serializing duplicate work, it eliminates duplicate work entirely.”

💀 chốt:

> “So mutex controls access, but singleflight controls duplication.”

---

## ❓ What happens if the DB is slow?

> “If the database is slow, the leader request will take longer to complete.”

> “All other requests will wait for that leader to finish, because they depend on the shared result.”

> “So latency becomes consistent but not necessarily fast.”

> “This is a trade-off — we sacrifice parallelism to avoid overwhelming the database.”

💀 chốt:

> “So singleflight improves stability, but it does not reduce the latency of the slow operation itself.”

---

## ❓ Where do you simulate concurrency?

👉 mở `handler.go`

```go
for i := 0; i < stormSize; i++ {
    go func() {
        fn(ctx, service.CacheKey)
    }()
}
```

> “Here I spawn 200 goroutines to simulate concurrent requests.”

> “Each goroutine directly calls the service layer instead of going through HTTP.”

> “This removes network overhead and makes the experiment deterministic.”

> “So we are measuring system behavior, not HTTP performance.”

💀 chốt:

> “This isolates concurrency and caching effects.”

---

## ❓ How do you ensure no race condition?

👉 chỉ atomic

```go
atomic.AddInt64(...)
```

> “All shared counters are updated using atomic operations.”

> “This ensures thread-safe increments without using locks.”

👉 nếu bị hỏi sâu hơn:

> “For latency recording, I use a mutex to protect the slice.”

> “So both read and write paths are synchronized.”

💀 chốt:

> “So correctness is ensured either by atomic operations or mutex, depending on the data structure.”

---

## ❓ What happens if Redis fails?

👉 câu này rất dễ dính

> “If Redis fails, the system falls back to the database.”

> “In the code, if Redis returns an error that is not a cache miss, we log the error and continue with a DB query.”

> “So the system remains functional, but performance degrades.”

💀 chốt:

> “Cache is an optimization, not a dependency.”

---

## ❓ Why not cache everything?

> “Caching everything is not practical because memory is limited.”

> “Also, large objects increase serialization and deserialization cost.”

> “So we should only cache frequently accessed data.”

💀 chốt:

> “Cache should be selective, not exhaustive.”

---

## ❓ Why not increase DB pool instead of using cache?

> “Increasing the DB pool only scales the database linearly.”

> “But under high concurrency, the database still becomes a bottleneck.”

> “Cache reduces the number of database requests, not just distributes them.”

💀 chốt:

> “So cache reduces load, while scaling the pool only delays saturation.”

---

## ❓ What is the limitation of your current approach?

> “Singleflight works per process, so it does not coordinate across multiple instances.”

> “In a distributed system, we would need a distributed lock or another coordination mechanism.”

💀 câu này = cực kỳ ăn điểm

---

## ❓ Why not use distributed locking?

> “Distributed locking can also prevent multiple requests from hitting the database.”

> “But it introduces additional complexity, like lock management and failure handling.”

> “Singleflight is simpler and works well within a single service instance.”

💀 chốt:

> “So it’s a trade-off between simplicity and scalability.”

---

# 🎯 FINAL MINDSET

👉 tất cả câu trả lời của m nên follow pattern:

```text
what it does  
→ why it exists  
→ trade-off
```

---

# 💀 3 CÂU CHỐT LUÔN CỨU MẠNG

👉 nếu bí, nói:

> “This is mainly to control concurrency.”

> “This avoids duplicate work.”

> “This is a trade-off between performance and stability.”

---

# 😈 verdict

👉 nếu m handle được mấy câu này:

```text
m không còn ở level sinh viên demo
m đang ở level hiểu system behavior
```

---

Nếu m muốn, t có thể:
👉 simulate luôn 1 đoạn **thầy hỏi liên tục 3 câu xoáy → m trả lời → t chỉnh cho m nói mượt hơn** 😈


---

# ✅ Why FakeDB

> “I use a fake database to make the experiment deterministic and controllable. With a real database, latency can vary due to network or disk I/O. Here, I simulate fixed latency and a fixed connection pool to clearly demonstrate the bottleneck.”

```go
latency time.Duration
semaphore chan struct{}
```

> “This allows me to control latency and concurrency explicitly.”

**Does it affect results?**

> “It affects absolute latency, but not relative behavior. The goal is to compare system behavior, not real-world performance.”

---

# 📊 Caching Layers

> “We can have multiple caching layers depending on system design. Typically, there are 3 to 4 common layers.”

* **Client-side cache**: “Browser cache for static assets or API responses.”
* **CDN / Edge cache**: “Like Cloudflare, caching closer to users.”
* **Application cache**: “Redis or Memcached.”
* **Database cache**: “Internal DB caching like buffer pool.”

> “So caching is usually layered, not a single component.”

**(Optional flex)**

> “Each layer reduces load at a different level — from network to application to database.”