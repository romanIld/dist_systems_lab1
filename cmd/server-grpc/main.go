// Command server-grpc is the part 1.3 echo server: one bidirectional-streaming
// method, EchoStream, over gRPC + Protobuf. The server echoes the client
// sequence number back (req 3.5) and returns its time in a structured Timestamp
// field, not text (req 3.4). Contract: proto/echo.proto -> internal/echopb.
//
// Usage: server-grpc -addr :9103
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"lab1/internal/echopb"
)

func main() {
	addr := flag.String("addr", ":9103", "TCP address to listen on (host:port)")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("server-grpc: listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	echopb.RegisterEchoServiceServer(grpcServer, &echoService{})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Print("shutdown signal received, stopping gRPC server")
		grpcServer.GracefulStop()
	}()

	log.Printf("listening on %s (gRPC bidirectional streaming)", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		log.Fatalf("server-grpc: serve: %v", err)
	}
	log.Print("stopped")
}

// echoService implements echopb.EchoServiceServer.
type echoService struct {
	echopb.UnimplementedEchoServiceServer
}

// EchoStream sends one echo response per received request until the client
// closes its side (io.EOF) or the stream fails.
func (echoService) EchoStream(stream grpc.BidiStreamingServer[echopb.EchoRequest, echopb.EchoResponse]) error {
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		resp := &echopb.EchoResponse{
			Seq:        req.GetSeq(),
			Payload:    "ECHO: " + req.GetPayload(),
			ServerTime: timestamppb.New(time.Now()),
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}
