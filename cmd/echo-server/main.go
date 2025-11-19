// Package main is the executable for the echo server.
package main

import (
	"bytes"
	"embed"

	// "encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"context"
	echo "http-echo/cmd/echo-server/grpc/generated"
	"http-echo/cmd/echo-server/openapi"
	"net"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// createRouter creates and configures the HTTP router with all routes
func createRouter() http.Handler {
	r := mux.NewRouter()

	// Create pet store and register OpenAPI routes
	store := openapi.NewPetStore()
	api := r.PathPrefix("/v1").Subrouter()
	api.HandleFunc("/pets", store.ListPets).Methods("GET")
	api.HandleFunc("/pets", store.CreatePets).Methods("POST")
	// api.HandleFunc("/pets", store.HandleOptions).Methods("OPTIONS")
	api.HandleFunc("/pets/{petId}", store.ShowPetById).Methods("GET")
	// api.HandleFunc("/pets/{petId}", store.HandleOptions).Methods("OPTIONS")

	// Add health check endpoint
	r.HandleFunc("/health", healthCheck).Methods("GET")

	// Add error throwing endpoint
	r.HandleFunc("/throw", throwErrorHandler).Methods("GET")

	// Default handler for echo server functionality
	r.PathPrefix("/").HandlerFunc(handler)

	return h2c.NewHandler(
		r,
		&http2.Server{},
	)
}

// startGRPCServer starts the gRPC server on the specified port
func startGRPCServer(grpcPort string) error {
	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	echo.RegisterEchoServer(s, &grpcEchoServer{})
	reflection.Register(s)
	if err := s.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve gRPC: %v", err)
	}
	return nil
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	grpcPort := os.Getenv("GRPC_PORT")
	if grpcPort == "" {
		grpcPort = "9090"
	}

	fmt.Printf("Version: 0.0.1\n")

	fmt.Printf("Echo HTTP server listening on port %s.\n", port)
	fmt.Printf("Echo gRPC server listening on port %s.\n", grpcPort)

	// Start gRPC server in goroutine
	go func() {
		if err := startGRPCServer(grpcPort); err != nil {
			panic(err)
		}
	}()

	// Start HTTP server
	err := http.ListenAndServe(":"+port, createRouter())
	if err != nil {
		panic(err)
	}
}

// grpcEchoServer implements echo.EchoServer
type grpcEchoServer struct {
	echo.UnimplementedEchoServer
}

func (s *grpcEchoServer) Echo(ctx context.Context, req *echo.EchoRequest) (*echo.EchoResponse, error) {
	fmt.Printf("gRPC Echo called: %s\n", req.GetMessage())
	return &echo.EchoResponse{Message: req.GetMessage()}, nil
}

// healthCheck provides a simple health check endpoint
func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"healthy","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool {
		return true
	},
}

func handler(wr http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	if os.Getenv("LOG_HTTP_BODY") != "" || os.Getenv("LOG_HTTP_HEADERS") != "" {
		fmt.Printf("--------  %s | %s %s\n", req.RemoteAddr, req.Method, req.URL)
	} else {
		fmt.Printf("%s | %s %s\n", req.RemoteAddr, req.Method, req.URL)
	}

	if os.Getenv("LOG_HTTP_HEADERS") != "" {
		fmt.Printf("Headers\n")
		printHeaders(os.Stdout, req.Header)
	}

	if os.Getenv("LOG_HTTP_BODY") != "" {
		buf := &bytes.Buffer{}
		buf.ReadFrom(req.Body) // nolint:errcheck

		if buf.Len() != 0 {
			fmt.Printf("Body:\n%s\n", buf.String())
		}

		// Replace original body with buffered version so it's still sent to the
		// browser.
		req.Body.Close()
		req.Body = io.NopCloser(
			bytes.NewReader(buf.Bytes()),
		)
	}

	sendServerHostnameString := os.Getenv("SEND_SERVER_HOSTNAME")
	if v := req.Header.Get("X-Send-Server-Hostname"); v != "" {
		sendServerHostnameString = v
	}

	sendServerHostname := !strings.EqualFold(
		sendServerHostnameString,
		"false",
	)

	for _, line := range os.Environ() {
		parts := strings.SplitN(line, "=", 2)
		key, value := parts[0], parts[1]

		if name, ok := strings.CutPrefix(key, `SEND_HEADER_`); ok {
			wr.Header().Set(
				strings.ReplaceAll(name, "_", "-"),
				value,
			)
		}
	}

	if websocket.IsWebSocketUpgrade(req) {
		serveWebSocket(wr, req, sendServerHostname)
	} else if path.Base(req.URL.Path) == ".ws" {
		serveFrontend(wr, req)
	} else if path.Base(req.URL.Path) == ".sse" {
		serveSSE(wr, req, sendServerHostname)
	} else {
		serveHTTP(wr, req, sendServerHostname)
	}
}

