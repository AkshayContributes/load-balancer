package main

import (
	"fmt"
	"log"
	"time"

	"github.com/akshaykumarthakur/load-balancer/internal/backend"
	"github.com/akshaykumarthakur/load-balancer/internal/healthcheck"
	"github.com/akshaykumarthakur/load-balancer/pkg/balancer"
)

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

	// Start health checker (queries every 5 seconds)
	healthChecker := healthcheck.NewHealthChecker(backends, 5*time.Second)
	healthChecker.Start()
	defer healthChecker.Stop()

	fmt.Println("=== Load Balancer Demo ===")
	fmt.Println()

	// Initial health check happens immediately
	fmt.Println("Test 1: Initial round-robin (servers being checked...)")
	for i := 1; i <= 6; i++ {
		selected, err := lb.SelectBackend()
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}
		fmt.Printf("Request %d → %s\n", i, selected.URL.Host)
	}

	// Simulate a server going down after 2 seconds
	fmt.Println("\nTest 2: Simulating server crash in 2 seconds...")
	go func() {
		time.Sleep(2 * time.Second)
		fmt.Println("\n⚠️  Simulating crash of backend :3001")
		backends[1].SetAlive(false)
	}()

	// Wait a bit for the crash to happen
	time.Sleep(3 * time.Second)

	// Now make more requests
	fmt.Println("\nTest 3: After server goes down (should skip :3001)")
	for i := 7; i <= 12; i++ {
		selected, err := lb.SelectBackend()
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}
		fmt.Printf("Request %d → %s\n", i, selected.URL.Host)
	}

	// Simulate recovery
	fmt.Println("\nTest 4: Simulating server recovery in 2 seconds...")
	go func() {
		time.Sleep(2 * time.Second)
		fmt.Println("\n✅ Server :3001 is recovering")
		backends[1].SetAlive(true)
	}()

	// Wait for recovery
	time.Sleep(3 * time.Second)

	// Make requests again
	fmt.Println("\nTest 5: After server recovers (should include :3001 again)")
	for i := 13; i <= 18; i++ {
		selected, err := lb.SelectBackend()
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}
		fmt.Printf("Request %d → %s\n", i, selected.URL.Host)
	}

	fmt.Println("\n=== Demo Complete ===")
	fmt.Println("\nIn production:")
	fmt.Println("• HealthChecker queries /health endpoint automatically")
	fmt.Println("• No manual SetAlive() calls needed")
	fmt.Println("• Servers automatically marked alive/dead")
	fmt.Println("• Recovery detected automatically")
}
