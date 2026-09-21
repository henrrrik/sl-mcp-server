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

	// Catalogs and the deviations snapshot are served from memory between
	// upstream refreshes; real-time endpoints always go upstream.
	client := slclient.NewCachingClient(slclient.NewClient(), slclient.SLCacheRules)
	mcpServer := NewSLServer(client)

	srv, shutdown := newHTTPServer(":"+port, mcpServer)

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
func newHTTPServer(addr string, mcpServer *server.MCPServer) (*http.Server, func(context.Context) error) {
	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
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
	mux.Handle("/mcp", streamable)
	mux.Handle("/", sseServer)

	return srv, sseServer.Shutdown
}
