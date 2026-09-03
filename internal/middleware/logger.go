package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger is a lightweight structured logger middleware.
// In production, replace with zap/logrus + trace ID.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		// Use gin's DefaultWriter (stdout); production should use structured JSON logger
		_ = path
		_ = query
		_ = latency
		_ = status
		// Example: log.Printf("[%d] %s %s | %s | %s", status, c.Request.Method, path, latency, c.ClientIP())
	}
}
