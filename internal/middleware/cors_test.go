package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const testOrigin = "https://app.example.com"

func newCORSTestRouter(allowedOrigins []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS(allowedOrigins))
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/api/v1/stats/:code", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"short_code": c.Param("code")})
	})
	r.POST("/api/v1/shorten", func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})
	return r
}

func performCORSRequest(
	r http.Handler,
	method, path string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	return recorder
}

func TestCORSActualRequests(t *testing.T) {
	t.Run("allowed origin", func(t *testing.T) {
		router := newCORSTestRouter([]string{testOrigin})
		rec := performCORSRequest(router, http.MethodGet, "/api/v1/stats/abc123", map[string]string{
			"Origin": testOrigin,
		})

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, testOrigin)
		}
		if got := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, "X-RateLimit-Reset") {
			t.Errorf("Access-Control-Expose-Headers = %q, want rate limit headers", got)
		}
		if got := rec.Header().Get("Vary"); !strings.Contains(got, "Origin") {
			t.Errorf("Vary = %q, want Origin", got)
		}
	})

	t.Run("configured origin with trailing slash is normalized", func(t *testing.T) {
		router := newCORSTestRouter([]string{testOrigin + "/"})
		rec := performCORSRequest(router, http.MethodGet, "/health", map[string]string{
			"Origin": testOrigin,
		})

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, testOrigin)
		}
	})

	t.Run("disallowed origin gets no CORS headers", func(t *testing.T) {
		router := newCORSTestRouter([]string{testOrigin})
		rec := performCORSRequest(router, http.MethodGet, "/health", map[string]string{
			"Origin": "https://evil.example",
		})

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
		}
	})

	t.Run("request without Origin passes through", func(t *testing.T) {
		router := newCORSTestRouter([]string{testOrigin})
		rec := performCORSRequest(router, http.MethodGet, "/health", nil)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
		}
	})

	t.Run("wildcard allows any origin", func(t *testing.T) {
		router := newCORSTestRouter([]string{"*"})
		rec := performCORSRequest(router, http.MethodGet, "/health", map[string]string{
			"Origin": "https://any.example",
		})

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
		}
	})

	t.Run("subdomain wildcard allows vercel preview deployments", func(t *testing.T) {
		router := newCORSTestRouter([]string{"https://*.vercel.app"})

		for _, origin := range []string{
			"https://my-app.vercel.app",
			"https://my-app-git-main-user.vercel.app",
			"https://nested.preview.vercel.app",
		} {
			rec := performCORSRequest(router, http.MethodGet, "/health", map[string]string{
				"Origin": origin,
			})

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
				t.Errorf("origin %q: Access-Control-Allow-Origin = %q, want %q", origin, got, origin)
			}
		}
	})

	t.Run("subdomain wildcard rejects apex, lookalikes, and other schemes", func(t *testing.T) {
		router := newCORSTestRouter([]string{"https://*.vercel.app"})

		for _, origin := range []string{
			"https://vercel.app",
			"https://vercel.app.evil.com",
			"https://notvercel.app",
			"http://my-app.vercel.app",
		} {
			rec := performCORSRequest(router, http.MethodGet, "/health", map[string]string{
				"Origin": origin,
			})

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("origin %q: Access-Control-Allow-Origin = %q, want empty", origin, got)
			}
		}
	})
}

func TestCORSPreflight(t *testing.T) {
	t.Run("allowed origin", func(t *testing.T) {
		router := newCORSTestRouter([]string{testOrigin})
		rec := performCORSRequest(router, http.MethodOptions, "/api/v1/shorten", map[string]string{
			"Origin":                         testOrigin,
			"Access-Control-Request-Method":  "POST",
			"Access-Control-Request-Headers": "content-type, authorization",
		})

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, testOrigin)
		}
		if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
			t.Errorf("Access-Control-Allow-Methods = %q, want POST", got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "content-type, authorization" {
			t.Errorf("Access-Control-Allow-Headers = %q, want reflected headers", got)
		}
		if got := rec.Header().Get("Access-Control-Max-Age"); got != corsMaxAge {
			t.Errorf("Access-Control-Max-Age = %q, want %q", got, corsMaxAge)
		}

		vary := strings.Join(rec.Header().Values("Vary"), ", ")
		for _, want := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"} {
			if !strings.Contains(vary, want) {
				t.Errorf("Vary = %q, want %q", vary, want)
			}
		}
	})

	t.Run("default allowed headers when none requested", func(t *testing.T) {
		router := newCORSTestRouter([]string{testOrigin})
		rec := performCORSRequest(router, http.MethodOptions, "/api/v1/shorten", map[string]string{
			"Origin":                        testOrigin,
			"Access-Control-Request-Method": "POST",
		})

		if got := rec.Header().Get("Access-Control-Allow-Headers"); got != corsAllowedHeaders {
			t.Errorf("Access-Control-Allow-Headers = %q, want %q", got, corsAllowedHeaders)
		}
	})

	t.Run("disallowed origin is rejected", func(t *testing.T) {
		router := newCORSTestRouter([]string{testOrigin})
		rec := performCORSRequest(router, http.MethodOptions, "/api/v1/shorten", map[string]string{
			"Origin":                        "https://evil.example",
			"Access-Control-Request-Method": "POST",
		})

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want empty", got)
		}
	})

	t.Run("subdomain wildcard origin", func(t *testing.T) {
		router := newCORSTestRouter([]string{"https://*.vercel.app"})
		origin := "https://my-app-git-main-user.vercel.app"
		rec := performCORSRequest(router, http.MethodOptions, "/api/v1/shorten", map[string]string{
			"Origin":                         origin,
			"Access-Control-Request-Method":  "POST",
			"Access-Control-Request-Headers": "content-type",
		})

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
		}
	})
}
