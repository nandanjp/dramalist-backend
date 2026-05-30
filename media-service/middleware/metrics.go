package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds the Prometheus instruments for a service.
// Construct once at startup via NewMetrics and wire into Gin with Handler().
type Metrics struct {
	requestDuration *prometheus.HistogramVec
	requestsTotal   *prometheus.CounterVec
}

func NewMetrics(serviceName string) *Metrics {
	return &Metrics{
		requestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace:   "dramalist",
				Name:        "http_request_duration_seconds",
				Help:        "HTTP request latency by route.",
				Buckets:     []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"route", "method", "status_code"},
		),
		requestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace:   "dramalist",
				Name:        "http_requests_total",
				Help:        "Total HTTP requests by route.",
				ConstLabels: prometheus.Labels{"service": serviceName},
			},
			[]string{"route", "method", "status_code"},
		),
	}
}

func (m *Metrics) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		// c.FullPath() returns the route template (e.g. "/catalog/:id"), not the
		// instantiated URL. Using c.Request.URL.Path would create one label value
		// per UUID and cause Prometheus cardinality explosion.
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		m.requestDuration.
			WithLabelValues(route, c.Request.Method, strconv.Itoa(c.Writer.Status())).
			Observe(time.Since(start).Seconds())
		m.requestsTotal.
			WithLabelValues(route, c.Request.Method, strconv.Itoa(c.Writer.Status())).
			Inc()
	}
}