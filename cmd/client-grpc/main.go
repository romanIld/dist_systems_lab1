// Command client-grpc is the load client for the part 1.3 streaming RPC server.
// Each of -streams bidirectional streams has a sender goroutine pushing
// "Hello X" messages with increasing seq and a receiver goroutine checking
// order and recording timings. It reports the total streaming time (req 3.9)
// and per-message RTT (Send(seq) -> Recv(seq)); under streaming these RTTs
// include pipeline queueing delay.
//
// Usage: client-grpc -addr host:port -messages 1000 -streams 10 [-json]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"lab1/internal/echopb"
	"lab1/internal/hrtime"
	"lab1/internal/metrics"
)

type config struct {
	addr     string
	messages int
	streams  int
	timeout  time.Duration
	jsonOut  bool
	quiet    bool
}

func main() {
	var cfg config
	flag.StringVar(&cfg.addr, "addr", "127.0.0.1:9103", "server address host:port")
	flag.IntVar(&cfg.messages, "messages", 1000, "messages sent per stream")
	flag.IntVar(&cfg.streams, "streams", 1, "number of concurrent bidirectional streams")
	flag.DurationVar(&cfg.timeout, "timeout", 60*time.Second, "overall deadline for a stream")
	flag.BoolVar(&cfg.jsonOut, "json", false, "print the result as a single JSON line only")
	flag.BoolVar(&cfg.quiet, "quiet", false, "suppress progress logging")
	flag.Parse()

	if cfg.messages <= 0 || cfg.streams <= 0 {
		log.Fatal("client-grpc: -messages and -streams must be positive")
	}
	if cfg.quiet || cfg.jsonOut {
		log.SetOutput(os.Stderr)
	}

	summary, err := run(cfg)
	if err != nil {
		log.Fatalf("client-grpc: %v", err)
	}

	label := fmt.Sprintf("grpc streams=%d n=%d", cfg.streams, cfg.messages)
	if cfg.jsonOut {
		fmt.Println(summary.Report(label).JSON())
		return
	}
	fmt.Println(summary.String())
}

func run(cfg config) (metrics.Summary, error) {
	conn, err := grpc.NewClient(cfg.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return metrics.Summary{}, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	client := echopb.NewEchoServiceClient(conn)

	if !cfg.quiet {
		log.Printf("opening %d streams to %s, %d messages each", cfg.streams, cfg.addr, cfg.messages)
	}

	collectors := make([]*metrics.Collector, cfg.streams)
	var firstErr error
	var errOnce sync.Once
	var wg sync.WaitGroup

	start := hrtime.Now()
	for i := 0; i < cfg.streams; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			col := metrics.NewCollector(cfg.messages)
			collectors[idx] = col
			if err := runStream(client, cfg, col); err != nil {
				errOnce.Do(func() { firstErr = err })
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Duration(hrtime.Now() - start)

	if firstErr != nil {
		return metrics.Summary{}, firstErr
	}

	merged := metrics.NewCollector(cfg.streams * cfg.messages)
	for _, col := range collectors {
		merged.Merge(col)
	}
	return merged.Summarize(elapsed), nil
}

// pending pairs a sequence number with its send time (hrtime nanoseconds).
type pending struct {
	seq uint64
	at  int64
}

// runStream sends cfg.messages requests on one stream and consumes the matching
// responses, checking seq order (req 3.5) and recording each RTT.
func runStream(client echopb.EchoServiceClient, cfg config, col *metrics.Collector) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	stream, err := client.EchoStream(ctx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}

	// Buffered so the sender never blocks; also the happens-before edge for
	// each send timestamp.
	pendCh := make(chan pending, cfg.messages)
	sendErr := make(chan error, 1)

	go func() {
		for i := 0; i < cfg.messages; i++ {
			p := pending{seq: uint64(i), at: hrtime.Now()}
			err := stream.Send(&echopb.EchoRequest{
				Seq:     p.seq,
				Payload: fmt.Sprintf("Hello %d", i),
			})
			if err != nil {
				sendErr <- fmt.Errorf("send seq %d: %w", i, err)
				close(pendCh)
				return
			}
			pendCh <- p
		}
		close(pendCh)
		sendErr <- stream.CloseSend()
	}()

	var received int
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("recv after %d msgs: %w", received, err)
		}

		p, ok := <-pendCh
		if !ok {
			return fmt.Errorf("received more responses than requests (%d)", received+1)
		}
		if resp.GetSeq() != p.seq {
			return fmt.Errorf("out-of-order response: got seq %d, want %d", resp.GetSeq(), p.seq)
		}
		if want := fmt.Sprintf("ECHO: Hello %d", p.seq); resp.GetPayload() != want {
			return fmt.Errorf("seq %d: payload = %q, want %q", p.seq, resp.GetPayload(), want)
		}
		if resp.GetServerTime() == nil {
			return fmt.Errorf("seq %d: missing server_time field", p.seq)
		}
		col.Add(time.Duration(hrtime.Now() - p.at))
		received++
	}

	if err := <-sendErr; err != nil {
		return err
	}
	if received != cfg.messages {
		return fmt.Errorf("received %d responses, want %d", received, cfg.messages)
	}
	return nil
}
