// Command server-async is the part 1.2 echo server: event-driven and
// non-blocking, using only the standard library.
//
// A fixed pool of -loops goroutines (default = NumCPU) serves every connection.
// Each loop owns a set of connections and cycles over them doing non-blocking
// reads (a read deadline of "now" makes Read return immediately when no data is
// ready). There is no goroutine per connection (req 2.1, 2.2). A loop sleeps
// with a short backoff only when a full pass moved no data, so latency stays low
// under load and idle CPU stays small. Protocol is the same as part 1.1
// (lab1/internal/protocol).
//
// Usage: server-async -addr :9102 [-loops 8]
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"lab1/internal/protocol"
)

const (
	// pollTimeout bounds how long a single Read waits for a quiet connection.
	// A connection that already has data returns immediately regardless, so
	// under load this never fires; it only paces truly idle connections.
	pollTimeout  = 200 * time.Microsecond
	minIdleSleep = 250 * time.Microsecond
	maxIdleSleep = 2 * time.Millisecond
	readChunk    = 16 * 1024
)

func main() {
	addr := flag.String("addr", ":9102", "TCP address to listen on (host:port)")
	loops := flag.Int("loops", runtime.NumCPU(), "number of event-loop goroutines serving all connections")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if *loops < 1 {
		*loops = 1
	}
	if err := run(*addr, *loops); err != nil {
		log.Fatalf("server-async: %v", err)
	}
}

func run(addr string, nLoops int) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("listening on %s (event-driven, loops=%d)", ln.Addr(), nLoops)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Print("shutdown signal received, closing listener")
		_ = ln.Close()
	}()

	var wg sync.WaitGroup
	loops := make([]*loop, nLoops)
	for i := range loops {
		loops[i] = newLoop()
		wg.Add(1)
		go func(l *loop) {
			defer wg.Done()
			l.serve(ctx)
		}(loops[i])
	}

	// Accept loop: hand each new connection to a loop, round-robin.
	next := 0
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}
			log.Printf("accept error: %v", err)
			time.Sleep(10 * time.Millisecond)
			continue
		}
		loops[next%nLoops].add(conn)
		next++
	}

	wg.Wait()
	log.Print("stopped")
	return nil
}

// loop is one event-loop goroutine: it owns a set of connections and services
// them with non-blocking reads.
type loop struct {
	incoming chan net.Conn
	conns    []*conn
}

func newLoop() *loop {
	return &loop{incoming: make(chan net.Conn, 128)}
}

func (l *loop) add(c net.Conn) { l.incoming <- c }

func (l *loop) serve(ctx context.Context) {
	sleep := minIdleSleep
	for {
		select {
		case <-ctx.Done():
			for _, c := range l.conns {
				_ = c.nc.Close()
			}
			return
		default:
		}

		// Register any newly accepted connections without blocking.
		for {
			select {
			case nc := <-l.incoming:
				l.conns = append(l.conns, newConn(nc))
			default:
				goto drained
			}
		}
	drained:

		active := false
		for i := 0; i < len(l.conns); {
			c := l.conns[i]
			n, err := c.pump()
			if n > 0 {
				active = true
			}
			if err != nil {
				c.logClose(err)
				_ = c.nc.Close()
				l.conns[i] = l.conns[len(l.conns)-1]
				l.conns = l.conns[:len(l.conns)-1]
				continue
			}
			i++
		}

		if active {
			sleep = minIdleSleep
			continue
		}
		time.Sleep(sleep)
		if sleep < maxIdleSleep {
			sleep *= 2
		}
	}
}

// conn is a connection plus the parser state for partial lines.
type conn struct {
	nc      net.Conn
	scratch []byte
	acc     bytes.Buffer
}

func newConn(nc net.Conn) *conn {
	if tcp, ok := nc.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}
	return &conn{nc: nc, scratch: make([]byte, readChunk)}
}

// pump does one short read and echoes every complete line it produced. It
// returns the number of bytes read and a non-nil error when the connection
// should be dropped.
func (c *conn) pump() (int, error) {
	_ = c.nc.SetReadDeadline(time.Now().Add(pollTimeout))
	n, err := c.nc.Read(c.scratch)
	if n > 0 {
		c.acc.Write(c.scratch[:n])
		if c.acc.Len() > protocol.MaxLineBytes {
			return n, errors.New("frame exceeds limit")
		}
		if werr := c.flushLines(); werr != nil {
			return n, werr
		}
	}
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return n, nil // no data ready, not an error
		}
		return n, err
	}
	return n, nil
}

func (c *conn) flushLines() error {
	var out bytes.Buffer
	for {
		data := c.acc.Bytes()
		i := bytes.IndexByte(data, protocol.Delimiter)
		if i < 0 {
			break
		}
		payload := string(data[:i])
		c.acc.Next(i + 1)
		out.WriteString(protocol.BuildResponse(payload, time.Now()))
	}
	if out.Len() == 0 {
		return nil
	}
	_ = c.nc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := c.nc.Write(out.Bytes())
	return err
}

func (c *conn) logClose(err error) {
	if errors.Is(err, io.EOF) {
		return
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return
	}
	log.Printf("close %s: %v", c.nc.RemoteAddr(), err)
}
