# Where Are Servers Queried? The Missing Piece

## Current State (Your Demo)

```go
// cmd/main.go
backends[1].SetAlive(false)  // ← Manual, not automatic
```

**Problem:** You manually set status. In production, nobody does this manually!

## Where Health Checks SHOULD Happen

Health queries happen in **three locations**:

### Location 1: Background Health Check Loop
This is the **proactive** detection:

```go
// In a HealthChecker goroutine, running every 5 seconds
func (hc *HealthChecker) healthCheckLoop() {
    ticker := time.NewTicker(5 * time.Second)
    for range ticker.C {
        for _, backend := range hc.backends {
            // ← QUERY #1: Here we query each backend
            resp, err := http.Get(backend.URL.String() + "/health")
            if err != nil {
                backend.SetAlive(false)  // Mark as dead
                continue
            }
            if resp.StatusCode == 200 {
                backend.SetAlive(true)   // Mark as alive
            }
        }
    }
}
```

**When:** Every 5 seconds (background)  
**What:** HTTP GET to `/health` endpoint  
**Who:** HealthChecker goroutine  
**Result:** Automatic detection of failures/recovery

---

### Location 2: On Request Proxy Failure
This is the **reactive** detection:

```go
// In your HTTP handler
http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    backend, err := lb.SelectBackend()
    if err != nil {
        http.Error(w, "Service Unavailable", 503)
        return
    }
    
    // ← QUERY #2: Request is sent here
    err := backend.ReverseProxy.ServeHTTP(w, r)
    if err != nil {
        // Request failed - mark backend as dead
        backend.SetAlive(false)  // ← QUERY happens when it fails
        log.Printf("Backend %s failed", backend.URL)
    }
})
```

**When:** On every request (if it fails)  
**What:** Actual client request to backend  
**Who:** HTTP handler  
**Result:** Fast detection when server crashes mid-operation

---

### Location 3: On-Demand Query (Optional)
When you explicitly need current status:

```go
// Get fresh status immediately
resp, err := http.Get(backend.URL.String() + "/health")
if err != nil {
    backend.SetAlive(false)
}
```

**When:** On demand (e.g., admin endpoint)  
**What:** HTTP GET to `/health`  
**Result:** Immediate status check

---

## Architecture Diagram: Where Queries Happen

```
┌─────────────────────────────────────────────────────────────┐
│                    Your Load Balancer                       │
└─────────────────────────────────────────────────────────────┘

                            ↓

┌────────────────────────────────────────────────────────────┐
│ HealthChecker Goroutine (Every 5 seconds)                 │
│                                                             │
│  FOR each backend:                                         │
│    GET backend.URL/health  ← QUERY #1                     │
│    If success → SetAlive(true)                            │
│    If fail → SetAlive(false)                              │
└────────────────────────────────────────────────────────────┘

                            ↓

┌────────────────────────────────────────────────────────────┐
│ Load Balancer.SelectBackend()                             │
│                                                             │
│  Uses atomic counter for round-robin                      │
│  Only selects alive backends (from HealthChecker)         │
└────────────────────────────────────────────────────────────┘

                            ↓

┌────────────────────────────────────────────────────────────┐
│ HTTP Handler (On every client request)                    │
│                                                             │
│  backend.ReverseProxy.ServeHTTP(w, r) ← QUERY #2        │
│  (Actual request to backend)                              │
│                                                             │
│  If error:                                                 │
│    backend.SetAlive(false)  ← Reactive update            │
│    Retry with different backend                           │
└────────────────────────────────────────────────────────────┘

                            ↓

              ┌─────────────┬─────────────┬─────────────┐
              ↓             ↓             ↓             ↓
        Backend 1    Backend 2      Backend 3      Backend 4
       (port 3000)  (port 3001)   (port 3002)   (port 3003)
```

---

## Production Example: Complete Flow

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"
    "github.com/akshaykumarthakur/load-balancer/internal/backend"
    "github.com/akshaykumarthakur/load-balancer/pkg/balancer"
)

// HealthChecker - the component that queries servers
type HealthChecker struct {
    backends []*backend.Backend
    interval time.Duration
    ctx      context.Context
    cancel   context.CancelFunc
}

func NewHealthChecker(backends []*backend.Backend, interval time.Duration) *HealthChecker {
    ctx, cancel := context.WithCancel(context.Background())
    return &HealthChecker{
        backends: backends,
        interval: interval,
        ctx:      ctx,
        cancel:   cancel,
    }
}

func (hc *HealthChecker) Start() {
    go hc.healthCheckLoop()
}

func (hc *HealthChecker) Stop() {
    hc.cancel()
}

