package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/henrrrik/sl-mcp-server/slclient"
)

// startTestServer serves the real HTTP wiring on a random loopback port.
// Access log lines go to the returned buffer.
func startTestServer(t *testing.T) (base string, shutdown func(context.Context) error) {
	base, shutdown, _ = startTestServerWithLog(t)
	return base, shutdown
}

func startTestServerWithLog(t *testing.T) (base string, shutdown func(context.Context) error, logs *bytes.Buffer) {
	t.Helper()
	logs = &bytes.Buffer{}
	srv, shutdown := newHTTPServer("127.0.0.1:0", NewSLServer(slclient.NewClient()), log.New(logs, "", 0))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), shutdown, logs
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
	defer func() { _ = shutdown(context.Background()) }()

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
	defer func() { _ = shutdown(context.Background()) }()

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
	srv, _ := newHTTPServer("127.0.0.1:0", NewSLServer(slclient.NewClient()), log.New(io.Discard, "", 0))
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

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`

func postMCP(t *testing.T, url string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(initializeBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// Clients and UIs normalise URLs differently; a trailing slash on the MCP
// endpoint must not turn into a 404 that a connector then misreads as
// "needs sign-in".
func TestStreamableHTTP_TrailingSlashAccepted(t *testing.T) {
	base, shutdown := startTestServer(t)
	defer func() { _ = shutdown(context.Background()) }()
	resp := postMCP(t, base+"/mcp/")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST /mcp/ should be served like /mcp, got %d", resp.StatusCode)
	}
}

// Some clients probe the endpoint with HEAD before connecting.
func TestStreamableHTTP_HeadIsOK(t *testing.T) {
	base, shutdown := startTestServer(t)
	defer func() { _ = shutdown(context.Background()) }()
	resp, err := http.Head(base + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("HEAD /mcp should be 200, got %d", resp.StatusCode)
	}
}

// Every request is logged with method, path, status and user agent — but
// never the query string, which carries the SSE session id.
func TestHTTPServer_AccessLog(t *testing.T) {
	base, shutdown, logs := startTestServerWithLog(t)
	defer func() { _ = shutdown(context.Background()) }()

	req, _ := http.NewRequest(http.MethodGet, base+"/does-not-exist?sessionId=secret", nil)
	req.Header.Set("User-Agent", "probe/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	line := logs.String()
	for _, want := range []string{"method=GET", "path=/does-not-exist", "status=404", `ua="probe/1.0"`} {
		if !strings.Contains(line, want) {
			t.Errorf("access log should contain %q, got: %s", want, line)
		}
	}
	if strings.Contains(line, "secret") {
		t.Errorf("access log must not contain the query string, got: %s", line)
	}
}

// Claude's connector client speaks Streamable HTTP first: it POSTs
// initialize to whatever URL it was given. A connector configured with the
// older /sse URL therefore hit a 405, which the client misread as "needs
// sign-in" and failed at OAuth registration. An SSE-transport client never
// POSTs to /sse (it posts to /message), so a POST there is unambiguously a
// Streamable HTTP client and is served as one.
func TestStreamableHTTP_PostToSSEPathIsServed(t *testing.T) {
	base, shutdown := startTestServer(t)
	defer func() { _ = shutdown(context.Background()) }()
	for _, path := range []string{"/sse", "/sse/"} {
		resp := postMCP(t, base+path)
		var out struct {
			Result struct {
				ServerInfo struct {
					Name string `json:"name"`
				} `json:"serverInfo"`
			} `json:"result"`
		}
		err := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || err != nil || out.Result.ServerInfo.Name != "sl-mcp-server" {
			t.Errorf("POST %s initialize: status %d err %v result %+v", path, resp.StatusCode, err, out)
		}
	}
}

// GET /sse must remain the SSE transport.
func TestSSE_GetStillOpensStream(t *testing.T) {
	base, shutdown := startTestServer(t)
	defer func() { _ = shutdown(context.Background()) }()
	resp, err := http.Get(base + "/sse")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("GET /sse should open an event stream, got %d %s", resp.StatusCode, ct)
	}
}
