// SPDX-License-Identifier: AGPL-3.0-only

// Command loadgen drives sustained public HTTP and WebSocket traffic
// through already-registered porthook tunnels, to verify capacity limits
// and help detect resource leaks under load. It only talks to the
// gateway's public listener; tunnel registration is handled separately by
// real `porthook` agent processes (see scripts/smoke-capacity.sh), since
// this tool lives outside the agent module and cannot import its internal
// package.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"nhooyr.io/websocket"
)

type config struct {
	publicAddr      string
	rootDomain      string
	subdomainPrefix string
	tunnelCount     int
	wsStreams       int
	targetRPS       float64
	duration        time.Duration
	reportInterval  time.Duration
	httpPath        string
	wsPath          string
	maxErrorRate    float64
	reportPath      string
}

func configFromEnv() config {
	return config{
		publicAddr:      envString("LOADGEN_PUBLIC_ADDR", "127.0.0.1:18080"),
		rootDomain:      envString("LOADGEN_ROOT_DOMAIN", "loadtest.porthook.test"),
		subdomainPrefix: envString("LOADGEN_SUBDOMAIN_PREFIX", "loadtest-"),
		tunnelCount:     envInt("LOADGEN_TUNNEL_COUNT", 100),
		wsStreams:       envIntAllowZero("LOADGEN_WS_STREAMS", 400),
		targetRPS:       envFloat("LOADGEN_TARGET_RPS", 100),
		duration:        envDuration("LOADGEN_DURATION", 30*time.Minute),
		reportInterval:  envDuration("LOADGEN_REPORT_INTERVAL", 30*time.Second),
		httpPath:        envString("LOADGEN_HTTP_PATH", "/smoke.txt"),
		wsPath:          envString("LOADGEN_WS_PATH", "/socket"),
		maxErrorRate:    envFloat("LOADGEN_MAX_ERROR_RATE", 0.01),
		reportPath:      os.Getenv("LOADGEN_REPORT_PATH"),
	}
}

func main() {
	cfg := configFromEnv()
	log.Printf("loadgen: tunnels=%d ws_streams=%d target_rps=%.1f duration=%s public_addr=%s root_domain=%s",
		cfg.tunnelCount, cfg.wsStreams, cfg.targetRPS, cfg.duration, cfg.publicAddr, cfg.rootDomain)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, cfg.duration)
	defer cancel()

	stats := newStats()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runHTTPLoad(ctx, cfg, stats)
	}()

	for i := 0; i < cfg.wsStreams; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			runWSStream(ctx, cfg, stats, i)
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		reportPeriodically(ctx, cfg, stats)
	}()

	wg.Wait()

	summary := stats.summary(time.Since(stats.startedAt))
	printSummary(summary)
	if cfg.reportPath != "" {
		if err := writeReportJSON(cfg.reportPath, summary); err != nil {
			log.Printf("loadgen: warning: could not write report to %s: %v", cfg.reportPath, err)
		}
	}

	if summary.ErrorRate > cfg.maxErrorRate {
		fmt.Fprintf(os.Stderr, "loadgen: error rate %.4f exceeds max %.4f\n", summary.ErrorRate, cfg.maxErrorRate)
		os.Exit(1)
	}
}

// --- HTTP load ---

func runHTTPLoad(ctx context.Context, cfg config, stats *stats) {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, cfg.publicAddr)
			},
			MaxIdleConnsPerHost: cfg.tunnelCount * 2,
		},
		Timeout: 30 * time.Second,
	}

	interval := time.Duration(float64(time.Second) / cfg.targetRPS)
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var inFlight sync.WaitGroup
	defer inFlight.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			inFlight.Add(1)
			go func() {
				defer inFlight.Done()
				doOneHTTPRequest(cfg, client, stats)
			}()
		}
	}
}

// doOneHTTPRequest deliberately uses a context independent of the driver's
// overall test deadline: once the request loop stops issuing new requests,
// any already in-flight request should still be allowed to finish normally
// rather than being counted as an error purely because the test ended.
func doOneHTTPRequest(cfg config, client *http.Client, stats *stats) {
	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host := subdomainHost(cfg, rand.Intn(cfg.tunnelCount))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+cfg.publicAddr+cfg.httpPath, nil)
	if err != nil {
		stats.recordHTTP(0, err)
		return
	}
	req.Host = host

	started := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(started)
	if err != nil {
		stats.recordHTTPDuration(duration, 0, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	stats.recordHTTPDuration(duration, resp.StatusCode, nil)
}

// --- WebSocket streams ---

func runWSStream(ctx context.Context, cfg config, stats *stats, index int) {
	host := subdomainHost(cfg, index%cfg.tunnelCount)
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return
		}
		if err := oneWSSession(ctx, cfg, host, stats); err != nil {
			stats.recordWSError()
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 10*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

func oneWSSession(ctx context.Context, cfg config, host string, stats *stats) error {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, cfg.publicAddr)
			},
		},
	}

	conn, _, err := websocket.Dial(dialCtx, "ws://"+cfg.publicAddr+cfg.wsPath, &websocket.DialOptions{
		HTTPClient: httpClient,
		Host:       host,
	})
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	stats.wsConnected()
	defer stats.wsDisconnected()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "loadgen done")
			return nil
		case <-ticker.C:
			started := time.Now()
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, []byte("ping"))
			cancel()
			if err != nil {
				return err
			}
			readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, _, err = conn.Read(readCtx)
			cancel()
			if err != nil {
				return err
			}
			stats.recordWSRoundTrip(time.Since(started))
		}
	}
}

