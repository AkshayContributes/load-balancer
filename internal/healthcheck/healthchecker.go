package healthcheck

import (
	"context"
	"log"
	"net/http"
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

// NewHealthChecker creates a new HealthChecker instance
func NewHealthChecker(backends []*backend.Backend, interval time.Duration) *HealthChecker {
	ctx, cancel := context.WithCancel(context.Background())
	return &HealthChecker{
		backends: backends,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
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

// checkAllBackends checks the health of all backends concurrently
func (hc *HealthChecker) checkAllBackends() {
	for _, backend := range hc.backends {
		go hc.checkBackend(backend)
	}
}

// checkBackend checks the health of a single backend
func (hc *HealthChecker) checkBackend(b *backend.Backend) {
	resp, err := hc.client.Get(b.URL.String() + "/health")

	if err != nil {
		log.Printf("❌ Health check failed for %s: %v", b.URL, err)
		b.SetAlive(false)
		return
	}
	defer resp.Body.Close()

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
