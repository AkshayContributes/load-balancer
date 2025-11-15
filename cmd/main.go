package main

import (
	"fmt"
	"log"

	"github.com/akshaykumarthakur/load-balancer/internal/backend"
	"github.com/akshaykumarthakur/load-balancer/pkg/balancer"
)

func main() {
	// Create backends
	backends := []*backend.Backend{
		backend.NewBackend("http://localhost:3000"),
		backend.NewBackend("http://localhost:3001"),
		backend.NewBackend("http://localhost:3002"),
	}

	// Mark all as alive
	for _, b := range backends {
		b.SetAlive(true)
	}

	// Create load balancer
	lb, err := balancer.New(backends)
	if err != nil {
		log.Fatalf("Failed to create load balancer: %v", err)
	}

	fmt.Println("=== Load Balancer Demo ===\n")

	// Test 1: Normal round-robin
	fmt.Println("Test 1: Round-Robin with all servers healthy")
	for i := 1; i <= 6; i++ {
		selected, err := lb.SelectBackend()
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}
		fmt.Printf("Request %d → %s\n", i, selected.URL.Host)
	}

	// Test 2: Server goes down
	fmt.Println("\nTest 2: Backend at :3001 goes offline")
	backends[1].SetAlive(false)
	for i := 7; i <= 12; i++ {
		selected, err := lb.SelectBackend()
		if err != nil {
			log.Printf("Request %d failed: %v", i, err)
			continue
		}
		fmt.Printf("Request %d → %s\n", i, selected.URL.Host)
	}

	fmt.Println("\n=== Demo Complete ===")
}
