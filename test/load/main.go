// Command load is a stdlib-only load generator for the Platform Agent
// control-plane endpoints. It simulates N agents performing an initial
// registration burst followed by a steady heartbeat load, and reports
// latency percentiles (p50/p95/p99), throughput, and error rates for each
// operation.
//
// Usage:
//
//	go run ./test/load \
//	  -url http://localhost:8080 \
//	  -agents 1000 \
//	  -duration 5m \
//	  -heartbeat-interval 30s \
//	  -ramp 30s \
//	  -creds creds.csv
//
// creds.csv (header required): clusterId,agentId,token
// When -creds is omitted, -synthetic generates random identities so you can
// characterize gateway/backend latency and rate-limiting under load even
// without pre-provisioned clusters (expect 4xx responses in that mode).
//
// Exit code is non-zero if the error rate exceeds -max-error-rate, so it can
// gate CI.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type creds struct {
	ClusterID string
	AgentID   string
	Token     string
}

type config struct {
	url               string
	agents            int
	duration          time.Duration
	heartbeatInterval time.Duration
	ramp              time.Duration
	requestTimeout    time.Duration
	credsFile         string
	synthetic         bool
	maxErrorRate      float64
	jsonOut           bool
}

func main() {
	cfg := parseFlags()

	agents, err := loadAgents(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load agents:", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, cfg.duration)
	defer cancel()

	client := &http.Client{Timeout: cfg.requestTimeout}

	regStats := newStats("register")
	hbStats := newStats("heartbeat")

	var wg sync.WaitGroup
	start := time.Now()
	for i, a := range agents {
		wg.Add(1)
		// Ramp start times evenly across the ramp window to avoid a thundering herd.
		delay := time.Duration(0)
		if cfg.ramp > 0 && len(agents) > 1 {
			delay = time.Duration(int64(cfg.ramp) * int64(i) / int64(len(agents)))
		}
		go func(a creds, delay time.Duration) {
			defer wg.Done()
			if !sleepCtx(ctx, delay) {
				return
			}
			runAgent(ctx, client, cfg, a, regStats, hbStats)
		}(a, delay)
	}
	wg.Wait()
	elapsed := time.Since(start)

	report := buildReport(cfg, elapsed, regStats, hbStats)
	if cfg.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		report.print()
	}

	if report.OverallErrorRate > cfg.maxErrorRate {
		fmt.Fprintf(os.Stderr, "FAIL: error rate %.4f exceeds max %.4f\n", report.OverallErrorRate, cfg.maxErrorRate)
		os.Exit(1)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.url, "url", "http://localhost:8080", "control-plane base URL (gateway)")
	flag.IntVar(&cfg.agents, "agents", 100, "number of simulated agents")
	flag.DurationVar(&cfg.duration, "duration", time.Minute, "total run duration")
	flag.DurationVar(&cfg.heartbeatInterval, "heartbeat-interval", 30*time.Second, "per-agent heartbeat interval")
	flag.DurationVar(&cfg.ramp, "ramp", 10*time.Second, "ramp window to stagger agent starts")
	flag.DurationVar(&cfg.requestTimeout, "request-timeout", 10*time.Second, "per-request timeout")
	flag.StringVar(&cfg.credsFile, "creds", "", "CSV of clusterId,agentId,token (header required)")
	flag.BoolVar(&cfg.synthetic, "synthetic", false, "generate random identities when -creds is not provided")
	flag.Float64Var(&cfg.maxErrorRate, "max-error-rate", 1.0, "fail (exit 1) if overall error rate exceeds this (0..1)")
	flag.BoolVar(&cfg.jsonOut, "json", false, "emit the report as JSON")
	flag.Parse()
	return cfg
}

func loadAgents(cfg config) ([]creds, error) {
	if cfg.credsFile != "" {
		return readCreds(cfg.credsFile)
	}
	if !cfg.synthetic {
		return nil, fmt.Errorf("either -creds <file> or -synthetic is required")
	}
	out := make([]creds, cfg.agents)
	for i := range out {
		out[i] = creds{
			ClusterID: "cluster-" + randHex(8),
			AgentID:   "agent-" + randHex(8),
			Token:     randHex(32),
		}
	}
	return out, nil
}

func readCreds(path string) ([]creds, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("creds file needs a header and at least one row")
	}
	var out []creds
	for _, row := range rows[1:] {
		if len(row) < 3 {
			continue
		}
		out = append(out, creds{ClusterID: row[0], AgentID: row[1], Token: row[2]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no credential rows found")
	}
	return out, nil
}

// runAgent performs one registration then heartbeats until ctx is done.
func runAgent(ctx context.Context, client *http.Client, cfg config, a creds, reg, hb *stats) {
	// Initial registration (idempotent server-side; safe to call once per run).
	doRegister(ctx, client, cfg, a, reg)

	ticker := time.NewTicker(cfg.heartbeatInterval)
	defer ticker.Stop()
	doHeartbeat(ctx, client, cfg, a, hb)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			doHeartbeat(ctx, client, cfg, a, hb)
		}
	}
}

