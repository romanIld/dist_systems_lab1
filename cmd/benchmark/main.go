// Command benchmark is the part 1.4 orchestrator. For each model it starts the
// server, sweeps a messages x concurrency grid, runs the matching load client
// per cell, samples the server CPU/RSS and writes the numbers to CSV under
// -out. Binaries must be built already (scripts/build.ps1, or `go build -o bin/`).
//
// Usage:
//
//	benchmark -bin bin -out results -messages 10,100,1000 -concurrency 1,10,50,100,200
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"lab1/internal/metrics"
	"lab1/internal/procstat"
)

// approach describes one communication model under test.
type approach struct {
	name      string // threading | async | grpc
	serverBin string
	clientBin string
	port      int
	concFlag  string // client flag carrying the concurrency level
	extraArgs []string
}

type options struct {
	binDir      string
	outDir      string
	host        string
	approaches  []string
	messages    []int
	concurrency []int
	repeat      int
	asyncLoops  int
	async500    bool
	warmup      bool
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	var (
		binDir      = flag.String("bin", "bin", "directory holding the built binaries")
		outDir      = flag.String("out", "results", "directory for CSV output")
		host        = flag.String("host", "127.0.0.1", "address the servers bind to and clients connect to")
		approaches  = flag.String("approaches", "threading,async,grpc", "comma-separated models to run")
		messages    = flag.String("messages", "10,100,1000", "comma-separated message counts per client")
		concurrency = flag.String("concurrency", "1,10,50,100,200", "comma-separated client-concurrency levels")
		repeat      = flag.Int("repeat", 1, "repetitions per grid cell")
		asyncLoops  = flag.Int("async-loops", runtime.NumCPU(), "event-loop goroutines for the async server")
		async500    = flag.Bool("async-500", true, "add a 500-client run for the async approach")
		warmup      = flag.Bool("warmup", true, "run one discarded warm-up per approach")
	)
	flag.Parse()

	opts := options{
		binDir:      *binDir,
		outDir:      *outDir,
		host:        *host,
		approaches:  splitCSV(*approaches),
		messages:    splitCSVInt(*messages),
		concurrency: splitCSVInt(*concurrency),
		repeat:      *repeat,
		asyncLoops:  *asyncLoops,
		async500:    *async500,
		warmup:      *warmup,
	}

	if err := run(opts); err != nil {
		log.Fatalf("benchmark: %v", err)
	}
}

