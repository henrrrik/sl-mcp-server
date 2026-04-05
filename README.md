# SL MCP Server

An [MCP](https://modelcontextprotocol.io/) server for Stockholm's public transit (SL). Gives AI assistants access to real-time departures, trip planning, deviations, and more via SL's open APIs.

Hosted on [Runway](https://www.runway.horse) at https://sl-mcp-server.pqapp.dev

## Tools

| Tool | Description |
|------|-------------|
| `departures` | Real-time departures from a stop |
| `trips` | Plan a trip between two locations |
| `stop_finder` | Search for stops by name |
| `deviations` | Traffic disruptions and deviations |
| `sites` | List all transit sites |
| `lines` | List all transit lines |
| `stop_points` | List stop points (platforms, quays) |
| `transport_authorities` | List transport authorities |
| `system_info` | Timetable validity period |

## Usage

No API key required — SL's integration APIs are open.

The hosted instance is available as an SSE MCP server at `https://sl-mcp-server.pqapp.dev/sse`.

### Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "sl": {
      "url": "https://sl-mcp-server.pqapp.dev/sse"
    }
  }
}
```

### Self-hosting

```sh
go build -o sl-mcp-server
PORT=5000 ./sl-mcp-server
```

## Development

```sh
go test -race ./...
go vet ./...
```

## License

MIT