func serveWebSocket(wr http.ResponseWriter, req *http.Request, sendServerHostname bool) {
	connection, err := upgrader.Upgrade(wr, req, nil)
	if err != nil {
		fmt.Printf("%s | %s\n", req.RemoteAddr, err)
		return
	}

	defer connection.Close()
	fmt.Printf("%s | upgraded to websocket\n", req.RemoteAddr)

	var message []byte

	if sendServerHostname {
		host, err := os.Hostname()
		if err == nil {
			message = []byte(fmt.Sprintf("Request served by %s", host))
		} else {
			message = []byte(fmt.Sprintf("Server hostname unknown: %s", err.Error()))
		}
	}

	err = connection.WriteMessage(websocket.TextMessage, message)
	if err == nil {
		var messageType int

		for {
			messageType, message, err = connection.ReadMessage()
			if err != nil {
				break
			}

			if messageType == websocket.TextMessage {
				fmt.Printf("%s | txt | %s\n", req.RemoteAddr, message)
			} else {
				fmt.Printf("%s | bin | %d byte(s)\n", req.RemoteAddr, len(message))
			}

			err = connection.WriteMessage(messageType, message)
			if err != nil {
				break
			}
		}
	}

	if err != nil {
		fmt.Printf("%s | %s\n", req.RemoteAddr, err)
	}
}

//go:embed "html"
var files embed.FS

func serveFrontend(wr http.ResponseWriter, req *http.Request) {
	const templateName = "html/frontend.tmpl.html"
	tmpl, err := template.ParseFS(files, templateName)
	if err != nil {
		http.Error(wr, err.Error(), http.StatusInternalServerError)
		return
	}
	templateData := struct {
		Path string
	}{
		Path: path.Join(
			os.Getenv("WEBSOCKET_ROOT"),
			path.Dir(req.URL.Path),
		),
	}
	err = tmpl.Execute(wr, templateData)
	if err != nil {
		http.Error(wr, err.Error(), http.StatusInternalServerError)
		return
	}
	wr.Header().Add("Content-Type", "text/html")
	wr.WriteHeader(200)
}

func serveHTTP(wr http.ResponseWriter, req *http.Request, sendServerHostname bool) {
	wr.Header().Add("Content-Type", "text/plain")
	wr.WriteHeader(200)

	if sendServerHostname {
		host, err := os.Hostname()
		if err == nil {
			fmt.Fprintf(wr, "Request served by %s\n\n", host)
		} else {
			fmt.Fprintf(wr, "Server hostname unknown: %s\n\n", err.Error())
		}
	}

	writeRequest(wr, req)
}

func serveSSE(wr http.ResponseWriter, req *http.Request, sendServerHostname bool) {
	if _, ok := wr.(http.Flusher); !ok {
		http.Error(wr, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	var echo strings.Builder
	writeRequest(&echo, req)

	wr.Header().Set("Content-Type", "text/event-stream")
	wr.Header().Set("Cache-Control", "no-cache")
	wr.Header().Set("Connection", "keep-alive")
	wr.Header().Set("Access-Control-Allow-Origin", "*")

	var id int

	// Write an event about the server that is serving this request.
	if sendServerHostname {
		if host, err := os.Hostname(); err == nil {
			writeSSE(
				wr,
				req,
				&id,
				"server",
				host,
			)
		}
	}

	// Write an event that echoes back the request.
	writeSSE(
		wr,
		req,
		&id,
		"request",
		echo.String(),
	)

	// Then send a counter event every second.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-req.Context().Done():
			return
		case t := <-ticker.C:
			writeSSE(
				wr,
				req,
				&id,
				"time",
				t.Format(time.RFC3339),
			)
		}
	}
}

// writeSSE sends a server-sent event and logs it to the console.
func writeSSE(
	wr http.ResponseWriter,
	req *http.Request,
	id *int,
	event, data string,
) {
	*id++
	writeSSEField(wr, req, "event", event)
	writeSSEField(wr, req, "data", data)
	writeSSEField(wr, req, "id", strconv.Itoa(*id))
	fmt.Fprintf(wr, "\n")
	wr.(http.Flusher).Flush()
}

// writeSSEField sends a single field within an event.
func writeSSEField(
	wr http.ResponseWriter,
	req *http.Request,
	k, v string,
) {
	for _, line := range strings.Split(v, "\n") {
		fmt.Fprintf(wr, "%s: %s\n", k, line)
		fmt.Printf("%s | sse | %s: %s\n", req.RemoteAddr, k, line)
	}
}

// writeRequest writes request headers to w.
func writeRequest(w io.Writer, req *http.Request) {
	fmt.Fprintf(w, "%s %s %s\n", req.Method, req.URL, req.Proto)
	fmt.Fprintln(w, "")

	fmt.Fprintf(w, "Host: %s\n", req.Host)
	printHeaders(w, req.Header)

	var body bytes.Buffer
	io.Copy(&body, req.Body) // nolint:errcheck

	if body.Len() > 0 {
		fmt.Fprintln(w, "")
		body.WriteTo(w) // nolint:errcheck
	}
}

func printHeaders(w io.Writer, h http.Header) {
	sortedKeys := make([]string, 0, len(h))

	for key := range h {
		sortedKeys = append(sortedKeys, key)
	}

	sort.Strings(sortedKeys)

	for _, key := range sortedKeys {
		for _, value := range h[key] {
			fmt.Fprintf(w, "%s: %s\n", key, value)
		}
	}
}

// throwErrorHandler throws an error with the given status code from the query param
func throwErrorHandler(w http.ResponseWriter, r *http.Request) {
	codeStr := r.URL.Query().Get("code")
	code, err := strconv.Atoi(codeStr)
	if err != nil || code < 100 || code > 599 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"Invalid status code"}`)
		return
	}
	w.WriteHeader(code)
	fmt.Fprintf(w, `{"error":"This is a forced error with status %d"}`, code)
}
