// SPDX-License-Identifier: AGPL-3.0-only

package metrics

import (
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriteGaugeFormat(t *testing.T) {
	var buf strings.Builder
	WriteGauge(&buf, "porthook_test_gauge", "A test gauge.", 42)

	got := buf.String()
	want := "# HELP porthook_test_gauge A test gauge.\n# TYPE porthook_test_gauge gauge\nporthook_test_gauge 42\n"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestWriteCounterFormat(t *testing.T) {
	var buf strings.Builder
	WriteCounter(&buf, "porthook_test_counter", "A test counter.", 7)

	got := buf.String()
	want := "# HELP porthook_test_counter A test counter.\n# TYPE porthook_test_counter counter\nporthook_test_counter 7\n"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestHistogramObserveBucketsCumulatively(t *testing.T) {
	h := NewHistogram([]float64{0.1, 0.5, 1})
	h.Observe(50 * time.Millisecond)  // falls in every bucket (<=0.1, <=0.5, <=1)
	h.Observe(300 * time.Millisecond) // falls in <=0.5, <=1
	h.Observe(2 * time.Second)        // falls only in +Inf

	var buf strings.Builder
	h.WriteTo(&buf, "porthook_test_latency_seconds", "Test latency.")
	got := buf.String()

	for _, want := range []string{
		`porthook_test_latency_seconds_bucket{le="0.1"} 1`,
		`porthook_test_latency_seconds_bucket{le="0.5"} 2`,
		`porthook_test_latency_seconds_bucket{le="1"} 2`,
		`porthook_test_latency_seconds_bucket{le="+Inf"} 3`,
		`porthook_test_latency_seconds_count 3`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestHistogramSumAccumulatesObservedDurations(t *testing.T) {
	h := NewHistogram(DefaultLatencyBuckets)
	h.Observe(1 * time.Second)
	h.Observe(2 * time.Second)

	var buf strings.Builder
	h.WriteTo(&buf, "porthook_test_sum_seconds", "Test sum.")
	got := buf.String()

	if !strings.Contains(got, "porthook_test_sum_seconds_sum 3\n") {
		t.Fatalf("output = %q, want sum of 3", got)
	}
}

func TestHistogramConcurrentObserveIsRaceFree(t *testing.T) {
	h := NewHistogram(DefaultLatencyBuckets)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Observe(10 * time.Millisecond)
		}()
	}
	wg.Wait()

	var buf strings.Builder
	h.WriteTo(&buf, "porthook_test_concurrent_seconds", "Test concurrency.")
	if !strings.Contains(buf.String(), "porthook_test_concurrent_seconds_count 100\n") {
		t.Fatalf("output = %q, want count of 100", buf.String())
	}
}

func TestWriteRuntimeStatsIncludesGoroutinesAndHeap(t *testing.T) {
	var buf strings.Builder
	WriteRuntimeStats(&buf, "porthook_test")
	got := buf.String()

	for _, want := range []string{
		"porthook_test_goroutines",
		"porthook_test_heap_alloc_bytes",
		"porthook_test_heap_sys_bytes",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want it to contain %q", got, want)
		}
	}

	// /proc/self/fd only exists on Linux; on other platforms the metric is
	// correctly omitted rather than reporting a misleading value.
	hasFDMetric := strings.Contains(got, "porthook_test_open_fds")
	if runtime.GOOS == "linux" && !hasFDMetric {
		t.Fatalf("output = %q, want porthook_test_open_fds on linux", got)
	}
	if runtime.GOOS != "linux" && hasFDMetric {
		t.Fatalf("output = %q, want no porthook_test_open_fds on %s", got, runtime.GOOS)
	}
}

func TestHistogramEmptyHasZeroCountAndSum(t *testing.T) {
	h := NewHistogram(DefaultLatencyBuckets)

	var buf strings.Builder
	h.WriteTo(&buf, "porthook_test_empty_seconds", "Test empty.")
	got := buf.String()

	if !strings.Contains(got, "porthook_test_empty_seconds_count 0\n") {
		t.Fatalf("output = %q, want count of 0", got)
	}
	if !strings.Contains(got, "porthook_test_empty_seconds_sum 0\n") {
		t.Fatalf("output = %q, want sum of 0", got)
	}
}
