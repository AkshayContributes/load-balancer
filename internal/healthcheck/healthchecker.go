package healthcheck

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/akshaykumarthakur/load-balancer/internal/backend"
)

// HealthChecker periodically checks the health of backends
type HealthChecker struct {
	backends []*backend.Backend
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	client   *http.Client
}

// NewHealthChecker creates a new HealthChecker instance with connection pooling
func NewHealthChecker(backends []*backend.Backend, interval time.Duration) *HealthChecker {
	ctx, cancel := context.WithCancel(context.Background())

	// Create HTTP client with connection pooling for optimal performance
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			// Connection pooling settings
			MaxIdleConns:        100,              // Total idle connections to keep alive
			MaxIdleConnsPerHost: 10,               // Per-host idle connections
			IdleConnTimeout:     90 * time.Second, // Keep connections alive for 90 seconds
			DisableKeepAlives:   false,            // Enable Keep-Alive (reuse connections)
			DisableCompression:  true,             // Disable gzip (not needed for health checks)
			MaxConnsPerHost:     10,               // Max concurrent connections per host
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		},
	}

	return &HealthChecker{
		backends: backends,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
		client:   client,
	}
}

// Start begins the health checking loop in a goroutine
func (hc *HealthChecker) Start() {
	go hc.healthCheckLoop()
	log.Printf("✅ Health checker started (interval: %v)", hc.interval)
}

// Stop stops the health checker gracefully
func (hc *HealthChecker) Stop() {
	hc.cancel()
	log.Println("⏹️  Health checker stopped")
}

// healthCheckLoop runs the health checks periodically
func (hc *HealthChecker) healthCheckLoop() {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	// Run health check immediately on start
	hc.checkAllBackends()

	for {
		select {
		case <-hc.ctx.Done():
			return
		case <-ticker.C:
			hc.checkAllBackends()
		}
	}
}

// checkAllBackends checks the health of all backends concurrently with proper synchronization
func (hc *HealthChecker) checkAllBackends() {
	var wg sync.WaitGroup

	for _, b := range hc.backends {
		wg.Add(1)
		// Pass backend as parameter to avoid closure variable capture issues
		go func(backend *backend.Backend) {
			defer wg.Done()
			hc.checkBackend(backend)
		}(b)
	}

	// Wait for all health checks to complete before returning
	wg.Wait()
}

// checkBackend checks the health of a single backend
func (hc *HealthChecker) checkBackend(b *backend.Backend) {
	resp, err := hc.client.Get(b.URL.String() + "/health")

	if err != nil {
		wasAlive := b.IsAlive()
		b.SetAlive(false)
		if wasAlive {
			log.Printf("❌ Health check failed for %s: %v", b.URL, err)
		}
		return
	}
	defer resp.Body.Close()

	// Read response body to enable connection reuse in the pool
	_, _ = io.ReadAll(resp.Body)

	// Check if response is successful
	if resp.StatusCode == http.StatusOK {
		wasAlive := b.IsAlive()
		b.SetAlive(true)
		if !wasAlive {
			log.Printf("✅ %s is now healthy (recovered)", b.URL)
		}
	} else {
		wasAlive := b.IsAlive()
		b.SetAlive(false)
		if wasAlive {
			log.Printf("❌ %s is now unhealthy (status: %d)", b.URL, resp.StatusCode)
		}
	}
}
