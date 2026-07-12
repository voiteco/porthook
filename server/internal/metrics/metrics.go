// SPDX-License-Identifier: AGPL-3.0-only

// Package metrics provides small, dependency-free helpers for writing
// Prometheus text-exposition format metrics, matching the hand-rolled style
// the gateway and control plane already use rather than adding a
// client_golang dependency.
package metrics

import (
	"database/sql"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"
)

// WriteGauge writes a single gauge sample in Prometheus text-exposition format.
func WriteGauge(w io.Writer, name, help string, value uint64) {
	writeSample(w, name, help, "gauge", value)
}

// WriteCounter writes a single counter sample in Prometheus text-exposition format.
func WriteCounter(w io.Writer, name, help string, value uint64) {
	writeSample(w, name, help, "counter", value)
}

func writeSample(w io.Writer, name, help, metricType string, value uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType)
	fmt.Fprintf(w, "%s %d\n", name, value)
}

// DefaultLatencyBuckets are reasonable upper bounds, in seconds, for
// request/round-trip latency histograms.
var DefaultLatencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// WriteRuntimeStats writes goroutine and heap-memory gauges for the current
// process, prefixed with prefix (for example "porthook_gateway").
func WriteRuntimeStats(w io.Writer, prefix string) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	WriteGauge(w, prefix+"_goroutines", "Current number of goroutines.", uint64(runtime.NumGoroutine()))
	WriteGauge(w, prefix+"_heap_alloc_bytes", "Bytes of allocated, reachable heap objects.", memStats.HeapAlloc)
	WriteGauge(w, prefix+"_heap_sys_bytes", "Bytes of heap memory obtained from the OS.", memStats.HeapSys)
}

// WriteDBPoolStats writes database connection pool gauges/counters from
// database/sql's own stats, prefixed with prefix.
func WriteDBPoolStats(w io.Writer, prefix string, stats sql.DBStats) {
	WriteGauge(w, prefix+"_db_open_connections", "Current number of established database connections.", uint64(stats.OpenConnections))
	WriteGauge(w, prefix+"_db_in_use_connections", "Current number of database connections in use.", uint64(stats.InUse))
	WriteGauge(w, prefix+"_db_idle_connections", "Current number of idle database connections.", uint64(stats.Idle))
	WriteCounter(w, prefix+"_db_wait_count_total", "Total database connections waited for because the pool was at its limit.", uint64(stats.WaitCount))
}

// Histogram is a fixed-bucket cumulative histogram safe for concurrent use.
// Observations are durations; buckets and the exposed sum are in seconds.
type Histogram struct {
	bounds   []float64
	buckets  []atomic.Uint64
	count    atomic.Uint64
	sumNanos atomic.Uint64
}

// NewHistogram returns a Histogram with the given ascending bucket upper
// bounds, in seconds.
func NewHistogram(bounds []float64) *Histogram {
	return &Histogram{bounds: bounds, buckets: make([]atomic.Uint64, len(bounds))}
}

// Observe records one duration measurement.
func (h *Histogram) Observe(d time.Duration) {
	seconds := d.Seconds()
	for i, bound := range h.bounds {
		if seconds <= bound {
			h.buckets[i].Add(1)
		}
	}
	h.count.Add(1)
	if d > 0 {
		h.sumNanos.Add(uint64(d.Nanoseconds()))
	}
}

// WriteTo writes the histogram in Prometheus text-exposition format.
func (h *Histogram) WriteTo(w io.Writer, name, help string) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s histogram\n", name)
	for i, bound := range h.bounds {
		fmt.Fprintf(w, "%s_bucket{le=%q} %d\n", name, strconv.FormatFloat(bound, 'f', -1, 64), h.buckets[i].Load())
	}
	count := h.count.Load()
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, count)
	fmt.Fprintf(w, "%s_sum %s\n", name, strconv.FormatFloat(float64(h.sumNanos.Load())/1e9, 'f', -1, 64))
	fmt.Fprintf(w, "%s_count %d\n", name, count)
}
