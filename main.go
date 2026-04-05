package main

import (
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"sl-mcp-server/slclient"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}

	client := slclient.NewClient()
	mcpServer := NewSLServer(client)

	sseServer := server.NewSSEServer(mcpServer,
		server.WithKeepAlive(true),
	)

	log.Printf("SL MCP server listening on :%s", port)
	log.Fatal(sseServer.Start(":" + port))
}
