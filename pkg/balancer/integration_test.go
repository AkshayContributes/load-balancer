package balancer

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akshaykumarthakur/load-balancer/internal/backend"
)

// TestRoundRobinDistribution tests that requests are distributed in round-robin fashion
func TestRoundRobinDistribution(t *testing.T) {
	// Create 3 test backend servers
	servers := []*httptest.Server{
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Server 1")
		})),
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Server 2")
		})),
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Server 3")
		})),
	}
	defer func() {
		for _, s := range servers {
			s.Close()
		}
	}()

	// Create backends from test servers
	backends := make([]*backend.Backend, len(servers))
	for i, server := range servers {
		backends[i] = backend.NewBackend(server.URL)
		backends[i].SetAlive(true)
	}

	lb, err := New(backends)
	if err != nil {
		t.Fatalf("Failed to create load balancer: %v", err)
	}

	// Test 1: Sequential selection should rotate through backends
	t.Run("Sequential Selection", func(t *testing.T) {
		expected := []int{0, 1, 2, 0, 1, 2}
		for i, expectedIdx := range expected {
			selected, err := lb.SelectBackend()
			if err != nil {
				t.Fatalf("Request %d failed: %v", i, err)
			}

			if selected != backends[expectedIdx] {
				t.Errorf("Request %d: expected backend %d, got different", i, expectedIdx)
			}
		}
	})

	// Test 2: Verify all backends can serve requests
	t.Run("All Backends Serve", func(t *testing.T) {
		served := make(map[*backend.Backend]bool)
		for i := 0; i < 10; i++ {
			selected, err := lb.SelectBackend()
			if err != nil {
				t.Fatalf("Request %d failed: %v", i, err)
			}
			served[selected] = true
		}

		if len(served) != 3 {
			t.Errorf("Expected all 3 backends to serve, but only %d did", len(served))
		}
	})
}

// TestBackendFailureHandling tests that failed backends are skipped
func TestBackendFailureHandling(t *testing.T) {
	backends := []*backend.Backend{
		backend.NewBackend("http://localhost:3000"),
		backend.NewBackend("http://localhost:3001"),
		backend.NewBackend("http://localhost:3002"),
	}

	// Mark all as alive initially
	for _, b := range backends {
		b.SetAlive(true)
	}

	lb, err := New(backends)
	if err != nil {
		t.Fatalf("Failed to create load balancer: %v", err)
	}

	t.Run("Skip Dead Backends", func(t *testing.T) {
		// Mark backend 1 as dead
		backends[1].SetAlive(false)

		// Make multiple requests - should skip backend 1
		for i := 0; i < 10; i++ {
			selected, err := lb.SelectBackend()
			if err != nil {
				t.Fatalf("Request %d failed: %v", i, err)
			}

			if selected == backends[1] {
				t.Errorf("Request %d: selected dead backend", i)
			}
		}
	})

	t.Run("Backends 0 and 2 Get Equal Load", func(t *testing.T) {
		// Reset - backend 1 still dead
		count := make(map[*backend.Backend]int)
		for i := 0; i < 100; i++ {
			selected, err := lb.SelectBackend()
			if err != nil {
				t.Fatalf("Request %d failed: %v", i, err)
			}
			count[selected]++
		}

		// Backend 1 should have 0
		if count[backends[1]] != 0 {
			t.Errorf("Backend 1 should be skipped, but got %d requests", count[backends[1]])
		}

		// Backends 0 and 2 should have roughly equal distribution
		ratio := float64(count[backends[0]]) / float64(count[backends[2]])
		if ratio < 0.8 || ratio > 1.2 {
			t.Errorf("Uneven distribution: 0=%d, 2=%d (ratio=%.2f)", 
				count[backends[0]], count[backends[2]], ratio)
		}
	})

	t.Run("Recovery Restores Backend", func(t *testing.T) {
		// Restore backend 1
		backends[1].SetAlive(true)

		// Now backend 1 should start receiving requests again
		received := false
		for i := 0; i < 20; i++ {
			selected, err := lb.SelectBackend()
			if err != nil {
				t.Fatalf("Request %d failed: %v", i, err)
			}
			if selected == backends[1] {
				received = true
				break
			}
		}

		if !received {
			t.Error("Backend 1 was not selected after recovery")
		}
	})
}

