😤🔥 **OK bài của m đang ở mức rất mạnh rồi — giờ t giúp m “defense mode” full luôn**

👉 t sẽ làm đúng thứ m cần:

* **thầy hỏi → m mở file → chỉ đúng code → nói đúng câu**
* không lý thuyết lan man

---

# 🎯 5 CÂU HỎI NGUY HIỂM NHẤT (RẤT DỄ DÍNH)

---

# 💣 1. “Show me where thundering herd happens in your code”

👉 mở: `service.go`
📍 

👉 chỉ vào function:

```go
func (s *Service) CacheHerd(...)
```

👉 chỉ đoạn này:

```go
val, err := s.cache.Get(ctx, key)
...
result, err := s.db.Query(ctx, key)
```

👉 nói:

> “Here, every request checks the cache.”

> “If the cache misses, all requests directly call the database.”

👉 chỉ tiếp:

```go
s.cache.Set(ctx, key, result, cacheTTLHerd)
```

> “There is no coordination between requests.”

> “So when TTL expires, all requests miss at the same time and hit the database together.”

💀 chốt:

> “This is where thundering herd happens — multiple concurrent DB queries for the same key.”

---

# 💣 2. “Where exactly do you fix the herd problem?”

👉 mở: `service.go`
📍 

👉 chỉ:

```go
v, err, shared := s.group.Do(key, func() (interface{}, error) {
```

👉 nói:

> “This line is the fix.”

> “Singleflight ensures only one execution per key.”

👉 chỉ tiếp:

```go
result, err := s.db.Query(dbCtx, key)
```

> “Only this goroutine hits the database.”

👉 chỉ tiếp:

```go
if shared {
    atomic.AddInt64(&s.sfSharedCount, 1)
}
```

> “Other requests reuse the result instead of querying the database.”

💀 chốt:

> “So we reduce 200 database hits to just 1.”

---

# 💣 3. “Why do you check cache OUTSIDE singleflight?”

👉 chỉ:

```go
val, err := s.cache.Get(ctx, key)
if err == nil {
    return val, nil
}
```

👉 nói:

> “This is the fast path.”

> “Cache hits should not be blocked by singleflight.”

💀 chốt:

> “We only use singleflight for the slow path — the database.”

---

# 💣 4. “Explain this context thing — why baseCtx?”

👉 chỉ:

```go
dbCtx, cancel := context.WithTimeout(s.baseCtx, sfDBTimeout)
```

👉 nói:

> “This context is detached from the request.”

> “If we use request context, the leader request might cancel the DB query.”

> “Using baseCtx ensures the query completes for all waiting requests.”

💀 câu này = ăn điểm cực mạnh

---

# 💣 5. “Why double-check cache inside singleflight?”

👉 chỉ:

```go
if cached, err := s.cache.Get(dbCtx, key); err == nil {
    return cached, nil
}
```

👉 nói:

> “Another goroutine might have already populated the cache.”

> “So we check again to avoid unnecessary DB queries.”

---

# 💣 BONUS — nếu thầy xoáy tiếp

---

## ❓ “Why not just use mutex?”

👉 nói:

> “Mutex only provides exclusion.”

> “Singleflight provides result sharing.”

💀 khác biệt rất quan trọng

---

## ❓ “What happens if DB is slow?”

👉 nói:

> “All requests will wait for the leader.”

> “So singleflight trades parallelism for stability.”

---

## ❓ “Where do you simulate concurrency?”

👉 mở: `handler.go`
📍 

👉 chỉ:

```go
for i := 0; i < stormSize; i++ {
    go func() {
        fn(ctx, service.CacheKey)
    }()
}
```

👉 nói:

> “Here I spawn 200 goroutines to simulate concurrent requests.”

---

## ❓ “How do you ensure no race condition?”

👉 chỉ:

```go
atomic.AddInt64(...)
```

👉 nói:

> “All shared counters use atomic operations.”

---

# 🎯 CHIẾN LƯỢC CHO M (CỰC QUAN TRỌNG)

👉 khi bị hỏi:

## 1. LUÔN mở code

## 2. LUÔN chỉ tay

## 3. LUÔN nói theo format:

```text
what this line does  
why it exists  
what problem it solves
```

---

# 💀 Ví dụ chuẩn chỉnh (1 câu full điểm)

👉 (chỉ vào `.Do`)

> “This ensures only one goroutine executes the database query.”

> “Other requests wait and reuse the same result.”

> “So it prevents thundering herd.”
