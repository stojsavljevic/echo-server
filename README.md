# Echo Server & OpenAPI PetStore Integration

A simple HTTP and gRPC echo server with WebSocket, Server-Sent Events (SSE), and an OpenAPI 3.0 PetStore API.

**Note:** This project is a fork of the [jmalloc/echo-server](https://github.com/jmalloc/echo-server) repository. It adds gRPC support and an OpenAPI 3.0-compliant PetStore API.

---

## Features

- HTTP echo (returns request details)  
- gRPC echo service  
- WebSocket echo  
- Server-Sent Events (SSE)  
- OpenAPI 3.0 PetStore API  
- Health check endpoint  

---

## Echo Server

A simple HTTP and gRPC echo server for testing HTTP proxies and clients. It echoes information about HTTP request headers and bodies. It also supports WebSocket and SSE.

### Behavior

- Messages sent from a WebSocket client are echoed back as WebSocket messages.  
- Requests to `*.ws` under any path serve a simple UI for WebSocket testing.  
- Requests to `*.sse` under any path stream server-sent events.  
- All other URLs return an HTTP echo response in plain text.

---

### Example HTTP Echo

```bash
curl -X POST http://localhost:8080 \
  -H "Content-Type: application/json" \
  -d '{"message": "hello world"}'
```

---

### Example gRPC Echo

```bash
grpcurl -plaintext -import-path . -proto echo.proto \
  -d '{"message":"hello world"}' localhost:9090 echo.Echo/Echo
```

---

### Example WebSocket Echo

```bash
wscat -c ws://localhost:8080/ws
# Then type a message and press enter to see it echoed back
```

---

### Example SSE

Requests to any path ending with `.sse` will stream server-sent events to the client.

```bash
curl http://localhost:8080/test.sse
```

---

## 🐾 OpenAPI PetStore API

Implements a simple PetStore API based on OpenAPI 3.0.  
The spec is located at `cmd/echo-server/openapi/petstore.yaml`.

### Endpoints (base path `/v1`)

| Method | Path               | Description                     | Example |
|--------|--------------------|----------------------------------|----------|
| GET    | `/v1/pets`         | List all pets (`limit` optional, max 100) | `curl http://localhost:8080/v1/pets?limit=10` |
| POST   | `/v1/pets`         | Create a new pet (`name`, `tag`) | `curl -X POST http://localhost:8080/v1/pets -H 'Content-Type: application/json' -d '{"name":"Rex","tag":"dog"}'` |
| GET    | `/v1/pets/{petId}` | Retrieve a specific pet          | `curl http://localhost:8080/v1/pets/1` |

---

### Data Models

#### Pet

```json
{
  "id": 1,
  "name": "Fluffy",
  "tag": "cat"
}
```

#### Error

```json
{
  "code": 404,
  "message": "Pet not found"
}
```

---

### Implementation Details

- Thread-safe (mutex locks)  
- Preloaded with 2 sample pets  
- Validation for required fields  
- In-memory only (data lost on restart)

---

## Health Check

```bash
curl http://localhost:8080/health
```

---

## Configuration

### Overview

| Variable | Description |
|-----------|-------------|
| `PORT`, `GRPC_PORT` | Set server ports (default 8080 / 9090) |
| `LOG_HTTP_HEADERS`, `LOG_HTTP_BODY` | Enable HTTP request logging |
| `SEND_SERVER_HOSTNAME` | Include hostname in echo response |
| `SEND_HEADER_*` | Add custom response headers |
| `WEBSOCKET_ROOT` | Prefix for WebSocket UI requests |

---

### Port Configuration

- `PORT` sets the HTTP server port (default: **8080**)  
- `GRPC_PORT` sets the gRPC server port (default: **9090**)

---

### Logging

Set environment variables to enable request logging:

```bash
LOG_HTTP_HEADERS=true
LOG_HTTP_BODY=true
```

---

### Server Hostname

By default, the server includes its hostname in responses.  
To disable this behavior:

```bash
SEND_SERVER_HOSTNAME=false
```

The client can also override this per request using the header:

```
X-Send-Server-Hostname: false
```

---

### Custom Response Headers

Add arbitrary headers using environment variables prefixed with `SEND_HEADER_`.  
Underscores become hyphens.

Example (to disable CORS restrictions):

```bash
SEND_HEADER_ACCESS_CONTROL_ALLOW_ORIGIN="*"
SEND_HEADER_ACCESS_CONTROL_ALLOW_METHODS="*"
SEND_HEADER_ACCESS_CONTROL_ALLOW_HEADERS="*"
```

---

### WebSocket Root Path

Set `WEBSOCKET_ROOT` to prefix all WebSocket requests made from the `.ws` UI.

Example:

```bash
WEBSOCKET_ROOT=/custom
```

Then visit:  
```
http://localhost:8080/custom.ws
```

---

## Building & Running

### Using Makefile

```bash
# Build binary
make build

# Build Docker image
make docker
```

---

### Running Locally

```bash
# Run the built binary
PORT=8081 ./artifacts/build/release/linux/amd64/echo-server

# Or run directly with Go
PORT=8081 go run ./cmd/echo-server
```

---

### Run with Docker

```bash
docker run -p 8081:8080 -p 9091:9090 stojs/echo-server:dev
```