func run(opts options) error {
	if err := os.MkdirAll(filepath.Join(opts.outDir, "raw"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(opts.outDir, "logs"), 0o755); err != nil {
		return err
	}

	catalog := map[string]approach{
		"threading": {
			name: "threading", serverBin: "server-threading", clientBin: "client-tcp",
			port: 9101, concFlag: "-concurrency",
		},
		"async": {
			name: "async", serverBin: "server-async", clientBin: "client-tcp",
			port: 9102, concFlag: "-concurrency",
			extraArgs: []string{"-loops", strconv.Itoa(opts.asyncLoops)},
		},
		"grpc": {
			name: "grpc", serverBin: "server-grpc", clientBin: "client-grpc",
			port: 9103, concFlag: "-streams",
		},
	}

	// Pre-flight: every needed binary must exist before we start timing.
	for _, name := range opts.approaches {
		ap, ok := catalog[name]
		if !ok {
			return fmt.Errorf("unknown approach %q", name)
		}
		for _, bin := range []string{ap.serverBin, ap.clientBin} {
			p := filepath.Join(opts.binDir, bin+binExt())
			if _, err := os.Stat(p); err != nil {
				return fmt.Errorf("missing binary %s (build with scripts/build.ps1): %w", p, err)
			}
		}
	}

	var rows []row
	for _, name := range opts.approaches {
		ap := catalog[name]
		got, err := runApproach(opts, ap)
		if err != nil {
			return fmt.Errorf("approach %s: %w", name, err)
		}
		rows = append(rows, got...)
	}

	stamp := time.Now().Format("20060102-150405")
	rawPath := filepath.Join(opts.outDir, "raw", "bench-"+stamp+".csv")
	sumPath := filepath.Join(opts.outDir, "summary.csv")
	if err := writeCSV(rawPath, rows); err != nil {
		return err
	}
	if err := writeCSV(sumPath, rows); err != nil {
		return err
	}
	log.Printf("wrote %d rows to %s and %s", len(rows), rawPath, sumPath)
	printTable(rows)
	return nil
}

// runApproach starts one server, runs the whole grid against it and returns the
// collected rows.
func runApproach(opts options, ap approach) ([]row, error) {
	addr := net.JoinHostPort(opts.host, strconv.Itoa(ap.port))

	// Fail fast if the port is already in use (usually a stale server on
	// Windows): otherwise our server exits at once and we measure nothing.
	if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		_ = c.Close()
		return nil, fmt.Errorf("%s is already in use; stop the stale server (scripts/kill-servers.ps1)", addr)
	}

	logPath := filepath.Join(opts.outDir, "logs", ap.name+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	serverArgs := append([]string{"-addr", addr}, ap.extraArgs...)
	srv := exec.Command(filepath.Join(opts.binDir, ap.serverBin+binExt()), serverArgs...)
	srv.Stdout = logFile
	srv.Stderr = logFile
	if err := srv.Start(); err != nil {
		return nil, fmt.Errorf("start server: %w", err)
	}
	defer func() {
		_ = srv.Process.Kill()
		_, _ = srv.Process.Wait()
	}()

	if err := waitForPort(addr, 10*time.Second); err != nil {
		return nil, fmt.Errorf("server not ready (%s): %w", logPath, err)
	}
	log.Printf("[%s] server up on %s (pid %d)", ap.name, addr, srv.Process.Pid)

	// messages x concurrency matrix, plus a 500-client point for async (req 2.7, 4.5).
	type cell struct{ messages, concurrency int }
	var grid []cell
	for _, m := range opts.messages {
		for _, c := range opts.concurrency {
			grid = append(grid, cell{m, c})
		}
	}
	if ap.name == "async" && opts.async500 {
		grid = append(grid, cell{100, 500})
	}

	if opts.warmup {
		log.Printf("[%s] warm-up", ap.name)
		_, _ = runClient(opts, ap, addr, 50, 4)
	}

	var rows []row
	for _, cl := range grid {
		for rep := 1; rep <= opts.repeat; rep++ {
			r, err := runCell(opts, ap, addr, srv.Process.Pid, cl.messages, cl.concurrency, rep)
			if err != nil {
				return nil, fmt.Errorf("cell m=%d c=%d: %w", cl.messages, cl.concurrency, err)
			}
			rows = append(rows, r)
			log.Printf("[%s] m=%-4d c=%-3d rep=%d  mean=%.3fms p95=%.3fms thr=%.0f/s cpu=%.0f%% rss=%.0fMB",
				ap.name, cl.messages, cl.concurrency, rep,
				r.MeanMS, r.P95MS, r.ThroughputHz, r.ServerPeakCPU, r.ServerPeakRSSMB)
		}
	}
	return rows, nil
}

// runCell runs one client invocation while sampling the server process.
func runCell(opts options, ap approach, addr string, serverPID, messages, concurrency, rep int) (row, error) {
	sampler, err := procstat.New(serverPID, 50*time.Millisecond)
	if err != nil {
		return row{}, fmt.Errorf("procstat: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan procstat.Result, 1)
	go func() { done <- sampler.Run(ctx) }()

	rep0 := time.Now()
	report, err := runClient(opts, ap, addr, messages, concurrency)
	clientWall := time.Since(rep0)
	cancel()
	res := <-done
	if err != nil {
		return row{}, err
	}

	return row{
		Approach:        ap.name,
		Messages:        messages,
		Concurrency:     concurrency,
		TotalMessages:   messages * concurrency,
		Repeat:          rep,
		MeanMS:          report.MeanMS,
		MinMS:           report.MinMS,
		MaxMS:           report.MaxMS,
		P95MS:           report.P95MS,
		TotalMS:         report.TotalMS,
		ThroughputHz:    report.ThroughputHz,
		ClientWallMS:    float64(clientWall) / float64(time.Millisecond),
		ServerPeakCPU:   res.PeakCPUPerc,
		ServerMeanCPU:   res.MeanCPUPerc,
		ServerPeakRSSMB: res.PeakRSSMB,
	}, nil
}

// runClient invokes the load client as a subprocess and parses its JSON line.
func runClient(opts options, ap approach, addr string, messages, concurrency int) (metrics.Report, error) {
	args := []string{
		"-addr", addr,
		"-messages", strconv.Itoa(messages),
		ap.concFlag, strconv.Itoa(concurrency),
		"-json", "-quiet",
	}
	cmd := exec.Command(filepath.Join(opts.binDir, ap.clientBin+binExt()), args...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return metrics.Report{}, fmt.Errorf("client failed: %s: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return metrics.Report{}, err
	}

	var rep metrics.Report
	if err := json.Unmarshal(lastJSONLine(out), &rep); err != nil {
		return metrics.Report{}, fmt.Errorf("parse client output %q: %w", string(out), err)
	}
	return rep, nil
}

// row is one line of the output CSV.
type row struct {
	Approach        string
	Messages        int
	Concurrency     int
	TotalMessages   int
	Repeat          int
	MeanMS          float64
	MinMS           float64
	MaxMS           float64
	P95MS           float64
	TotalMS         float64
	ThroughputHz    float64
	ClientWallMS    float64
	ServerPeakCPU   float64
	ServerMeanCPU   float64
	ServerPeakRSSMB float64
}

var csvHeader = []string{
	"approach", "messages", "concurrency", "total_messages", "repeat",
	"mean_ms", "min_ms", "max_ms", "p95_ms", "total_ms",
	"throughput_msg_per_s", "client_wall_ms",
	"server_peak_cpu_pct", "server_mean_cpu_pct", "server_peak_rss_mb",
}

func (r row) record() []string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
	return []string{
		r.Approach,
		strconv.Itoa(r.Messages),
		strconv.Itoa(r.Concurrency),
		strconv.Itoa(r.TotalMessages),
		strconv.Itoa(r.Repeat),
		f(r.MeanMS), f(r.MinMS), f(r.MaxMS), f(r.P95MS), f(r.TotalMS),
		f(r.ThroughputHz), f(r.ClientWallMS),
		f(r.ServerPeakCPU), f(r.ServerMeanCPU), f(r.ServerPeakRSSMB),
	}
}

func writeCSV(path string, rows []row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write(csvHeader); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write(r.record()); err != nil {
			return err
		}
	}
	return w.Error()
}

func printTable(rows []row) {
	fmt.Printf("\n%-10s %8s %6s %10s %10s %12s %10s %9s\n",
		"approach", "messages", "conc", "mean_ms", "p95_ms", "thr_msg/s", "cpu_%", "rss_MB")
	for _, r := range rows {
		fmt.Printf("%-10s %8d %6d %10.3f %10.3f %12.0f %10.0f %9.0f\n",
			r.Approach, r.Messages, r.Concurrency, r.MeanMS, r.P95MS, r.ThroughputHz, r.ServerPeakCPU, r.ServerPeakRSSMB)
	}
}

// --- helpers ---------------------------------------------------------------

func waitForPort(addr string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for {
		c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func lastJSONLine(b []byte) []byte {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		s := strings.TrimSpace(lines[i])
		if strings.HasPrefix(s, "{") {
			return []byte(s)
		}
	}
	return b
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitCSVInt(s string) []int {
	var out []int
	for _, p := range splitCSV(s) {
		n, err := strconv.Atoi(p)
		if err != nil {
			log.Fatalf("benchmark: invalid integer %q in list", p)
		}
		out = append(out, n)
	}
	return out
}

func binExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
