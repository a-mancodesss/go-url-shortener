package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrCacheMiss = errors.New("cache miss")

// RedisCache wraps go-redis for URL cache and rate limiting.
type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisCache(client *redis.Client, ttl time.Duration) *RedisCache {
	return &RedisCache{client: client, ttl: ttl}
}

func (c *RedisCache) Client() *redis.Client { return c.client }

// --- URL Cache (cache-aside) ---

func cacheKey(code string) string { return "url:" + code }

// Set caches short_code -> long_url
func (c *RedisCache) Set(ctx context.Context, code, longURL string) error {
	return c.client.Set(ctx, cacheKey(code), longURL, c.ttl).Err()
}

// SetWithTTL caches with custom TTL (e.g., respects expires_at)
func (c *RedisCache) SetWithTTL(ctx context.Context, code, longURL string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.ttl
	}
	return c.client.Set(ctx, cacheKey(code), longURL, ttl).Err()
}

// Get retrieves long_url from cache.
func (c *RedisCache) Get(ctx context.Context, code string) (string, error) {
	val, err := c.client.Get(ctx, cacheKey(code)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrCacheMiss
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// Delete evicts cache entry.
func (c *RedisCache) Delete(ctx context.Context, code string) error {
	return c.client.Del(ctx, cacheKey(code)).Err()
}

// --- Rate Limiting: Fixed Window Counter ---
// Simple and efficient, uses one Redis key per window.
// For stricter guarantees, replace with sliding window via Lua script.

// Allow checks if request is within limit.
// key: e.g., "rl:192.168.1.1" or "rl:api:<key>"
// limit: max requests, window: e.g., 10 * time.Second
// Returns allowed, remaining, resetAt, error
func (c *RedisCache) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, int, time.Duration, error) {
	redisKey := "rl:" + key

	// Lua script for atomic INCR + EXPIRE
	// KEYS[1] = key, ARGV[1] = window seconds
	script := redis.NewScript(`
		local current = redis.call("INCR", KEYS[1])
		if current == 1 then
			redis.call("EXPIRE", KEYS[1], ARGV[1])
		end
		local ttl = redis.call("PTTL", KEYS[1])
		return {current, ttl}
	`)

	result, err := script.Run(ctx, c.client, []string{redisKey}, int(window.Seconds())).Result()
	if err != nil {
		return false, 0, 0, err
	}

	arr, ok := result.([]interface{})
	if !ok || len(arr) != 2 {
		return false, 0, 0, errors.New("unexpected redis script result")
	}

	count, _ := arr[0].(int64)
	ttlMs, _ := arr[1].(int64)
	resetIn := time.Duration(ttlMs) * time.Millisecond
	remaining := limit - int(count)
	if remaining < 0 {
		remaining = 0
	}

	allowed := count <= int64(limit)
	return allowed, remaining, resetIn, nil
}

// Ping checks Redis connectivity.
func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}