// TestAllBackendsDown tests error handling when all backends are offline
func TestAllBackendsDown(t *testing.T) {
	backends := []*backend.Backend{
		backend.NewBackend("http://localhost:3000"),
		backend.NewBackend("http://localhost:3001"),
		backend.NewBackend("http://localhost:3002"),
	}

	// Mark all as dead
	for _, b := range backends {
		b.SetAlive(false)
	}

	lb, err := New(backends)
	if err != nil {
		t.Fatalf("Failed to create load balancer: %v", err)
	}

	_, err = lb.SelectBackend()
	if err == nil {
		t.Error("Expected error when all backends are down")
	}

	if err.Error() != "all backends are offline" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

// TestConcurrentRequests tests that concurrent requests are handled fairly
func TestConcurrentRequests(t *testing.T) {
	backends := []*backend.Backend{
		backend.NewBackend("http://localhost:3000"),
		backend.NewBackend("http://localhost:3001"),
		backend.NewBackend("http://localhost:3002"),
	}

	for _, b := range backends {
		b.SetAlive(true)
	}

	lb, err := New(backends)
	if err != nil {
		t.Fatalf("Failed to create load balancer: %v", err)
	}

	t.Run("1000 Concurrent Requests", func(t *testing.T) {
		count := make(map[*backend.Backend]int)
		var mu sync.Mutex
		var wg sync.WaitGroup

		numRequests := 1000
		for i := 0; i < numRequests; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				selected, err := lb.SelectBackend()
				if err != nil {
					t.Errorf("Request failed: %v", err)
					return
				}

				mu.Lock()
				count[selected]++
				mu.Unlock()
			}()
		}

		wg.Wait()

		// Check distribution is roughly equal
		total := 0
		for _, c := range count {
			total += c
		}

		if total != numRequests {
			t.Errorf("Expected %d total requests, got %d", numRequests, total)
		}

		// Each backend should get approximately 1/3
		expectedPerBackend := numRequests / len(backends)
		tolerance := expectedPerBackend / 5 // 20% tolerance

		for i, b := range backends {
			c := count[b]
			if c < expectedPerBackend-tolerance || c > expectedPerBackend+tolerance {
				t.Errorf("Backend %d got %d requests, expected ~%d (±%d)",
					i, c, expectedPerBackend, tolerance)
			}
		}
	})
}

// TestPartialFailureDuringConcurrentLoad tests recovery during load
func TestPartialFailureDuringConcurrentLoad(t *testing.T) {
	backends := []*backend.Backend{
		backend.NewBackend("http://localhost:3000"),
		backend.NewBackend("http://localhost:3001"),
		backend.NewBackend("http://localhost:3002"),
	}

	for _, b := range backends {
		b.SetAlive(true)
	}

	lb, err := New(backends)
	if err != nil {
		t.Fatalf("Failed to create load balancer: %v", err)
	}

	var wg sync.WaitGroup
	successCount := atomic.Int64{}
	failureCount := atomic.Int64{}

	// Start 100 concurrent requests
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Simulate a request
			selected, err := lb.SelectBackend()
			if err != nil {
				failureCount.Add(1)
				return
			}

			if selected.IsAlive() {
				successCount.Add(1)
			}
		}()

		// After 30 requests, kill backend 1
		if i == 30 {
			backends[1].SetAlive(false)
		}

		// After 70 requests, restore backend 1
		if i == 70 {
			backends[1].SetAlive(true)
		}
	}

	wg.Wait()

	// All requests should succeed (backend selection, not actual HTTP call)
	total := successCount.Load() + failureCount.Load()
	if failureCount.Load() > 0 {
		t.Errorf("Expected no failures, got %d failures out of %d requests",
			failureCount.Load(), total)
	}
}

