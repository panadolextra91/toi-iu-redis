package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/huynhngocanhthu/toi-yeu-redis/internal/cache"
	"github.com/huynhngocanhthu/toi-yeu-redis/internal/db"
	"github.com/huynhngocanhthu/toi-yeu-redis/internal/handler"
	"github.com/huynhngocanhthu/toi-yeu-redis/internal/service"
)

const (
	dbPoolSize = 10
	dbLatency  = 200 * time.Millisecond
	redisAddr  = "localhost:6379"
	redisTTL   = 30 * time.Second
	serverAddr = ":8080"
)

func main() {
	// Server-level context — cancel on SIGINT/SIGTERM for graceful shutdown
	baseCtx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Initialize components
	fakeDB := db.NewFakeDB(dbPoolSize, dbLatency)
	redisCache := cache.NewRedisCache(redisAddr, redisTTL)
	svc := service.NewService(fakeDB, redisCache, baseCtx)
	startTime := time.Now()
	h := handler.NewHandler(svc, startTime)

	// Register routes
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Server
	srv := &http.Server{
		Addr:         serverAddr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second, // storm can take a while
		IdleTimeout:  60 * time.Second,
	}

	// Start
	go func() {
		log.Println("═══════════════════════════════════════════")
		log.Println("🔥 Redis Under Fire — Server starting")
		log.Printf("📡 Listening on %s", serverAddr)
		log.Printf("📊 DB Pool: %d | Latency: %v", dbPoolSize, dbLatency)
		log.Printf("🗄  Redis: %s | Default TTL: %v", redisAddr, redisTTL)
		log.Printf("🧠 Herd TTL: 3s | Singleflight DB Timeout: 5s")
		log.Println("═══════════════════════════════════════════")
		log.Println("")
		log.Println("Endpoints:")
		log.Println("  GET  /nocache          — always DB")
		log.Println("  GET  /cache            — cache-aside (TTL=30s)")
		log.Println("  GET  /cache-herd       — short TTL (3s), no protection")
		log.Println("  GET  /cache-protected  — singleflight + short TTL")
		log.Println("  GET  /stats            — metrics dashboard")
		log.Println("  POST /reset            — reset all counters")
		log.Println("  GET  /storm?mode=X     — 200-goroutine stress test")
		log.Println("")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-baseCtx.Done()
	log.Println("")
	log.Println("🛑 Shutdown signal received, draining connections...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("❌ Shutdown error: %v", err)
	}

	redisCache.Close()
	log.Println("✅ Server stopped gracefully")
}
