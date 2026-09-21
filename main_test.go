package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/henrrrik/sl-mcp-server/slclient"
)

// startTestServer serves the real HTTP wiring on a random loopback port.
func startTestServer(t *testing.T) (base string, shutdown func(context.Context) error) {
	t.Helper()
	srv, shutdown := newHTTPServer("127.0.0.1:0", NewSLServer(slclient.NewClient()))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), shutdown
}

// A connected SSE client must not stall graceful shutdown: the stream has
// to be closed server-side so http.Server.Shutdown can complete promptly,
// instead of waiting out the whole deadline and cutting the stream.
func TestShutdown_CompletesPromptlyWithSSEClientConnected(t *testing.T) {
	base, shutdown := startTestServer(t)

	resp, err := http.Get(base + "/sse")
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	defer resp.Body.Close()
	// Wait for the endpoint event so the session is registered.
	r := bufio.NewReader(resp.Body)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading SSE stream: %v", err)
		}
		if strings.HasPrefix(line, "event: endpoint") {
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err = shutdown(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("shutdown returned %v after %s", err, elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("shutdown took %s with one SSE client connected; expected the stream to be closed server-side", elapsed)
	}
}

// The Streamable HTTP transport is served at /mcp so clients that don't
// need a long-lived stream survive restarts and multiple replicas.
func TestStreamableHTTP_InitializeAtMCPEndpoint(t *testing.T) {
	base, shutdown := startTestServer(t)
	defer shutdown(context.Background())

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	req, _ := http.NewRequest(http.MethodPost, base+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /mcp initialize: status %d", resp.StatusCode)
	}
	var out struct {
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Result.ServerInfo.Name != "sl-mcp-server" {
		t.Errorf("unexpected initialize result: %+v", out)
	}
}

func TestRootEndpoint_AdvertisesBothTransports(t *testing.T) {
	base, shutdown := startTestServer(t)
	defer shutdown(context.Background())

	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["sse_endpoint"] != "/sse" || out["mcp_endpoint"] != "/mcp" {
		t.Errorf("root should advertise both transports, got %v", out)
	}
}

// A public, unauthenticated listener needs header and idle timeouts so a
// slow or idle peer can't hold connections open indefinitely. WriteTimeout
// must stay unset: SSE streams are long-lived by design.
func TestHTTPServer_HasHeaderAndIdleTimeouts(t *testing.T) {
	srv, _ := newHTTPServer("127.0.0.1:0", NewSLServer(slclient.NewClient()))
	if srv.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout must be set")
	}
	if srv.IdleTimeout <= 0 {
		t.Error("IdleTimeout must be set")
	}
	if srv.WriteTimeout != 0 {
		t.Error("WriteTimeout must stay 0 for SSE")
	}
}
