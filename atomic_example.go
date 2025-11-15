package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Example 1: Demonstrate race condition without atomic
func exampleWithoutAtomic() {
	fmt.Println("❌ WITHOUT ATOMIC (RACE CONDITION)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	var counter uint64 = 0
	var wg sync.WaitGroup

	// 10 goroutines each incrementing 1000 times
	numGoroutines := 10
	incrementsPerGoroutine := 1000

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				// NOT SAFE! This is a race condition
				counter = counter + 1
			}
		}()
	}

	wg.Wait()

	expected := uint64(numGoroutines * incrementsPerGoroutine)
	fmt.Printf("Expected: %d\n", expected)
	fmt.Printf("Got:      %d\n", counter)
	fmt.Printf("Lost:     %d increments (BUG!)\n\n", expected-counter)
}

// Example 2: Demonstrate safety with atomic
func exampleWithAtomic() {
	fmt.Println("✅ WITH ATOMIC (SAFE)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	var counter atomic.Uint64
	var wg sync.WaitGroup

	// Same 10 goroutines
	numGoroutines := 10
	incrementsPerGoroutine := 1000

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				// SAFE! Atomic guarantees no lost updates
				counter.Add(1)
			}
		}()
	}

	wg.Wait()

	expected := uint64(numGoroutines * incrementsPerGoroutine)
	fmt.Printf("Expected: %d\n", expected)
	fmt.Printf("Got:      %d\n", counter.Load())
	if counter.Load() == expected {
		fmt.Println("✓ Perfect! All increments counted.\n")
	}
}

// Example 3: Demonstrate round-robin behavior
func exampleRoundRobin() {
	fmt.Println("🔄 ROUND-ROBIN WITH ATOMIC")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	var index atomic.Uint64
	backends := []string{"Backend A", "Backend B", "Backend C"}

	fmt.Printf("Total backends: %d\n\n", len(backends))

	// Simulate 15 requests
	for i := 0; i < 15; i++ {
		// This is what SelectBackend() does
		idx := index.Add(1) - 1
		idx = idx % uint64(len(backends))

		fmt.Printf("Request %2d: index.Add(1) returned %d → idx = %d → %s\n",
			i+1, index.Load(), idx, backends[idx])
	}

	fmt.Println()
}

// Example 4: Compare performance
func examplePerformance() {
	fmt.Println("⚡ PERFORMANCE COMPARISON")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	iterations := 1000000

	// With Mutex
	var mutexCounter uint64 = 0
	var mu sync.Mutex

	start := time.Now()
	for i := 0; i < iterations; i++ {
		mu.Lock()
		mutexCounter++
		mu.Unlock()
	}
	mutexDuration := time.Since(start)

	// With Atomic
	var atomicCounter atomic.Uint64

	start = time.Now()
	for i := 0; i < iterations; i++ {
		atomicCounter.Add(1)
	}
	atomicDuration := time.Since(start)

	fmt.Printf("Mutex:    %v (%d ns/op)\n", mutexDuration, mutexDuration.Nanoseconds()/int64(iterations))
	fmt.Printf("Atomic:   %v (%d ns/op)\n", atomicDuration, atomicDuration.Nanoseconds()/int64(iterations))
	fmt.Printf("Speedup:  %.1fx faster\n\n", float64(mutexDuration)/float64(atomicDuration))
}

// Example 5: Concurrent requests to load balancer simulation
func exampleConcurrentRequests() {
	fmt.Println("🎯 SIMULATING CONCURRENT REQUESTS")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	var index atomic.Uint64
	backends := []string{"Backend 1", "Backend 2", "Backend 3"}
	var wg sync.WaitGroup

	// Track which backends got selected
	selections := make([]int, len(backends))
	var mu sync.Mutex

	numRequests := 300
	numConcurrent := 10

	fmt.Printf("Simulating %d concurrent requests to %d backends\n\n", numRequests, len(backends))

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Get next backend using atomic counter
			idx := index.Add(1) - 1
			idx = idx % uint64(len(backends))

			// Record selection
			mu.Lock()
			selections[idx]++
			mu.Unlock()
		}()

		// Limit concurrent goroutines for demo
		if (i+1)%numConcurrent == 0 {
			wg.Wait()
		}
	}

	wg.Wait()

	fmt.Println("Distribution (should be roughly equal):")
	for i, count := range selections {
		percentage := float64(count) / float64(numRequests) * 100
		bar := ""
		for j := 0; j < count/10; j++ {
			bar += "█"
		}
		fmt.Printf("  %s: %3d requests (%.1f%%) %s\n", backends[i], count, percentage, bar)
	}
	fmt.Println()
}

func main() {
	divider := "══════════════════════════════════════════════════════════════════════"
	fmt.Println("\n" + divider)
	fmt.Println("ATOMIC.UINT64 EXAMPLES")
	fmt.Println(divider + "\n")

	exampleWithoutAtomic()
	exampleWithAtomic()
	exampleRoundRobin()
	examplePerformance()
	exampleConcurrentRequests()

	fmt.Println(divider)
	fmt.Println("KEY TAKEAWAY:")
	fmt.Println("atomic.Uint64 ensures thread-safe, fair, fast round-robin distribution!")
	fmt.Println(divider + "\n")
}

