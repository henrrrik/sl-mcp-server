# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

SL MCP Server is an MCP (Model Context Protocol) server proxy for SL (Stockholms Lokaltrafik) public transport APIs. It exposes 9 tools over SSE transport, proxying three SL REST APIs: Deviations, Journey Planner v2, and Transport.

Hosted on Runway at https://sl-mcp-server.pqapp.dev

## Build & Test
- `go test -v -race ./...` — run tests (matches CI)
- `go fmt ./...` — run before committing
- `go vet ./...` — run before committing

## Project Structure
- `slclient/` — `HTTPDoer` interface wrapping `http.Client` for testability, plus URL builder
- `tools/` — MCP tool definitions and handlers (deviations, transport, journeyplanner)
- `testdata/` — canned JSON fixtures for tests
- `server.go` — wires all tools into MCP server
- `main.go` — SSE transport entry point, listens on PORT env

## Key Patterns
- `HTTPDoer` interface is the testability seam — all tools accept it, tests inject a mock
- Each tool is a factory function returning `(mcp.Tool, server.ToolHandlerFunc)`
- `fetchJSON` is the shared helper for HTTP GET + return as text result
- Keep this pattern when adding new tools

## Workflow
- Use Red/Green TDD
- Create a PR for all changes — do not push directly to main
- CI runs tests on push and PR via GitHub Actions

## Deployment
- Deployed on Runway (EU PaaS) via `runway app-deploy`
- Go buildpack auto-detects and builds
- App listens on PORT env (default 5000)
