package otel

import (
	"context"
	"net/http"
	"time"

	"github.com/jimmitjoo/gemquick/logging"
	"go.opentelemetry.io/otel/attribute"
)

// MetricsMiddleware returns a middleware that records HTTP metrics.
// It integrates with Gemquick's existing metrics system.
func MetricsMiddleware(metrics *logging.ApplicationMetrics) func(http.Handler) http.Handler {
	if metrics == nil {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Track active connections
			metrics.ActiveConnections.Inc()
			defer metrics.ActiveConnections.Dec()

			// Wrap response writer
			rw := newResponseWriter(w)

			// Serve request
			next.ServeHTTP(rw, r)

			// Record metrics
			duration := time.Since(start)
			metrics.RequestsTotal.Inc()
			metrics.RequestDuration.Observe(duration.Seconds())

			// Record status code (simplified - ideally we'd have a per-status counter)
			if rw.statusCode >= 500 {
				metrics.ErrorsTotal.Inc()
			}
		})
	}
}

// HTTPRequestMetrics records metrics for an HTTP request.
// Use this when you need manual control over metric recording.
type HTTPRequestMetrics struct {
	metrics   *logging.ApplicationMetrics
	startTime time.Time
	method    string
	path      string
}

// StartRequest begins tracking metrics for an HTTP request.
func StartRequest(metrics *logging.ApplicationMetrics, method, path string) *HTTPRequestMetrics {
	if metrics == nil {
		return nil
	}

	metrics.ActiveConnections.Inc()

	return &HTTPRequestMetrics{
		metrics:   metrics,
		startTime: time.Now(),
		method:    method,
		path:      path,
	}
}

// End completes the metric recording for the request.
func (m *HTTPRequestMetrics) End(statusCode int) {
	if m == nil || m.metrics == nil {
		return
	}

	m.metrics.ActiveConnections.Dec()
	m.metrics.RequestsTotal.Inc()

	duration := time.Since(m.startTime)
	m.metrics.RequestDuration.Observe(duration.Seconds())

	if statusCode >= 500 {
		m.metrics.ErrorsTotal.Inc()
	}
}

// RecordDuration is a helper to record operation duration.
func RecordDuration(histogram *logging.Histogram, start time.Time) {
	if histogram == nil {
		return
	}
	histogram.Observe(time.Since(start).Seconds())
}

// SpanMetrics bridges OpenTelemetry spans with Gemquick metrics.
// It automatically records span duration as a histogram observation.
type SpanMetrics struct {
	ctx       context.Context
	histogram *logging.Histogram
	counter   *logging.Counter
	startTime time.Time
	attrs     []attribute.KeyValue
}

// NewSpanMetrics creates a span metrics tracker.
func NewSpanMetrics(ctx context.Context, histogram *logging.Histogram, counter *logging.Counter) *SpanMetrics {
	if counter != nil {
		counter.Inc()
	}
	return &SpanMetrics{
		ctx:       ctx,
		histogram: histogram,
		counter:   counter,
		startTime: time.Now(),
	}
}

// WithAttributes adds attributes to track.
func (s *SpanMetrics) WithAttributes(attrs ...attribute.KeyValue) *SpanMetrics {
	s.attrs = append(s.attrs, attrs...)
	return s
}

// End records the duration metric.
func (s *SpanMetrics) End() {
	if s.histogram == nil {
		return
	}
	s.histogram.Observe(time.Since(s.startTime).Seconds())
}

// EndWithError records the duration and optionally increments an error counter.
func (s *SpanMetrics) EndWithError(err error, errorCounter *logging.Counter) {
	s.End()
	if err != nil && errorCounter != nil {
		errorCounter.Inc()
	}
}

// MetricsRecorder provides a simple interface for recording custom metrics.
type MetricsRecorder struct {
	registry *logging.MetricRegistry
}

// NewMetricsRecorder creates a new metrics recorder.
func NewMetricsRecorder(registry *logging.MetricRegistry) *MetricsRecorder {
	return &MetricsRecorder{registry: registry}
}

// Counter gets or creates a counter by name.
func (r *MetricsRecorder) Counter(name string) *logging.Counter {
	if r.registry == nil {
		return nil
	}
	if m, ok := r.registry.Get(name); ok {
		if c, ok := m.(*logging.Counter); ok {
			return c
		}
	}
	c := logging.NewCounter(name, nil)
	r.registry.Register(c)
	return c
}

// Gauge gets or creates a gauge by name.
func (r *MetricsRecorder) Gauge(name string) *logging.Gauge {
	if r.registry == nil {
		return nil
	}
	if m, ok := r.registry.Get(name); ok {
		if g, ok := m.(*logging.Gauge); ok {
			return g
		}
	}
	g := logging.NewGauge(name, nil)
	r.registry.Register(g)
	return g
}

// Histogram gets or creates a histogram by name.
func (r *MetricsRecorder) Histogram(name string) *logging.Histogram {
	if r.registry == nil {
		return nil
	}
	if m, ok := r.registry.Get(name); ok {
		if h, ok := m.(*logging.Histogram); ok {
			return h
		}
	}
	h := logging.NewHistogram(name, nil)
	r.registry.Register(h)
	return h
}

// Inc increments a counter by name.
func (r *MetricsRecorder) Inc(name string) {
	if c := r.Counter(name); c != nil {
		c.Inc()
	}
}

// Add adds to a counter by name.
func (r *MetricsRecorder) Add(name string, delta int64) {
	if c := r.Counter(name); c != nil {
		c.Add(delta)
	}
}

// Set sets a gauge by name.
func (r *MetricsRecorder) Set(name string, value int64) {
	if g := r.Gauge(name); g != nil {
		g.Set(value)
	}
}

// Observe records a histogram observation by name.
func (r *MetricsRecorder) Observe(name string, value float64) {
	if h := r.Histogram(name); h != nil {
		h.Observe(value)
	}
}

// TimeSince records the duration since start as a histogram observation.
func (r *MetricsRecorder) TimeSince(name string, start time.Time) {
	r.Observe(name, time.Since(start).Seconds())
}