func doRegister(ctx context.Context, client *http.Client, cfg config, a creds, st *stats) {
	body, _ := json.Marshal(map[string]any{
		"token":             a.Token,
		"agentId":           a.AgentID,
		"kubernetesVersion": "v1.28.0",
		"nodeCount":         3,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.url+"/v1/agent/register", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	timeRequest(client, req, st)
}

func doHeartbeat(ctx context.Context, client *http.Client, cfg config, a creds, st *stats) {
	body, _ := json.Marshal(map[string]any{
		"agentId":           a.AgentID,
		"kubernetesVersion": "v1.28.0",
		"nodeCount":         3,
		"apiServerHealthy":  true,
	})
	url := cfg.url + "/v1/agent/clusters/" + a.ClusterID + "/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Cluster-ID", a.ClusterID)
	req.Header.Set("X-Agent-ID", a.AgentID)
	timeRequest(client, req, st)
}

func timeRequest(client *http.Client, req *http.Request, st *stats) {
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		st.record(elapsed, false)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	st.record(elapsed, resp.StatusCode >= 200 && resp.StatusCode < 300)
}

// ---- stats ----------------------------------------------------------------

type stats struct {
	name    string
	mu      sync.Mutex
	samples []time.Duration
	ok      atomic.Int64
	failed  atomic.Int64
}

func newStats(name string) *stats { return &stats{name: name, samples: make([]time.Duration, 0, 1024)} }

func (s *stats) record(d time.Duration, ok bool) {
	if ok {
		s.ok.Add(1)
	} else {
		s.failed.Add(1)
	}
	s.mu.Lock()
	s.samples = append(s.samples, d)
	s.mu.Unlock()
}

type opReport struct {
	Op        string  `json:"op"`
	Count     int64   `json:"count"`
	OK        int64   `json:"ok"`
	Failed    int64   `json:"failed"`
	ErrorRate float64 `json:"errorRate"`
	RPS       float64 `json:"rps"`
	P50ms     float64 `json:"p50Ms"`
	P95ms     float64 `json:"p95Ms"`
	P99ms     float64 `json:"p99Ms"`
	MaxMs     float64 `json:"maxMs"`
}

func (s *stats) summarize(elapsed time.Duration) opReport {
	s.mu.Lock()
	samples := append([]time.Duration(nil), s.samples...)
	s.mu.Unlock()
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })

	ok, failed := s.ok.Load(), s.failed.Load()
	total := ok + failed
	rep := opReport{Op: s.name, Count: total, OK: ok, Failed: failed}
	if total > 0 {
		rep.ErrorRate = float64(failed) / float64(total)
	}
	if elapsed > 0 {
		rep.RPS = float64(total) / elapsed.Seconds()
	}
	rep.P50ms = pct(samples, 0.50)
	rep.P95ms = pct(samples, 0.95)
	rep.P99ms = pct(samples, 0.99)
	if len(samples) > 0 {
		rep.MaxMs = ms(samples[len(samples)-1])
	}
	return rep
}

func pct(sorted []time.Duration, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	return ms(sorted[idx])
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000.0 }

// ---- report ---------------------------------------------------------------

type report struct {
	Agents           int      `json:"agents"`
	DurationSec      float64  `json:"durationSec"`
	Register         opReport `json:"register"`
	Heartbeat        opReport `json:"heartbeat"`
	OverallErrorRate float64  `json:"overallErrorRate"`
}

func buildReport(cfg config, elapsed time.Duration, reg, hb *stats) report {
	r := reg.summarize(elapsed)
	h := hb.summarize(elapsed)
	total := r.Count + h.Count
	var errRate float64
	if total > 0 {
		errRate = float64(r.Failed+h.Failed) / float64(total)
	}
	return report{
		Agents:           cfg.agents,
		DurationSec:      elapsed.Seconds(),
		Register:         r,
		Heartbeat:        h,
		OverallErrorRate: errRate,
	}
}

func (r report) print() {
	fmt.Printf("Load test: %d agents over %.1fs\n\n", r.Agents, r.DurationSec)
	printOp(r.Register)
	printOp(r.Heartbeat)
	fmt.Printf("Overall error rate: %.4f\n", r.OverallErrorRate)
}

func printOp(o opReport) {
	fmt.Printf("[%s]\n", o.Op)
	fmt.Printf("  count=%d ok=%d failed=%d errRate=%.4f rps=%.1f\n", o.Count, o.OK, o.Failed, o.ErrorRate, o.RPS)
	fmt.Printf("  latency ms: p50=%.1f p95=%.1f p99=%.1f max=%.1f\n\n", o.P50ms, o.P95ms, o.P99ms, o.MaxMs)
}

// ---- helpers --------------------------------------------------------------

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
