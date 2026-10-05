# Build stage
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sl-mcp-server .

# Final stage
FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/sl-mcp-server /usr/local/bin/sl-mcp-server
ENV PORT=5000
EXPOSE 5000
USER nobody
ENTRYPOINT ["sl-mcp-server"]
