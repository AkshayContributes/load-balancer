// +build ignore

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/akshaykumarthakur/load-balancer/internal/backend"
	"github.com/akshaykumarthakur/load-balancer/internal/healthcheck"
	"github.com/akshaykumarthakur/load-balancer/pkg/balancer"
)

// This demo shows the HealthChecker in action with real backend servers
// Run with: go run cmd/demo_healthchecker.go cmd/demo_with_backends.go

func runHealthCheckerDemo() {
	// Create and start backend servers
	backend1 := NewBackendServer(3000, "Backend-1")
	backend2 := NewBackendServer(3001, "Backend-2")
	backend3 := NewBackendServer(3002, "Backend-3")

	backend1.Start()
	backend2.Start()
	backend3.Start()

	// Give servers time to start
	time.Sleep(500 * time.Millisecond)

	// Create load balancer backends
	lbBackends := []*backend.Backend{
		backend.NewBackend("http://localhost:3000"),
		backend.NewBackend("http://localhost:3001"),
		backend.NewBackend("http://localhost:3002"),
	}

	// Create load balancer
	lb, err := balancer.New(lbBackends)
	if err != nil {
		log.Fatalf("Failed to create load balancer: %v", err)
	}

	// Start health checker (queries every 2 seconds for demo)
	healthChecker := healthcheck.NewHealthChecker(lbBackends, 2*time.Second)
	healthChecker.Start()
	defer healthChecker.Stop()

	fmt.Println("=== Health Checker Demo ===\n")

	// Wait for initial health check
	time.Sleep(500 * time.Millisecond)

	// Test 1: All servers healthy
	fmt.Println("Test 1: Round-robin with all servers healthy")
	for i := 1; i <= 6; i++ {
		selected, err := lb.SelectBackend()
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}
		fmt.Printf("Request %d → %s\n", i, selected.URL.Host)
	}

	// Test 2: Crash a server
	fmt.Println("\nTest 2: Crashing Backend-2 (:3001)...")
	backend2.Stop()

	// Wait for health checker to detect the failure
	time.Sleep(3 * time.Second)

	fmt.Println("After health check detected failure (should skip :3001):")
	for i := 7; i <= 12; i++ {
		selected, err := lb.SelectBackend()
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}
		fmt.Printf("Request %d → %s\n", i, selected.URL.Host)
	}

	// Test 3: Recover the server
	fmt.Println("\nTest 3: Recovering Backend-2 (:3001)...")
	backend2.Resume()

	// Wait for health checker to detect recovery
	time.Sleep(3 * time.Second)

	fmt.Println("After health check detected recovery (should include :3001 again):")
	for i := 13; i <= 18; i++ {
		selected, err := lb.SelectBackend()
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}
		fmt.Printf("Request %d → %s\n", i, selected.URL.Host)
	}

	fmt.Println("\n=== Demo Complete ===")
}

