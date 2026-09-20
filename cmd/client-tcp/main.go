// Command client-tcp is the load client for the text-protocol echo servers
// (parts 1.1 and 1.2). With -concurrency C it runs C parallel connections, each
// sending -messages "Hello X\n" messages, measures per-message RTT and prints
// aggregate statistics (req 1.6-1.10, 2.5-2.7).
//
// Usage: client-tcp -addr host:port -messages 1000 -concurrency 100 [-json]
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"lab1/internal/hrtime"
	"lab1/internal/metrics"
	"lab1/internal/protocol"
)

type config struct {
	addr        string
	messages    int
	concurrency int
	timeout     time.Duration
	connectWait time.Duration
	jsonOut     bool
	quiet       bool
}

func main() {
	var cfg config
	flag.StringVar(&cfg.addr, "addr", "127.0.0.1:9101", "server address host:port")
	flag.IntVar(&cfg.messages, "messages", 1000, "messages sent per connection")
	flag.IntVar(&cfg.concurrency, "concurrency", 1, "number of parallel connections")
	flag.DurationVar(&cfg.timeout, "timeout", 10*time.Second, "per-message I/O timeout")
	flag.DurationVar(&cfg.connectWait, "connect-wait", 5*time.Second, "how long to keep retrying the initial dial")
	flag.BoolVar(&cfg.jsonOut, "json", false, "print the result as a single JSON line only")
	flag.BoolVar(&cfg.quiet, "quiet", false, "suppress progress logging")
	flag.Parse()

	if cfg.messages <= 0 || cfg.concurrency <= 0 {
		log.Fatal("client-tcp: -messages and -concurrency must be positive")
	}
	if cfg.quiet || cfg.jsonOut {
		log.SetOutput(os.Stderr)
	}

	summary, err := run(cfg)
	if err != nil {
		log.Fatalf("client-tcp: %v", err)
	}

	label := fmt.Sprintf("threading/async c=%d n=%d", cfg.concurrency, cfg.messages)
	if cfg.jsonOut {
		fmt.Println(summary.Report(label).JSON())
		return
	}
	fmt.Println(summary.String())
}

// run dials all connections, then drives the send/receive loops in parallel and
// returns the merged statistics. The timer starts after connection setup so
// throughput reflects steady-state exchange, not TCP handshakes.
func run(cfg config) (metrics.Summary, error) {
	conns := make([]net.Conn, cfg.concurrency)
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	for i := range conns {
		c, err := dialWithRetry(dialer, cfg.addr, cfg.connectWait)
		if err != nil {
			closeAll(conns[:i])
			return metrics.Summary{}, fmt.Errorf("dial %d/%d: %w", i+1, cfg.concurrency, err)
		}
		if tcp, ok := c.(*net.TCPConn); ok {
			_ = tcp.SetNoDelay(true)
		}
		conns[i] = c
	}
	defer closeAll(conns)

	if !cfg.quiet {
		log.Printf("connected %d clients to %s, sending %d messages each", cfg.concurrency, cfg.addr, cfg.messages)
	}

	collectors := make([]*metrics.Collector, cfg.concurrency)
	var firstErr error
	var errOnce sync.Once
	var wg sync.WaitGroup

	start := hrtime.Now()
	for i, c := range conns {
		wg.Add(1)
		go func(idx int, conn net.Conn) {
			defer wg.Done()
			col := metrics.NewCollector(cfg.messages)
			collectors[idx] = col
			if err := driveConn(conn, cfg, col); err != nil {
				errOnce.Do(func() { firstErr = err })
			}
		}(i, c)
	}
	wg.Wait()
	elapsed := time.Duration(hrtime.Now() - start)

	if firstErr != nil {
		return metrics.Summary{}, firstErr
	}

	merged := metrics.NewCollector(cfg.concurrency * cfg.messages)
	for _, col := range collectors {
		merged.Merge(col)
	}
	return merged.Summarize(elapsed), nil
}

// driveConn sends cfg.messages "Hello X\n" messages on conn, one at a time,
// recording the RTT of each into col.
func driveConn(conn net.Conn, cfg config, col *metrics.Collector) error {
	reader := bufio.NewReaderSize(conn, protocol.MaxLineBytes)

	for i := 0; i < cfg.messages; i++ {
		req := fmt.Sprintf("Hello %d\n", i)

		_ = conn.SetDeadline(time.Now().Add(cfg.timeout))

		t0 := hrtime.Now()
		if _, err := conn.Write([]byte(req)); err != nil {
			return fmt.Errorf("write msg %d: %w", i, err)
		}
		line, err := reader.ReadString(protocol.Delimiter)
		if err != nil {
			return fmt.Errorf("read reply %d: %w", i, err)
		}
		rtt := time.Duration(hrtime.Now() - t0)

		payload, _, err := protocol.ParseResponse(line)
		if err != nil {
			return fmt.Errorf("reply %d: %w", i, err)
		}
		if want := fmt.Sprintf("Hello %d", i); payload != want {
			return fmt.Errorf("reply %d: payload = %q, want %q", i, payload, want)
		}
		col.Add(rtt)
	}
	return nil
}

func dialWithRetry(d *net.Dialer, addr string, budget time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(budget)
	var lastErr error
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, err := d.DialContext(ctx, "tcp", addr)
		cancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func closeAll(conns []net.Conn) {
	for _, c := range conns {
		if c != nil {
			_ = c.Close()
		}
	}
}
