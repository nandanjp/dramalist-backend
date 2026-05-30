package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger replaces gin.Logger() with structured JSON output parseable by Loki.
func RequestLogger(serviceName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		slog.LogAttrs(c.Request.Context(), slog.LevelInfo, "request",
			slog.String("service", serviceName),
			slog.String("route", route),
			slog.String("method", c.Request.Method),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("latency_ms", time.Since(start).Milliseconds()),
			slog.String("user_id", c.GetHeader("X-User-Id")),
			slog.String("client_ip", c.ClientIP()),
		)
	}
}