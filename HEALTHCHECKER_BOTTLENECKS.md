# HealthChecker Bottleneck Analysis

## Overview

This document analyzes potential bottlenecks in the HealthChecker implementation and provides optimization strategies.

## Current Implementation Review

```go
func (hc *HealthChecker) checkAllBackends() {
	for _, backend := range hc.backends {
		go hc.checkBackend(backend)  // ← Creates new goroutine per backend
	}
}

func (hc *HealthChecker) checkBackend(b *backend.Backend) {
	resp, err := hc.client.Get(b.URL.String() + "/health")  // ← Blocking HTTP call
	// ... status update ...
}
```

## Identified Bottlenecks

### 🔴 Bottleneck #1: No Rate Limiting / Goroutine Explosion

**Problem:**
```go
for _, backend := range hc.backends {
    go hc.checkBackend(backend)  // Creates new goroutine every time!
}
```

**Scenario:**
- 100 backends
- Health check every 5 seconds
- Each check creates 100 new goroutines
- Over time: 2,000 goroutines per minute!
- Memory leak over long-running servers

**Impact:** Medium (High with many backends)

**Proof:**
```go
// Current behavior:
T=0s:  ✓ Create 100 goroutines for health checks
T=5s:  ✓ Create 100 NEW goroutines (old ones might still be checking!)
T=10s: ✓ Create 100 NEW goroutines
T=15s: ✓ 300+ goroutines now active!
```

**Solution:**

Option 1: Wait for all checks to complete
```go
func (hc *HealthChecker) checkAllBackends() {
	var wg sync.WaitGroup
	for _, backend := range hc.backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			hc.checkBackend(b)
		}(backend)
	}
	wg.Wait()  // ← Wait for all to complete
}
```

Option 2: Semaphore to limit concurrent checks
```go
func (hc *HealthChecker) checkAllBackends() {
	sem := make(chan struct{}, 10)  // Max 10 concurrent
	var wg sync.WaitGroup
	for _, backend := range hc.backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			hc.checkBackend(b)
		}(backend)
	}
	wg.Wait()
}
```

---

### 🔴 Bottleneck #2: Single HTTP Client - Connection Pooling

**Problem:**
```go
client: &http.Client{
	Timeout: 2 * time.Second,
}
```

**Current Behavior:**
```
GET http://backend1:3000/health
GET http://backend2:3001/health
GET http://backend3:3002/health
```

Each request:
1. ❌ TCP connection setup (handshake)
2. ❌ TLS handshake (if HTTPS)
3. ✅ HTTP GET
4. ❌ Connection close / re-open for next

**Impact:** Medium-High (main bottleneck for health checks)

**Calculation:**
```
Assumptions:
- 10 backends
- 5 second health check interval
- 100ms per connection setup
- 2ms per health check request

Per check cycle:
  Setup: 10 × 100ms = 1000ms ← WASTED!
  Request: 10 × 2ms = 20ms
  Total: 1020ms (98% wasted on setup!)

Per hour:
  720 cycles × 1000ms = 720 seconds = 12 minutes wasted!
```

**Solution: Connection Pooling**

```go
client: &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,           // Keep connections alive
		MaxIdleConnsPerHost: 10,            // Per host
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,         // ← Keep-Alive enabled
		DisableCompression:  true,          // Disable gzip for health checks
	},
}
```

**Impact:**
```
With connection pooling:
  Setup: 10 × 0ms = 0ms (connections reused!)
  Request: 10 × 2ms = 20ms
  Total: 20ms per cycle

Before: 1020ms per cycle
After: 20ms per cycle
Improvement: 50x faster! ✓

Per hour:
  Before: 720 × 1020ms = 12 minutes wasted
  After: 720 × 20ms = 14 seconds
  Saved: 11.77 minutes per hour!
```

---

### 🟡 Bottleneck #3: Logging Overhead

**Problem:**
```go
log.Printf("❌ Health check failed for %s: %v", b.URL, err)
log.Printf("✅ %s is now healthy (recovered)", b.URL)
log.Printf("❌ %s is now unhealthy (status: %d)", b.URL, resp.StatusCode)
```

