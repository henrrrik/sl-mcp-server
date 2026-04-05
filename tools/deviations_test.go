package tools

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
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
