package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "time/tzdata" // embed tzdata so Europe/Stockholm resolves inside minimal containers

	"github.com/henrrrik/sl-mcp-server/slclient"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}

	// Transient upstream failures (connection errors, 429, 5xx) get one
	// retry; catalogs and the deviations snapshot are then served from
	// memory between refreshes. Real-time endpoints always go upstream.
	client := slclient.NewCachingClient(
		slclient.NewRetryingClient(slclient.NewClient(), 2, 250*time.Millisecond),
		slclient.SLCacheRules,
	)
	mcpServer := NewSLServer(client)

	srv, shutdown := newHTTPServer(":"+port, mcpServer, log.Default())

	go func() {
		log.Printf("SL MCP server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

// newHTTPServer wires the MCP server behind both transports and returns the
// listener together with the shutdown func to use instead of srv.Shutdown.
//
// SSE at /sse (+ /message) is the original transport; it is stateful, so a
// restart drops every session and, with more than one replica, a client's
// GET /sse and POST /message can land on different processes. Streamable
// HTTP at /mcp is served stateless so clients using it survive both.
func newHTTPServer(addr string, mcpServer *server.MCPServer, logger *log.Logger) (*http.Server, func(context.Context) error) {
	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    addr,
		Handler: withAccessLog(logger, mux),
		// A public, unauthenticated listener: bound header reads and idle
		// keep-alives. WriteTimeout stays 0 because SSE streams are long-lived.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	// WithHTTPServer lets sseServer.Shutdown close every session's stream
	// before shutting the listener down; without it, Shutdown is a no-op
	// and http.Server.Shutdown waits out its deadline on the open streams.
	sseServer := server.NewSSEServer(mcpServer,
		server.WithKeepAlive(true),
		server.WithHTTPServer(srv),
	)
	streamable := server.NewStreamableHTTPServer(mcpServer,
		server.WithStateLess(true),
	)

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"sl-mcp-server","sse_endpoint":"/sse","mcp_endpoint":"/mcp"}`))
	})
	// Tolerate a trailing slash and HEAD probes: connector UIs normalise
	// URLs differently, and a 404 on the first request is easily misread
	// by a client as "this server needs sign-in".
	mcp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		streamable.ServeHTTP(w, r)
	})
	mux.Handle("/mcp", mcp)
	mux.Handle("/mcp/", mcp)
	mux.Handle("/", sseServer)

	return srv, sseServer.Shutdown
}

// withAccessLog logs one line per request: method, path, status, duration
// and user agent. The query string is deliberately omitted — it carries
// the SSE session id. The status recorder keeps http.Flusher so SSE and
// Streamable HTTP streams still flush through it.
func withAccessLog(logger *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		logger.Printf("http method=%s path=%s status=%d duration=%s ua=%q",
			r.Method, r.URL.Path, rec.status(), time.Since(start).Round(time.Millisecond), r.UserAgent())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.code == 0 {
		s.code = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.code == 0 {
		s.code = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if s.code == 0 {
		s.code = http.StatusOK
	}
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) status() int {
	if s.code == 0 {
		return http.StatusOK
	}
	return s.code
}
