package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/example/url-shortener/internal/cache"
)

// RateLimiter returns Gin middleware implementing fixed-window rate limiting via Redis.
// limit: requests per window per IP (e.g., 100 req/min)
// window: time window
//
// Design choice: Fixed window counter with Redis Lua script (atomic INCR+EXPIRE).
// Why Redis vs in-memory? -> distributed: works across N app instances.
// Why fixed window vs token bucket / sliding window?
//  + Fixed window: simplest, O(1), low Redis load. Good enough for abuse protection.
//  - Burst at window boundary (2x limit). Sliding window is more accurate but needs
//    sorted sets / Lua + more memory. Token bucket needs periodic refill logic.
// Tradeoff: start with fixed window, upgrade to sliding window log if abuse detected.
//
// Alternative: use github.com/ulule/limiter or Envoy/NGINX rate limit in production.
func RateLimiter(c *cache.RedisCache, limit int, window time.Duration) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ip := ctx.ClientIP()
		key := ip // could also be "ip:" + ip + ":path:" + ctx.FullPath() for per-route

		allowed, remaining, resetIn, err := c.Allow(ctx.Request.Context(), key, limit, window)
		// If Redis is down, fail open (allow request) — availability over strict limiting
		if err != nil {
			ctx.Next()
			return
		}

		// Headers for client visibility
		ctx.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		ctx.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		ctx.Header("X-RateLimit-Reset", strconv.Itoa(int(resetIn.Seconds())))

		if !allowed {
			ctx.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": int(resetIn.Seconds()),
			})
			return
		}

		ctx.Next()
	}
}