// ← THIS IS WHERE QUERIES HAPPEN (Location #1)
func (hc *HealthChecker) healthCheckLoop() {
    ticker := time.NewTicker(hc.interval)
    defer ticker.Stop()

    for {
        select {
        case <-hc.ctx.Done():
            return
        case <-ticker.C:
            log.Println("🔍 Starting health check round...")
            for _, b := range hc.backends {
                go func(backend *backend.Backend) {
                    client := &http.Client{Timeout: 2 * time.Second}
                    
                    // ← QUERY: HTTP GET /health
                    resp, err := client.Get(backend.URL.String() + "/health")
                    
                    if err != nil {
                        log.Printf("❌ %s failed: %v", backend.URL, err)
                        backend.SetAlive(false)
                        return
                    }
                    defer resp.Body.Close()

                    if resp.StatusCode == http.StatusOK {
                        log.Printf("✅ %s is healthy", backend.URL)
                        backend.SetAlive(true)
                    } else {
                        log.Printf("❌ %s returned %d", backend.URL, resp.StatusCode)
                        backend.SetAlive(false)
                    }
                }(b)
            }
        }
    }
}

func main() {
    // Create backends
    backends := []*backend.Backend{
        backend.NewBackend("http://localhost:3000"),
        backend.NewBackend("http://localhost:3001"),
        backend.NewBackend("http://localhost:3002"),
    }

    // Create load balancer
    lb, err := balancer.New(backends)
    if err != nil {
        log.Fatalf("Failed to create load balancer: %v", err)
    }

    // ← START HEALTH CHECKER (Location #1 begins)
    healthChecker := NewHealthChecker(backends, 5*time.Second)
    healthChecker.Start()
    defer healthChecker.Stop()

    log.Println("✅ Health checker started (queries every 5 seconds)")

    // ← HTTP HANDLER (Location #2 begins)
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        backend, err := lb.SelectBackend()
        if err != nil {
            http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
            return
        }

        log.Printf("→ Proxying request to %s", backend.URL)
        
        // ← QUERY: Actual proxy request (Location #2)
        err = backend.ReverseProxy.ServeHTTP(w, r)
        if err != nil {
            // ← REACTIVE: Request failed, mark as dead
            backend.SetAlive(false)
            log.Printf("❌ Backend %s failed, marking as down", backend.URL)
        }
    })

    log.Println("🚀 Load balancer listening on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

---

## Query Timeline: What Happens When

```
Time    Event                           Query Location
────────────────────────────────────────────────────
T=0s    HealthChecker starts
T=5s    Background check round 1        ← Location #1: /health queries
        GET http://localhost:3000/health → 200 OK ✅
        GET http://localhost:3001/health → 200 OK ✅
        GET http://localhost:3002/health → 200 OK ✅

T=7s    Client sends request
        Load balancer selects backend
        ProxyRequest sent              ← Location #2: Actual proxy
        Response returned to client

T=10s   Background check round 2        ← Location #1: /health queries
        GET http://localhost:3000/health → 200 OK ✅
        GET http://localhost:3001/health → TIMEOUT ❌
        GET http://localhost:3002/health → 200 OK ✅
        Marked 3001 as dead

T=15s   Client sends request
        Load balancer selects backend (skips 3001 - it's dead)
        ProxyRequest sent              ← Location #2: Actual proxy

T=20s   Background check round 3        ← Location #1: /health queries
        GET http://localhost:3001/health → 200 OK ✅
        Marked 3001 as alive again

T=25s   Background check round 4        ← Location #1: /health queries
        ...
```

---

## Summary: The Three Query Locations

| Location | When | What | Who | Frequency |
|----------|------|------|-----|-----------|
| **#1: Background Health Check** | Every 5 seconds | GET `/health` | HealthChecker goroutine | Periodic |
| **#2: Proxy Request Failure** | On request failure | Actual proxy attempt | HTTP handler | On demand |
| **#3: On-Demand Check** | When needed | GET `/health` | Admin/debug endpoint | Manual |

---

## Missing Piece in Current Code

Your current `cmd/main.go`:
```go
backends[1].SetAlive(false)  // ← You manually set it
```

Should be replaced with:
```go
// Start the HealthChecker - it will automatically query servers
healthChecker := NewHealthChecker(backends, 5*time.Second)
healthChecker.Start()
defer healthChecker.Stop()

// Now servers are queried automatically every 5 seconds
// No manual SetAlive() calls needed!
```

---

## Key Insight

**You don't query servers manually!** 

Instead:
1. **HealthChecker** queries servers every 5 seconds (proactive)
2. **HTTP handler** queries servers on each request (reactive)
3. **LoadBalancer** uses the results to decide routing

The beauty is: **you set it up once, and it runs automatically** 🚀

