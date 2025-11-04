// Integration tests for the echo server
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	echo "http-echo/cmd/echo-server/grpc/generated"
	"http-echo/cmd/echo-server/openapi"

	"github.com/gorilla/websocket"
	"golang.org/x/net/http2"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	testHTTPPort = "18080"
	testGRPCPort = "19090"
)

var (
	serverOnce  sync.Once
	serverReady = make(chan struct{})
	httpBaseURL string
	grpcAddress string
)

// setupTestServers starts the HTTP and gRPC servers for integration testing
func setupTestServers() {
	serverOnce.Do(func() {
		httpBaseURL = "http://localhost:" + testHTTPPort
		grpcAddress = "localhost:" + testGRPCPort

		// Start gRPC server
		go func() {
			if err := startGRPCServer(testGRPCPort); err != nil {
				panic(err)
			}
		}()

		// Start HTTP server
		go func() {
			server := &http.Server{
				Addr:    ":" + testHTTPPort,
				Handler: createRouter(),
			}

			if err := server.ListenAndServe(); err != nil {
				panic(fmt.Sprintf("failed to serve HTTP: %v", err))
			}
		}()

		// Wait for servers to be ready
		time.Sleep(500 * time.Millisecond)

		// Verify HTTP server is ready
		for i := 0; i < 30; i++ {
			resp, err := http.Get(httpBaseURL + "/health")
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				break
			}
			if resp != nil {
				resp.Body.Close()
			}
			time.Sleep(100 * time.Millisecond)
		}

		// // Verify gRPC server is ready
		// ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		// defer cancel()

		// for i := 0; i < 30; i++ {
		// 	conn, err := grpc.NewClient(grpcAddress,
		// 		grpc.WithTransportCredentials(insecure.NewCredentials()))
		// 	if err == nil {
		// 		// Try to actually use the connection
		// 		client := echo.NewEchoClient(conn)
		// 		_, err := client.Echo(ctx, &echo.EchoRequest{Message: "test"})
		// 		conn.Close()
		// 		if err == nil {
		// 			break
		// 		}
		// 	}
		// 	time.Sleep(100 * time.Millisecond)
		// }

		close(serverReady)
	})

	<-serverReady
}

// TestHealthCheck verifies the health check endpoint
func TestHealthCheck(t *testing.T) {
	setupTestServers()

	resp, err := http.Get(httpBaseURL + "/health")
	if err != nil {
		t.Fatalf("failed to make health check request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status, ok := result["status"].(string); !ok || status != "healthy" {
		t.Errorf("expected status 'healthy', got %v", result["status"])
	}

	if _, ok := result["timestamp"]; !ok {
		t.Error("expected timestamp in response")
	}

	t.Log("TestHealthCheck passed")
}

// TestHTTPEcho verifies basic HTTP echo functionality
func TestHTTPEcho(t *testing.T) {
	setupTestServers()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		headers    map[string]string
		wantStatus int
		checkBody  func(t *testing.T, body string)
	}{
		{
			name:       "GET request",
			method:     "GET",
			path:       "/test",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				if !strings.Contains(body, "GET /test HTTP") {
					t.Errorf("response doesn't contain request line: %s", body)
				}
			},
		},
		{
			name:       "POST with JSON body",
			method:     "POST",
			path:       "/api/data",
			body:       `{"message": "hello world"}`,
			headers:    map[string]string{"Content-Type": "application/json"},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				if !strings.Contains(body, "POST /api/data HTTP") {
					t.Errorf("response doesn't contain request line: %s", body)
				}
				if !strings.Contains(body, "Content-Type: application/json") {
					t.Error("response doesn't contain Content-Type header")
				}
				if !strings.Contains(body, `{"message": "hello world"}`) {
					t.Error("response doesn't contain request body")
				}
			},
		},
		{
			name:       "Custom headers echoed",
			method:     "GET",
			path:       "/headers",
			headers:    map[string]string{"X-Custom-Header": "test-value"},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body string) {
				if !strings.Contains(body, "X-Custom-Header: test-value") {
					t.Error("response doesn't contain custom header")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyReader io.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			}

			req, err := http.NewRequest(tt.method, httpBaseURL+tt.path, bodyReader)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read response body: %v", err)
			}

			if tt.checkBody != nil {
				tt.checkBody(t, string(body))
			}
		})
	}

	t.Log("TestHTTPEcho passed")
}

