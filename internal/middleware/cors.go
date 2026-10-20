package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	corsAllowedMethods = "GET, POST, OPTIONS"
	corsAllowedHeaders = "Accept, Content-Type"
	corsExposedHeaders = "X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset"
	corsMaxAge         = "600"
)

// corsSubdomainPattern matches any subdomain of a parent domain for one scheme.
// Example: https://*.vercel.app -> {scheme: "https", suffix: ".vercel.app"}.
type corsSubdomainPattern struct {
	scheme string
	suffix string
}

func parseCORSSubdomainPattern(origin string) (corsSubdomainPattern, bool) {
	scheme, host, ok := strings.Cut(origin, "://")
	if !ok || scheme == "" || !strings.HasPrefix(host, "*.") {
		return corsSubdomainPattern{}, false
	}

	suffix := strings.TrimPrefix(host, "*")
	if suffix == "." || strings.ContainsAny(suffix, "/?#@:") {
		return corsSubdomainPattern{}, false
	}

	return corsSubdomainPattern{
		scheme: strings.ToLower(scheme),
		suffix: strings.ToLower(suffix),
	}, true
}

func (p corsSubdomainPattern) matches(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || !strings.EqualFold(u.Scheme, p.scheme) {
		return false
	}

	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	return len(host) > len(p.suffix) && strings.HasSuffix(host, p.suffix)
}

// CORS allows browser requests from a configured list of origins.
//
// Example CORS_ALLOWED_ORIGINS values:
//   - https://app.example.com
//   - https://app.example.com,https://staging.example.com
//   - https://*.vercel.app (any HTTPS subdomain, e.g. preview deployments)
//   - * (any origin; intended for local development only)
//
// Wildcard entries match subdomains only, never the apex domain or lookalike
// domains such as vercel.app.evil.com.
//
// Preflight requests are answered here, before route-level middleware such as
// the rate limiter runs. Requests without an Origin header (curl, server-to-
// server calls) pass through unchanged.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	origins := make(map[string]struct{}, len(allowedOrigins))
	subdomains := make([]corsSubdomainPattern, 0)
	allowAll := false

	for _, origin := range allowedOrigins {
		origin = strings.TrimSuffix(strings.TrimSpace(origin), "/")
		switch {
		case origin == "":
			continue
		case origin == "*":
			allowAll = true
		default:
			if pattern, ok := parseCORSSubdomainPattern(origin); ok {
				subdomains = append(subdomains, pattern)
				continue
			}
			origins[origin] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		c.Writer.Header().Add("Vary", "Origin")

		_, originAllowed := origins[origin]
		if !originAllowed {
			for _, pattern := range subdomains {
				if pattern.matches(origin) {
					originAllowed = true
					break
				}
			}
		}

		if !originAllowed && !allowAll {
			// Keep same-origin and non-browser traffic working. Browsers block
			// disallowed cross-origin responses because no CORS headers are set.
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		if allowAll {
			c.Header("Access-Control-Allow-Origin", "*")
		} else {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Access-Control-Expose-Headers", corsExposedHeaders)

		if c.Request.Method != http.MethodOptions {
			c.Next()
			return
		}

		// Reflect requested headers so future additions (e.g. Authorization)
		// work without another backend change. The origin is already trusted.
		if requestedHeaders := c.GetHeader("Access-Control-Request-Headers"); requestedHeaders != "" {
			c.Header("Access-Control-Allow-Headers", requestedHeaders)
		} else {
			c.Header("Access-Control-Allow-Headers", corsAllowedHeaders)
		}
		c.Header("Access-Control-Allow-Methods", corsAllowedMethods)
		c.Header("Access-Control-Max-Age", corsMaxAge)
		c.Writer.Header().Add("Vary", "Access-Control-Request-Method")
		c.Writer.Header().Add("Vary", "Access-Control-Request-Headers")

		c.AbortWithStatus(http.StatusNoContent)
	}
}
