package cache

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisTimeout = 2 * time.Second

// RedisCache wraps go-redis client with configurable TTL and context-aware operations.
type RedisCache struct {
	client *redis.Client
	ttl    time.Duration // default TTL, can be overridden per Set call
}

// NewRedisCache creates a new Redis cache and pings to verify connection.
func NewRedisCache(addr string, defaultTTL time.Duration) *RedisCache {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), redisTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("⚠️  Redis ping failed: %v (will continue with fallback)", err)
	} else {
		log.Printf("✅ Redis connected: %s", addr)
	}

	return &RedisCache{
		client: client,
		ttl:    defaultTTL,
	}
}

// Get retrieves a value from cache.
// Returns redis.Nil error on cache miss — caller MUST distinguish this from real errors.
func (c *RedisCache) Get(ctx context.Context, key string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	return c.client.Get(ctx, key).Result()
}

// Set stores a value in cache with explicit TTL.
// TTL is passed explicitly to prevent accidental use of wrong default.
func (c *RedisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	return c.client.Set(ctx, key, value, ttl).Err()
}

// Delete removes a specific key from cache. Used for targeted reset.
func (c *RedisCache) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	return c.client.Del(ctx, key).Err()
}

// Close closes the Redis connection.
func (c *RedisCache) Close() error {
	return c.client.Close()
}

// IsNil checks if an error is a Redis cache miss (redis.Nil).
func IsNil(err error) bool {
	return err == redis.Nil
}
