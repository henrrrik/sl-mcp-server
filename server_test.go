package main

import (
	"testing"

	"github.com/henrrrik/sl-mcp-server/slclient"
)

func TestNewSLServer(t *testing.T) {
	client := slclient.NewClient()
	s := NewSLServer(client)
	if s == nil {
		t.Fatal("NewSLServer returned nil")
	}
}
