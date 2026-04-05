package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/henrrrik/sl-mcp-server/slclient"
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

	go func() {
		log.Printf("SL MCP server listening on :%s", port)
		if err := sseServer.Start(":" + port); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sseServer.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