func subdomainHost(cfg config, index int) string {
	return cfg.subdomainPrefix + strconv.Itoa(index) + "." + cfg.rootDomain
}

// --- stats ---

type stats struct {
	startedAt time.Time

	httpRequests atomic.Int64
	httpErrors   atomic.Int64

	wsActive     atomic.Int64
	wsMaxActive  atomic.Int64
	wsErrors     atomic.Int64
	wsRoundTrips atomic.Int64

	mu        sync.Mutex
	latencies []time.Duration
}

func newStats() *stats {
	return &stats{startedAt: time.Now(), latencies: make([]time.Duration, 0, 1<<16)}
}

func (s *stats) recordHTTP(status int, err error) {
	s.recordHTTPDuration(0, status, err)
}

func (s *stats) recordHTTPDuration(d time.Duration, status int, err error) {
	s.httpRequests.Add(1)
	if err != nil || status == 0 || status >= 500 {
		s.httpErrors.Add(1)
	}
	if d > 0 {
		s.mu.Lock()
		s.latencies = append(s.latencies, d)
		s.mu.Unlock()
	}
}

func (s *stats) wsConnected() {
	active := s.wsActive.Add(1)
	for {
		max := s.wsMaxActive.Load()
		if active <= max || s.wsMaxActive.CompareAndSwap(max, active) {
			return
		}
	}
}

func (s *stats) wsDisconnected() {
	s.wsActive.Add(-1)
}

func (s *stats) recordWSError() {
	s.wsErrors.Add(1)
}

func (s *stats) recordWSRoundTrip(d time.Duration) {
	s.wsRoundTrips.Add(1)
	s.mu.Lock()
	s.latencies = append(s.latencies, d)
	s.mu.Unlock()
}

type summary struct {
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	HTTPRequests   int64   `json:"http_requests_total"`
	HTTPErrors     int64   `json:"http_errors_total"`
	ErrorRate      float64 `json:"error_rate"`
	AchievedRPS    float64 `json:"achieved_rps"`
	WSActive       int64   `json:"ws_active"`
	WSMaxActive    int64   `json:"ws_max_active"`
	WSErrors       int64   `json:"ws_errors_total"`
	WSRoundTrips   int64   `json:"ws_round_trips_total"`
	LatencyP50Ms   float64 `json:"latency_p50_ms"`
	LatencyP95Ms   float64 `json:"latency_p95_ms"`
	LatencyP99Ms   float64 `json:"latency_p99_ms"`
	LatencyMaxMs   float64 `json:"latency_max_ms"`
	SampledLatency int     `json:"sampled_latency_count"`
}

func (s *stats) summary(elapsed time.Duration) summary {
	requests := s.httpRequests.Load()
	errors := s.httpErrors.Load()

	s.mu.Lock()
	samples := make([]time.Duration, len(s.latencies))
	copy(samples, s.latencies)
	s.mu.Unlock()
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })

	var errRate float64
	if requests > 0 {
		errRate = float64(errors) / float64(requests)
	}
	var rps float64
	if elapsed > 0 {
		rps = float64(requests) / elapsed.Seconds()
	}

	return summary{
		ElapsedSeconds: elapsed.Seconds(),
		HTTPRequests:   requests,
		HTTPErrors:     errors,
		ErrorRate:      errRate,
		AchievedRPS:    rps,
		WSActive:       s.wsActive.Load(),
		WSMaxActive:    s.wsMaxActive.Load(),
		WSErrors:       s.wsErrors.Load(),
		WSRoundTrips:   s.wsRoundTrips.Load(),
		LatencyP50Ms:   percentileMs(samples, 0.50),
		LatencyP95Ms:   percentileMs(samples, 0.95),
		LatencyP99Ms:   percentileMs(samples, 0.99),
		LatencyMaxMs:   percentileMs(samples, 1.0),
		SampledLatency: len(samples),
	}
}

func percentileMs(sorted []time.Duration, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return float64(sorted[idx]) / float64(time.Millisecond)
}

func printSummary(sm summary) {
	log.Printf("loadgen: elapsed=%.0fs requests=%d errors=%d error_rate=%.4f achieved_rps=%.1f ws_active=%d ws_max_active=%d ws_errors=%d p50=%.1fms p95=%.1fms p99=%.1fms max=%.1fms samples=%d",
		sm.ElapsedSeconds, sm.HTTPRequests, sm.HTTPErrors, sm.ErrorRate, sm.AchievedRPS,
		sm.WSActive, sm.WSMaxActive, sm.WSErrors, sm.LatencyP50Ms, sm.LatencyP95Ms, sm.LatencyP99Ms, sm.LatencyMaxMs, sm.SampledLatency)
}

func reportPeriodically(ctx context.Context, cfg config, stats *stats) {
	ticker := time.NewTicker(cfg.reportInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			printSummary(stats.summary(time.Since(stats.startedAt)))
		}
	}
}

func writeReportJSON(path string, sm summary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(sm)
}

// --- env helpers ---

func envString(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) int {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

// envIntAllowZero is like envInt but treats an explicit 0 as a valid value
// (e.g. LOADGEN_WS_STREAMS=0 to disable the WebSocket driver entirely)
// rather than falling back to the default. Only safe for settings whose
// zero value is meaningful and doesn't lead to a divide-by-zero or panic
// elsewhere (unlike tunnel count or target RPS).
func envIntAllowZero(name string, fallback int) int {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func envFloat(name string, fallback float64) float64 {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(v, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
