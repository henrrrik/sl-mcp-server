# SL MCP Server

An [MCP](https://modelcontextprotocol.io/) server for Stockholm's public transit (SL). Gives AI assistants access to real-time departures, trip planning, deviations, and more via SL's open APIs.

Hosted on [Runway](https://www.runway.horse) at https://sl-mcp-server.pqapp.dev

## Tools

| Tool | Description |
|------|-------------|
| `sl_departures` | Real-time departures from a stop |
| `sl_trips` | Plan a trip between two locations |
| `sl_stop_finder` | Search for stops by name |
| `sl_deviations` | Traffic disruptions and deviations |
| `sl_sites` | List all transit sites |
| `sl_lines` | List all transit lines |
| `sl_stop_points` | List stop points (platforms, quays) |
| `sl_transport_authorities` | List transport authorities |
| `sl_system_info` | Timetable validity period |

## Setup

No API key required — SL's integration APIs are open.

```sh
go build -o sl-mcp-server
PORT=5000 ./sl-mcp-server
```

The server runs as an SSE (Server-Sent Events) MCP transport on the specified port (default 5000).

### Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "sl": {
      "command": "/path/to/sl-mcp-server"
    }
  }
}
```

## Development

```sh
go test -race ./...
go vet ./...
```

## License

MIT