// TestWebSocketEcho verifies WebSocket echo functionality
func TestWebSocketEcho(t *testing.T) {
	setupTestServers()

	// Connect to WebSocket
	wsURL := "ws://localhost:" + testHTTPPort + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Read the initial server hostname message (if sent)
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, _, _ = conn.ReadMessage()
	// We expect this to either succeed (with hostname) or timeout (empty message)
	// Reset deadline
	conn.SetReadDeadline(time.Time{})

	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "Simple text message",
			message: "Hello, WebSocket!",
		},
		{
			name:    "JSON message",
			message: `{"type":"test","data":"value"}`,
		},
		{
			name:    "Unicode message",
			message: "Hello 世界 🌍",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Send message
			err := conn.WriteMessage(websocket.TextMessage, []byte(tt.message))
			if err != nil {
				t.Fatalf("failed to send message: %v", err)
			}

			// Read echo
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			messageType, received, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("failed to read message: %v", err)
			}

			if messageType != websocket.TextMessage {
				t.Errorf("expected text message type, got %d", messageType)
			}

			if string(received) != tt.message {
				t.Errorf("expected %q, got %q", tt.message, string(received))
			}
		})
	}

	t.Log("TestWebSocketEcho passed")
}

// TestServerSentEvents verifies SSE functionality
func TestServerSentEvents(t *testing.T) {
	setupTestServers()

	// Use path ending with .sse (path.Base must be ".sse")
	req, err := http.NewRequest("GET", httpBaseURL+"/events/.sse", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", ct)
	}

	// Read SSE events
	reader := bufio.NewReader(resp.Body)
	eventsFound := make(map[string]bool)

	// Read events - time events come every second
	timeout := time.After(5 * time.Second)
	done := make(chan bool)
	errors := make(chan error, 1)

	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				errors <- err
				return
			}

			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "event:") {
				eventType := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				eventsFound[eventType] = true
				t.Logf("Received event: %s", eventType)
			}

			if len(eventsFound) >= 3 { // We expect "server", "request" and "time" events
				done <- true
				return
			}
		}
	}()

	select {
	case <-done:
		// Success
	case err := <-errors:
		t.Logf("Error reading SSE stream: %v", err)
	case <-timeout:
		t.Logf("Timeout - received events: %v", eventsFound)
		t.Error("timeout waiting for SSE events")
	}

	if !eventsFound["request"] {
		t.Error("expected to receive 'request' event")
	}

	if !eventsFound["time"] {
		t.Error("expected to receive 'time' event")
	}

	t.Log("TestServerSentEvents passed")
}

// TestGRPCEcho verifies gRPC echo functionality
func TestGRPCEcho(t *testing.T) {
	setupTestServers()

	// Wait a bit more for gRPC server to be fully ready
	time.Sleep(1000 * time.Millisecond)

	conn, err := grpc.Dial(
		grpcAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to create gRPC client: %v", err)
	}
	defer conn.Close()

	client := echo.NewEchoClient(conn)

	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "Simple message",
			message: "Hello, gRPC!",
		},
		{
			name:    "Empty message",
			message: "",
		},
		{
			name:    "Unicode message",
			message: "测试 🚀",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			resp, err := client.Echo(ctx, &echo.EchoRequest{Message: tt.message})
			if err != nil {
				t.Fatalf("failed to call Echo: %v", err)
			}

			if resp.Message != tt.message {
				t.Errorf("expected %q, got %q", tt.message, resp.Message)
			}
		})
	}

	t.Log("TestGRPCEcho passed")
}

