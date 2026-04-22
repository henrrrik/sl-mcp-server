package tools

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

type mockHTTPDoer struct {
	response *http.Response
	lastReq  *http.Request
}

func (m *mockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	m.lastReq = req
	return m.response, nil
}

func newMockDoer(body string) *mockHTTPDoer {
	return newMockDoerWithStatus(body, 200)
}

func newMockDoerWithStatus(body string, status int) *mockHTTPDoer {
	return &mockHTTPDoer{
		response: &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
		},
	}
}

// routedMock dispatches responses by path-substring and records every call.
// Each Do() builds a fresh Response so the mock can handle multiple calls in
// one test — needed by the trips ambiguity flow, which fetches /v2/trips and
// then one or two /v2/stop-finder calls.
type routedMock struct {
	routes []mockRoute
	mu     sync.Mutex
	calls  []*http.Request
}

type mockRoute struct {
	pathContains string
	queryMatches map[string]string // optional: require these query params to match
	body         string
	status       int
}

func (m *routedMock) Do(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.mu.Unlock()
	for _, r := range m.routes {
		if !strings.Contains(req.URL.Path, r.pathContains) {
			continue
		}
		ok := true
		for k, v := range r.queryMatches {
			if req.URL.Query().Get(k) != v {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		status := r.status
		if status == 0 {
			status = 200
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(r.body)),
		}, nil
	}
	return &http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(strings.NewReader(`{"error":"no route in test mock for ` + req.URL.String() + `"}`)),
	}, nil
}

func loadTestData(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDeviationsTool_NoParams(t *testing.T) {
	body := loadTestData(t, "deviations.json")
	mock := newMockDoer(body)

	_, handler := DeviationsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Fatal("expected non-error result")
	}

	// Verify the URL was correct
	if mock.lastReq.URL.String() != "https://deviations.integration.sl.se/v1/messages" {
		t.Errorf("unexpected URL: %s", mock.lastReq.URL.String())
	}

	// Verify result contains the fixture data
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "Avstängd hållplats") {
		t.Error("result should contain fixture data")
	}
}

func TestFetchJSON_HTTPError(t *testing.T) {
	mock := newMockDoerWithStatus("not found", 404)

	_, handler := DeviationsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Fatal("expected error result for HTTP 404")
	}

	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "404") {
		t.Errorf("expected error to mention status code, got %q", text)
	}
}

func TestDeviationsTool_WithParams(t *testing.T) {
	body := loadTestData(t, "deviations.json")
	mock := newMockDoer(body)

	_, handler := DeviationsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"future":         true,
		"transport_mode": "BUS",
		"line":           float64(42),
	}

	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := mock.lastReq.URL.Query()
	if q.Get("future") != "true" {
		t.Errorf("expected future=true, got %q", q.Get("future"))
	}
	if q.Get("transport_mode") != "BUS" {
		t.Errorf("expected transport_mode=BUS, got %q", q.Get("transport_mode"))
	}
	if q.Get("line") != "42" {
		t.Errorf("expected line=42, got %q", q.Get("line"))
	}
}

// P1: site accepts all four id formats and normalizes to short form before
// passing to the upstream query.
func TestDeviationsTool_SiteAcceptsAllIDFormats(t *testing.T) {
	cases := []struct {
		name  string
		input any
	}{
		{"short number (backwards compat)", float64(9001)},
		{"short string", "9001"},
		{"18xx string", "18009001"},
		{"9-digit string", "300109001"},
		{"16-digit GID string", "9091001000009001"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := newMockDoer(`[]`)
			_, handler := DeviationsTool(mock)

			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{"site": tc.input}

			result, err := handler(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsError {
				t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
			}
			if got := mock.lastReq.URL.Query().Get("site"); got != "9001" {
				t.Errorf("expected site=9001 (normalized), got %q", got)
			}
		})
	}
}

// P1: invalid site input echoes as a string, never as scientific-notation
// number. 1e18 is too big to fit safely in a JSON number, so it should
// come back as a decimal string rather than "9.09e+15".
func TestDeviationsTool_InvalidSiteEchoesAsDecimalString(t *testing.T) {
	mock := newMockDoer(`[]`)
	_, handler := DeviationsTool(mock)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site": float64(1e18)}

	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Fatal("expected error for oversized input")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if strings.Contains(text, "e+") || strings.Contains(text, "e-") {
		t.Errorf("error echo must not use scientific notation, got %s", text)
	}
	if !strings.Contains(text, `"error":"invalid_site_id_format"`) {
		t.Errorf("expected invalid_site_id_format, got %s", text)
	}
}

// P1: a 16-digit GID passed to site should normalize to the short form
// without precision loss (so no "9.09e+15" — the exact input echoes when
// out-of-range).
func TestDeviationsTool_InvalidGIDStringEchoedVerbatim(t *testing.T) {
	mock := newMockDoer(`[]`)
	_, handler := DeviationsTool(mock)

	// All-zero tail: a valid GID shape but resolves outside range.
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"site": "9091001000000000"}

	result, _ := handler(context.Background(), req)
	if !result.IsError {
		t.Fatal("expected error for out-of-range GID")
	}
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, `"input":"9091001000000000"`) {
		t.Errorf("expected exact input echo, got %s", text)
	}
	if !strings.Contains(text, `"error":"site_id_out_of_range"`) {
		t.Errorf("expected site_id_out_of_range, got %s", text)
	}
}