**Current Behavior:**
- 10 backends
- 5 second interval
- Logging on every check: 2 log lines per backend
- 120 log lines per minute!

**Impact:** Low-Medium (depends on logging backend)

**With many backends:**
```
- 100 backends
- 5 second interval
- 2 log lines per backend per cycle
- 24 log lines per second
- Disk I/O: ~1-2KB per second
- CPU: 2-5% for logging
```

**Solution: Conditional Logging**

```go
// Option 1: Only log on state change (already done!)
if !wasAlive {
    log.Printf("✅ %s is now healthy (recovered)", b.URL)
}

// Option 2: Suppress repeated failures
func (hc *HealthChecker) checkBackend(b *backend.Backend) {
	resp, err := hc.client.Get(b.URL.String() + "/health")

	if err != nil {
		wasAlive := b.IsAlive()
		b.SetAlive(false)
		if wasAlive {  // ← Only log on state change
			log.Printf("❌ %s failed: %v", b.URL, err)
		}
		return
	}
	// ...
}

// Option 3: Use structured logging with lower level
slog.Debug("health check", "backend", b.URL.String(), "status", resp.StatusCode)
```

---

### 🟡 Bottleneck #4: Interval Not Adaptive to Load

**Problem:**
```go
ticker := time.NewTicker(hc.interval)  // Fixed interval
```

**Current Behavior:**
```
Fixed 5 second interval, regardless of:
  • How many backends exist
  • How long checks take
  • Current server load
  • Network latency
```

**Scenario:**
```
T=0s:   Start health checks (100 backends)
T=2.5s: Checks complete
T=5s:   Start NEXT health checks
T=7.5s: Checks complete
T=10s:  Start NEXT health checks

Result: Good - 2.5s gap between checks

BUT if backend network is slow:
T=0s:   Start health checks (100 backends)
T=4.8s: Checks complete (took 4.8 seconds!)
T=5s:   Start NEXT health checks (overlapping!)
T=9.8s: Previous checks complete
T=10s:  Start NEXT health checks

Result: Overlapping checks, wasted resources
```

**Impact:** Low (unless network is very slow)

**Solution: Jitter + Sequential Wait**

```go
func (hc *HealthChecker) healthCheckLoop() {
	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	hc.checkAllBackends()

	for {
		select {
		case <-hc.ctx.Done():
			return
		case <-ticker.C:
			start := time.Now()
			hc.checkAllBackends()
			elapsed := time.Since(start)
			
			// Wait for checks to complete before next cycle
			if elapsed > hc.interval {
				log.Warnf("Health checks took %v (longer than interval %v)",
					elapsed, hc.interval)
			}
		}
	}
}
```

---

### 🟢 Bottleneck #5: Response Body Not Fully Read (Minor)

**Problem:**
```go
resp, err := hc.client.Get(b.URL.String() + "/health")
// ...
defer resp.Body.Close()  // ← Not reading body!
```

**Current Behavior:**
```go
// The response body is NOT read, just closed
// This is actually OK but can cause connection reuse issues
```

**Impact:** Very Low (but good practice to fix)

**Solution:**

```go
resp, err := hc.client.Get(b.URL.String() + "/health")
if err != nil {
	b.SetAlive(false)
	return
}
defer resp.Body.Close()

// Read the body to allow connection reuse
io.ReadAll(resp.Body)  // ← Ensures connection reuse

if resp.StatusCode == http.StatusOK {
	b.SetAlive(true)
}
```

---

## Summary: Bottleneck Severity

| Bottleneck | Severity | Impact | Fix Difficulty | Priority |
|-----------|----------|--------|-----------------|----------|
| Goroutine Explosion | Medium | Memory leak over time | Easy | High |
| Connection Pooling | High | Slow health checks | Medium | High |
| Logging Overhead | Low-Med | CPU/Disk usage | Easy | Medium |
| Non-Adaptive Interval | Low | Overlapping checks | Medium | Low |
| Response Body | Very Low | Minor connection leak | Very Easy | Low |

