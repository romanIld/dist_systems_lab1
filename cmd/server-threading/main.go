// Command server-threading is the part 1.1 echo server: one goroutine per
// connection doing blocking reads/writes (thread-per-connection model).
// Protocol: see lab1/internal/protocol.
//
// Usage: server-threading -addr :9101 [-idle-timeout 2m] [-verbose]
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"lab1/internal/protocol"
)

func main() {
	addr := flag.String("addr", ":9101", "TCP address to listen on")
	idleTimeout := flag.Duration("idle-timeout", 2*time.Minute, "close a connection after this period with no data")
	verbose := flag.Bool("verbose", false, "log every accepted/closed connection")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	srv := &server{idleTimeout: *idleTimeout, verbose: *verbose}
	if err := srv.run(*addr); err != nil {
		log.Fatalf("server-threading: %v", err)
	}
}

type server struct {
	idleTimeout time.Duration
	verbose     bool

	active  int64 // live connections (atomic)
	handled int64 // total connections handled (atomic)
	wg      sync.WaitGroup
}

func (s *server) run(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("listening on %s (thread-per-connection, idle-timeout=%s)", ln.Addr(), s.idleTimeout)

	// Turn SIGINT/SIGTERM into a listener close so Accept unblocks.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Print("shutdown signal received, closing listener")
		_ = ln.Close()
	}()

	go s.reportLoop(ctx) // periodic active-connection count (req 1.3)

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
		s.wg.Add(1)
		go s.handleConn(conn)
	}

	log.Print("waiting for in-flight connections to drain")
	s.wg.Wait()
	log.Printf("stopped, handled %d connections in total", atomic.LoadInt64(&s.handled))
	return nil
}

func (s *server) reportLoop(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if a := atomic.LoadInt64(&s.active); a > 0 {
				log.Printf("connections: active=%d handled=%d", a, atomic.LoadInt64(&s.handled))
			}
		}
	}
}

// handleConn serves one connection until the peer closes it, an I/O error
// occurs, or the idle timeout fires.
func (s *server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	active := atomic.AddInt64(&s.active, 1)
	atomic.AddInt64(&s.handled, 1)
	defer atomic.AddInt64(&s.active, -1)

	if s.verbose {
		log.Printf("accept %s (active=%d)", conn.RemoteAddr(), active)
	}

	// Small request/response frames: latency over packing.
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}

	reader := bufio.NewReaderSize(conn, protocol.MaxLineBytes)
	writer := bufio.NewWriterSize(conn, protocol.MaxLineBytes)

	for {
		if s.idleTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(s.idleTimeout))
		}

		line, err := reader.ReadString(protocol.Delimiter)
		if err != nil {
			s.logReadError(conn, err)
			return
		}
		if len(line) > protocol.MaxLineBytes {
			log.Printf("drop %s: frame exceeds %d bytes", conn.RemoteAddr(), protocol.MaxLineBytes)
			return
		}

		payload := line[:len(line)-1] // strip trailing '\n'
		resp := protocol.BuildResponse(payload, time.Now())

		if s.idleTimeout > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(s.idleTimeout))
		}
		if _, err := writer.WriteString(resp); err != nil {
			log.Printf("write %s: %v", conn.RemoteAddr(), err)
			return
		}
		if err := writer.Flush(); err != nil {
			log.Printf("flush %s: %v", conn.RemoteAddr(), err)
			return
		}
	}
}

func (s *server) logReadError(conn net.Conn, err error) {
	switch {
	case errors.Is(err, io.EOF):
		if s.verbose {
			log.Printf("close %s (peer closed)", conn.RemoteAddr())
		}
	case errors.Is(err, net.ErrClosed):
		// connection closed during shutdown
	default:
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			log.Printf("close %s (idle timeout)", conn.RemoteAddr())
			return
		}
		log.Printf("read %s: %v", conn.RemoteAddr(), err)
	}
}