// TestHealthStatusChanges tests that health status changes are reflected immediately
func TestHealthStatusChanges(t *testing.T) {
	backends := []*backend.Backend{
		backend.NewBackend("http://localhost:3000"),
		backend.NewBackend("http://localhost:3001"),
	}

	for _, b := range backends {
		b.SetAlive(true)
	}

	lb, err := New(backends)
	if err != nil {
		t.Fatalf("Failed to create load balancer: %v", err)
	}

	t.Run("Immediate State Change", func(t *testing.T) {
		// Get initial state
		healthy := lb.GetHealthyBackends()
		if len(healthy) != 2 {
			t.Errorf("Expected 2 healthy backends, got %d", len(healthy))
		}

		// Kill one backend
		backends[0].SetAlive(false)

		// Next selection should skip it
		selected, err := lb.SelectBackend()
		if err != nil {
			t.Fatalf("Selection failed: %v", err)
		}

		if selected == backends[0] {
			t.Error("Selected dead backend immediately after marking dead")
		}

		// Restore it
		backends[0].SetAlive(true)

		// Should be selectable again
		found := false
		for i := 0; i < 10; i++ {
			selected, err := lb.SelectBackend()
			if err != nil {
				t.Fatalf("Selection %d failed: %v", i, err)
			}
			if selected == backends[0] {
				found = true
				break
			}
		}

		if !found {
			t.Error("Backend 0 not selected after recovery in 10 attempts")
		}
	})
}

// TestLoadBalancerCreation tests that load balancer validates input
func TestLoadBalancerCreation(t *testing.T) {
	t.Run("No Backends Error", func(t *testing.T) {
		_, err := New([]*backend.Backend{})
		if err == nil {
			t.Error("Expected error when creating load balancer with no backends")
		}
	})

	t.Run("Single Backend", func(t *testing.T) {
		backends := []*backend.Backend{
			backend.NewBackend("http://localhost:3000"),
		}
		backends[0].SetAlive(true)

		lb, err := New(backends)
		if err != nil {
			t.Fatalf("Failed to create load balancer: %v", err)
		}

		selected, err := lb.SelectBackend()
		if err != nil {
			t.Fatalf("Selection failed: %v", err)
		}

		if selected != backends[0] {
			t.Error("Single backend not selected")
		}
	})

	t.Run("Many Backends", func(t *testing.T) {
		backends := make([]*backend.Backend, 100)
		for i := 0; i < 100; i++ {
			backends[i] = backend.NewBackend(fmt.Sprintf("http://localhost:%d", 3000+i))
			backends[i].SetAlive(true)
		}

		lb, err := New(backends)
		if err != nil {
			t.Fatalf("Failed to create load balancer: %v", err)
		}

		// Test round-robin works with many backends
		for i := 0; i < 100; i++ {
			selected, err := lb.SelectBackend()
			if err != nil {
				t.Fatalf("Selection failed: %v", err)
			}

			if selected != backends[i] {
				t.Errorf("Expected backend %d, got different", i)
			}
		}
	})
}

// TestStressTest performs a high-concurrency stress test
func TestStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	backends := []*backend.Backend{
		backend.NewBackend("http://localhost:3000"),
		backend.NewBackend("http://localhost:3001"),
		backend.NewBackend("http://localhost:3002"),
		backend.NewBackend("http://localhost:3003"),
		backend.NewBackend("http://localhost:3004"),
	}

	for _, b := range backends {
		b.SetAlive(true)
	}

	lb, err := New(backends)
	if err != nil {
		t.Fatalf("Failed to create load balancer: %v", err)
	}

	// Run 10,000 concurrent requests
	numRequests := 10000
	count := make(map[*backend.Backend]int)
	var mu sync.Mutex
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			selected, err := lb.SelectBackend()
			if err != nil {
				t.Errorf("Request failed: %v", err)
				return
			}

			mu.Lock()
			count[selected]++
			mu.Unlock()
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	// Verify all requests succeeded
	total := 0
	for _, c := range count {
		total += c
	}

	if total != numRequests {
		t.Errorf("Expected %d total requests, got %d", numRequests, total)
	}

	// Check distribution
	t.Logf("Stress test completed: %d requests in %v (%.0f req/ms)", 
		numRequests, duration, float64(numRequests)/duration.Seconds()/1000)

	for i, b := range backends {
		t.Logf("Backend %d: %d requests (%.1f%%)", 
			i, count[b], float64(count[b])/float64(numRequests)*100)
	}
}

