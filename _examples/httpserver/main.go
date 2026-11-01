// This example runs an HTTP server with an /echo endpoint wired together
// by gad. Try it with:
//
//	go run ./examples/httpserver
//	curl -X POST -d 'hello' http://localhost:8080/echo
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/roy2220/gad"
)

// RegisterHTTPServer registers an HTTP server that will begin serving requests
// when the application starts.
func RegisterHTTPServer(g *gad.G) {
	g.RootTask(func(g *gad.G) error {
		mux := g.Require[*http.ServeMux]("")

		if !g.Ready() {
			return nil
		}

		srv := &http.Server{Addr: ":8080", Handler: mux}
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			return err
		}

		fmt.Println("Starting HTTP server at", srv.Addr)
		go func() { _ = srv.Serve(ln) }()

		g.Cleanup(func() {
			fmt.Println("Stopping HTTP server at", srv.Addr)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(ctx)
		})
		return nil
	})
}

// RegisterServeMux provides a new ServeMux.
func RegisterServeMux(g *gad.G) {
	g.Task(func(g *gad.G) error {
		var mux *http.ServeMux
		g.Provide(&mux, "")

		if !g.Ready() {
			return nil
		}

		mux = http.NewServeMux()
		return nil
	})
}

// EchoHandler is an http.Handler that copies its request body back to the
// response.
type EchoHandler struct{}

// ServeHTTP handles an HTTP request to the /echo endpoint.
func (*EchoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := io.Copy(w, r.Body); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to handle request:", err)
	}
}

// RegisterEchoHandler registers the /echo route on the ServeMux.
func RegisterEchoHandler(g *gad.G) {
	g.Task(func(g *gad.G) error {
		mux := g.Decorate[*http.ServeMux]("")

		if !g.Ready() {
			return nil
		}

		(*mux).Handle("/echo", &EchoHandler{})
		return nil
	})
}

func main() {
	var g gad.G
	RegisterHTTPServer(&g)
	RegisterServeMux(&g)
	RegisterEchoHandler(&g)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := g.Start(ctx); err != nil {
		panic(err)
	}
	defer g.Stop()

	// Keep the main goroutine alive until a SIGINT or SIGTERM is received.
	<-ctx.Done()
}