// TestPetStoreAPI verifies OpenAPI PetStore endpoints
func TestPetStoreAPI(t *testing.T) {
	setupTestServers()

	t.Run("Get existing pet", func(t *testing.T) {
		resp, err := http.Get(httpBaseURL + "/v1/pets/1")
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var pet openapi.Pet
		if err := json.NewDecoder(resp.Body).Decode(&pet); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if pet.ID != 1 {
			t.Errorf("expected pet ID 1, got %d", pet.ID)
		}

		if pet.Name != "Fluffy" {
			t.Errorf("expected pet name 'Fluffy', got %s", pet.Name)
		}
	})

	t.Run("Get non-existent pet", func(t *testing.T) {
		resp, err := http.Get(httpBaseURL + "/v1/pets/999")
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", resp.StatusCode)
		}

		var apiErr openapi.Error
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if apiErr.Code != http.StatusNotFound {
			t.Errorf("expected error code 404, got %d", apiErr.Code)
		}
	})

	t.Run("Invalid pet ID", func(t *testing.T) {
		resp, err := http.Get(httpBaseURL + "/v1/pets/invalid")
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("List all pets", func(t *testing.T) {
		resp, err := http.Get(httpBaseURL + "/v1/pets")
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var pets []openapi.Pet
		if err := json.NewDecoder(resp.Body).Decode(&pets); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Should have at least 2 default pets (Fluffy and Rex)
		if len(pets) < 2 {
			t.Errorf("expected at least 2 pets, got %d", len(pets))
		}

		// Verify first pet is Fluffy
		found := false
		for _, pet := range pets {
			if pet.Name == "Fluffy" && pet.Tag == "cat" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected to find Fluffy in the list")
		}
	})

	t.Run("List pets with limit", func(t *testing.T) {
		resp, err := http.Get(httpBaseURL + "/v1/pets?limit=1")
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var pets []openapi.Pet
		if err := json.NewDecoder(resp.Body).Decode(&pets); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if len(pets) != 1 {
			t.Errorf("expected 1 pet with limit=1, got %d", len(pets))
		}
	})

	t.Run("Create a new pet", func(t *testing.T) {
		newPet := openapi.Pet{
			Name: "Buddy",
			Tag:  "dog",
		}

		petJSON, err := json.Marshal(newPet)
		if err != nil {
			t.Fatalf("failed to marshal pet: %v", err)
		}

		resp, err := http.Post(
			httpBaseURL+"/v1/pets",
			"application/json",
			bytes.NewReader(petJSON),
		)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("expected status 201, got %d", resp.StatusCode)
		}

		var createdPet openapi.Pet
		if err := json.NewDecoder(resp.Body).Decode(&createdPet); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if createdPet.ID == 0 {
			t.Error("expected non-zero ID for created pet")
		}

		if createdPet.Name != newPet.Name {
			t.Errorf("expected name %q, got %q", newPet.Name, createdPet.Name)
		}

		if createdPet.Tag != newPet.Tag {
			t.Errorf("expected tag %q, got %q", newPet.Tag, createdPet.Tag)
		}

		// Verify we can retrieve the created pet
		getResp, err := http.Get(httpBaseURL + "/v1/pets/" + fmt.Sprintf("%d", createdPet.ID))
		if err != nil {
			t.Fatalf("failed to get created pet: %v", err)
		}
		defer getResp.Body.Close()

		if getResp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200 when getting created pet, got %d", getResp.StatusCode)
		}

		var retrievedPet openapi.Pet
		if err := json.NewDecoder(getResp.Body).Decode(&retrievedPet); err != nil {
			t.Fatalf("failed to decode retrieved pet: %v", err)
		}

		if retrievedPet.ID != createdPet.ID {
			t.Errorf("expected ID %d, got %d", createdPet.ID, retrievedPet.ID)
		}
	})

	t.Run("Create pet without name fails", func(t *testing.T) {
		invalidPet := openapi.Pet{
			Tag: "bird",
		}

		petJSON, err := json.Marshal(invalidPet)
		if err != nil {
			t.Fatalf("failed to marshal pet: %v", err)
		}

		resp, err := http.Post(
			httpBaseURL+"/v1/pets",
			"application/json",
			bytes.NewReader(petJSON),
		)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", resp.StatusCode)
		}

		var apiErr openapi.Error
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}

		if !strings.Contains(apiErr.Message, "name") {
			t.Errorf("expected error message about name, got %q", apiErr.Message)
		}
	})

	t.Log("TestPetStoreAPI passed")
}

// TestHTTP2Support verifies HTTP/2 support via h2c
func TestHTTP2Support(t *testing.T) {
	setupTestServers()

	// Create an HTTP/2 client that allows cleartext HTTP/2
	client := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
				// Ignore TLS config and use plaintext connection
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}

	req, err := http.NewRequest("GET", httpBaseURL+"/test-h2c", bytes.NewReader([]byte("test body")))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to make HTTP/2 request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr, "GET /test-h2c HTTP/2.0") {
		t.Errorf("expected HTTP/2.0 in response, got: %s", bodyStr)
	}

	if !strings.Contains(bodyStr, "test body") {
		t.Error("response doesn't contain request body")
	}

	t.Log("TestHTTP2Support passed")
}
