package backend

import (
	"log"
	"net/http/httputil"
	"net/url"
	"sync"
)

// Backend represents a single backend server in the load balancer.
type Backend struct {
	URL          *url.URL
	ReverseProxy *httputil.ReverseProxy
	mu           sync.RWMutex
	alive        bool
}

// NewBackend creates a new Backend instance for the given URL.
func NewBackend(urlStr string) *Backend {
	serverURL, err := url.Parse(urlStr)
	if err != nil {
		log.Fatalf("Error parsing backend URL: %v", err)
	}
	return &Backend{
		URL:          serverURL,
		ReverseProxy: httputil.NewSingleHostReverseProxy(serverURL),
		alive:        false,
	}
}

// IsAlive returns whether the backend is currently healthy.
func (b *Backend) IsAlive() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.alive
}

// SetAlive sets the alive status of the backend.
func (b *Backend) SetAlive(alive bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.alive = alive
}