---

## Recommended Optimizations (by priority)

### Priority 1: Add Connection Pooling (Most Impact)

```go
NewHealthChecker with optimized transport:

client: &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		DisableCompression:  true,
	},
}
```

**Expected Improvement:** 50x faster health checks

---

### Priority 2: Add WaitGroup (Prevent Goroutine Explosion)

```go
func (hc *HealthChecker) checkAllBackends() {
	var wg sync.WaitGroup
	for _, backend := range hc.backends {
		wg.Add(1)
		go func(b *backend.Backend) {
			defer wg.Done()
			hc.checkBackend(b)
		}(backend)
	}
	wg.Wait()
}
```

**Expected Improvement:** Prevent memory leak, predictable resource usage

---

### Priority 3: Improve Logging (Already Done!)

Your current logging only logs on state changes, which is already optimal!

---

## Current State Assessment

Your HealthChecker implementation is **good** but has **two significant bottlenecks**:

✅ **Already Good:**
- Concurrent backend checking (goroutines per backend)
- Logging on state change only
- Graceful shutdown with context
- Configurable interval
- Fixed timeout (2 seconds)

⚠️ **Should Improve:**
1. **HIGH PRIORITY:** Add connection pooling (50x performance gain)
2. **MEDIUM PRIORITY:** Add WaitGroup to prevent goroutine accumulation

❌ **Current Issues:**
- New goroutines created every 5 seconds (never cleaned up until stop)
- Each health check creates new TCP connection (no reuse)

---

## Real-World Example: Current vs Optimized

### Scenario: 50 backends, 5-second interval, running for 24 hours

**Current Implementation:**
```
Goroutines created per 24 hours:
  (86400 seconds / 5 seconds) × 50 backends = 864,000 goroutines
  (Even though most complete, some accumulation)

Connection overhead per 24 hours:
  864,000 checks × 100ms setup = 86,400 seconds = 24 hours!
  ✗ Complete waste of time!

Memory usage:
  ~2KB per goroutine × 1000 active = ~2MB
  (If goroutines don't clean up properly)
```

**Optimized Implementation:**
```
Goroutines created per 24 hours:
  (86400 seconds / 5 seconds) × 1 monitoring goroutine = Predictable
  Max concurrent: 50 (with WaitGroup sync)

Connection overhead per 24 hours:
  Reused connections, setup only on first check
  ✓ Negligible overhead

Memory usage:
  Fixed connection pool: ~100 connections × 1KB = ~100KB
  (Predictable, bounded)
```

---

## When These Bottlenecks Matter

### Small Scale (1-5 backends)
- Impact: Negligible
- Current implementation: Fine

### Medium Scale (10-50 backends)
- Impact: Noticeable
- Connection pooling: 10-100x improvement
- WaitGroup: Prevents long-term memory issues

### Large Scale (100+ backends)
- Impact: Critical
- Connection pooling: 50-500x improvement
- WaitGroup: Must have to prevent crashes
- Consider: Distributed health checking

---

## Configuration Recommendations

### For Small Deployments (< 10 backends)
```go
healthcheck.NewHealthChecker(backends, 5*time.Second)
// Current implementation is fine
```

### For Medium Deployments (10-100 backends)
```go
// Add connection pooling
// Add WaitGroup
// Check interval: 5-10 seconds
healthcheck.NewHealthChecker(backends, 5*time.Second)
```

### For Large Deployments (100+ backends)
```go
// Add connection pooling
// Add WaitGroup with semaphore
// Add exponential backoff for failures
// Check interval: 10-30 seconds (increase to reduce overhead)
// Consider: Distributed health checkers per region
healthcheck.NewHealthChecker(backends, 10*time.Second)
```

---

## Next Steps

Want to implement these optimizations? Priority order:

1. ✅ **Add Connection Pooling** (30 lines of code)
2. ✅ **Add WaitGroup** (10 lines of code)
3. 🟡 **Add Exponential Backoff** (20 lines of code)
4. 🟡 **Add Metrics** (logging + prometheus)

Let me know which optimizations you'd like to implement!

