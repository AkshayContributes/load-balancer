# Health Checking: Detecting When Servers Go Down

## Current Implementation

In the current code, server health is managed manually:

```go
// Mark a server as alive
backend.SetAlive(true)

// Mark a server as dead
backend.SetAlive(false)

// Check if server is alive
if backend.IsAlive() {
    // Use this backend
}
```

This is **passive detection** - we detect failures when requests fail, not proactively.

## The Problem: Passive vs Active Detection

### Passive Detection (Current)

```
Server crashes
    ↓
Request tries to reach it
    ↓
Connection times out / fails
    ↓
We mark it as dead
    ↓
Next request uses different server
```

**Pros:** Simple, no overhead  
**Cons:** First request fails, slower detection, bad user experience

### Active Detection (Recommended)

```
Background goroutine (every 5 seconds)
    ↓
HTTP GET /health to each backend
    ↓
Success → Mark alive
Failure → Mark dead
    ↓
Next request already knows status
```

**Pros:** Proactive, fast detection, better UX  
**Cons:** Adds complexity, uses resources

## Implementation: Health Check Goroutine

### Step 1: Add Health Check Manager

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"
)

// HealthChecker manages background health checks
type HealthChecker struct {
    backends []*Backend
    interval time.Duration
    ctx      context.Context
    cancel   context.CancelFunc
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(backends []*Backend, interval time.Duration) *HealthChecker {
    ctx, cancel := context.WithCancel(context.Background())
    return &HealthChecker{
        backends: backends,
        interval: interval,
        ctx:      ctx,
        cancel:   cancel,
    }
}

// Start begins the health checking loop
func (hc *HealthChecker) Start() {
    go hc.healthCheckLoop()
}

// Stop stops the health checker
func (hc *HealthChecker) Stop() {
    hc.cancel()
}

// healthCheckLoop runs the health check every interval
func (hc *HealthChecker) healthCheckLoop() {
    ticker := time.NewTicker(hc.interval)
    defer ticker.Stop()

    for {
        select {
        case <-hc.ctx.Done():
            return
        case <-ticker.C:
            hc.checkAllBackends()
        }
    }
}

// checkAllBackends checks health of all backends
func (hc *HealthChecker) checkAllBackends() {
    for _, backend := range hc.backends {
        go hc.checkBackend(backend)
    }
}

// checkBackend checks health of a single backend
func (hc *HealthChecker) checkBackend(backend *Backend) {
    client := &http.Client{
        Timeout: 2 * time.Second,
    }

    // Try to reach the health endpoint
    resp, err := client.Get(backend.URL.String() + "/health")
    if err != nil {
        log.Printf("Health check failed for %s: %v", backend.URL, err)
        backend.SetAlive(false)
        return
    }
    defer resp.Body.Close()

    // Check if response is successful
    if resp.StatusCode == http.StatusOK {
        backend.SetAlive(true)
        log.Printf("Health check passed for %s", backend.URL)
    } else {
        backend.SetAlive(false)
        log.Printf("Health check failed for %s: status %d", backend.URL, resp.StatusCode)
    }
}
```

### Step 2: Use in Your Application

```go
func main() {
    // Create backends
    backends := []*Backend{
        NewBackend("http://localhost:3000"),
        NewBackend("http://localhost:3001"),
        NewBackend("http://localhost:3002"),
    }

    // Create load balancer
    lb, _ := balancer.New(backends)

    // Start health checker (check every 5 seconds)
    healthChecker := NewHealthChecker(backends, 5*time.Second)
    healthChecker.Start()
    defer healthChecker.Stop()

    // Now the load balancer automatically knows which servers are alive
    // No manual SetAlive() calls needed!

    // Simulate server crashes
    go func() {
        time.Sleep(10 * time.Second)
        backends[1].SetAlive(false)  // Simulate crash
        log.Println("Server 1 crashed!")
    }()

    // Simulate server recovery
    go func() {
        time.Sleep(20 * time.Second)
        backends[1].SetAlive(true)  // Simulate recovery
        log.Println("Server 1 recovered!")
    }()

    // Keep running
    select {}
}
```

## Advanced: Exponential Backoff

When a server fails repeatedly, don't hammer it with checks:

```go
type BackendWithBackoff struct {
    *Backend
    failureCount int
    lastCheck    time.Time
}

func (b *BackendWithBackoff) shouldCheck() bool {
    if b.failureCount == 0 {
        return true  // Always check if healthy
    }

    // Exponential backoff: 1s, 2s, 4s, 8s, 16s...
    backoffDuration := time.Second * time.Duration(1<<uint(min(b.failureCount, 4)))
    return time.Since(b.lastCheck) > backoffDuration
}

func (hc *HealthChecker) checkBackend(backend *Backend) {
    b := backend.(*BackendWithBackoff)
    
    if !b.shouldCheck() {
        return  // Skip this check, it's in backoff
    }

    client := &http.Client{Timeout: 2 * time.Second}
    resp, err := client.Get(b.URL.String() + "/health")

    b.lastCheck = time.Now()

    if err != nil || resp.StatusCode != http.StatusOK {
        b.failureCount++
        b.SetAlive(false)
        return
    }

    // Success - reset failure count
    b.failureCount = 0
    b.SetAlive(true)
}
```

## Alternative: On-Demand Health Checks

Instead of background goroutines, check health only when a request fails:

```go
func (lb *LoadBalancer) SelectBackendWithFallback() (*Backend, error) {
    // Try to get a backend
    selected, err := lb.SelectBackend()
    if err != nil {
        return nil, err  // All backends dead
    }

    // If we got here, we have an alive backend
    return selected, nil
}

// In your HTTP proxy:
func proxyRequest(lb *LoadBalancer, w http.ResponseWriter, r *http.Request) {
    backend, err := lb.SelectBackendWithFallback()
    if err != nil {
        http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
        return
    }

    // Try to proxy
    err = backend.ReverseProxy.ServeHTTP(w, r)
    if err != nil {
        // Request failed - mark backend as dead
        backend.SetAlive(false)
        log.Printf("Backend %s failed: %v", backend.URL, err)

        // Try again with different backend
        backend, err = lb.SelectBackendWithFallback()
        if err != nil {
            http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
            return
        }

        backend.ReverseProxy.ServeHTTP(w, r)
    }
}
```

## Real-World Scenario: Production Setup

```go
func setupLoadBalancer() *balancer.LoadBalancer {
    backends := []*Backend{
        NewBackend("http://api1.example.com"),
        NewBackend("http://api2.example.com"),
        NewBackend("http://api3.example.com"),
    }

    lb, _ := balancer.New(backends)

    // Start active health checks
    healthChecker := NewHealthChecker(backends, 5*time.Second)
    healthChecker.Start()

    // Also use passive detection (on request failure)
    go func() {
        for range time.Tick(30 * time.Second) {
            healthy := lb.GetHealthyBackends()
            log.Printf("Current healthy backends: %d/%d", len(healthy), len(backends))
        }
    }()

    return lb
}

func main() {
    lb := setupLoadBalancer()

    // HTTP server with proxy
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        backend, err := lb.SelectBackend()
        if err != nil {
            http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
            return
        }

        backend.ReverseProxy.ServeHTTP(w, r)
    })

    log.Println("Listening on :8080")
    http.ListenAndServe(":8080", nil)
}
```

## Health Check Endpoints

Your backend servers should expose a `/health` endpoint:

### Node.js Example
```javascript
app.get('/health', (req, res) => {
    res.status(200).json({ status: 'healthy' });
});
```

### Go Example
```go
http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
})
```

### Python Example
```python
@app.route('/health')
def health():
    return {'status': 'healthy'}, 200
```

## Comparison: Detection Methods

| Method | Detection Time | Overhead | Implementation |
|--------|---|---|---|
| Passive (on failure) | Slow (after timeout) | Zero | Simple |
| Background health checks | Fast (~5s) | Minimal | Moderate |
| Health checks + exponential backoff | Fast + adaptive | Very minimal | Advanced |
| Hybrid (both methods) | Very fast | Minimal | Recommended |

## Summary

### How to Detect Dead Servers

**Option 1: Manual (Current - Good for Testing)**
```go
backend.SetAlive(false)
```

**Option 2: Passive (Reactive - Simple)**
```
Request fails → Mark dead → Use different backend next time
```

**Option 3: Active Health Checks (Proactive - Production)**
```
Background goroutine every 5 seconds → HTTP /health → Mark alive/dead
```

**Option 4: Hybrid (Best - Production)**
```
Active checks + Passive detection on failure
```

### Recommendation for Your Load Balancer

1. **Currently:** Works with manual status changes (good for testing)
2. **Add:** Background health check goroutine (5 second interval)
3. **Result:** Automatic detection of dead/recovered servers

This keeps the load balancer core simple (selecting from backends) while adding production-ready health detection!

