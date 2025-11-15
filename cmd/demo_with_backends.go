package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

// BackendServer represents a simple test backend server
type BackendServer struct {
	Port    int
	Name    string
	Healthy atomic.Bool
}

// NewBackendServer creates a new test backend server
func NewBackendServer(port int, name string) *BackendServer {
	return &BackendServer{
		Port: port,
		Name: name,
	}
}

// Start starts the backend server
func (bs *BackendServer) Start() {
	bs.Healthy.Store(true)

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !bs.Healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "healthy",
			"name":   bs.Name,
		})
	})

	// Echo endpoint
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if !bs.Healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "server unhealthy"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Hello from " + bs.Name,
			"path":    r.URL.Path,
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	addr := fmt.Sprintf(":%d", bs.Port)
	log.Printf("Starting %s on %s", bs.Name, addr)

	go http.ListenAndServe(addr, mux)
	time.Sleep(100 * time.Millisecond) // Give server time to start
}

// Stop stops the backend server by marking it unhealthy
func (bs *BackendServer) Stop() {
	bs.Healthy.Store(false)
	log.Printf("Marked %s as unhealthy", bs.Name)
}

// Resume marks the backend server as healthy again
func (bs *BackendServer) Resume() {
	bs.Healthy.Store(true)
	log.Printf("Marked %s as healthy", bs.Name)
}

