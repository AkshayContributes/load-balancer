package balancer

import (
	"fmt"
	"sync/atomic"

	"github.com/akshaykumarthakur/load-balancer/internal/backend"
)

type LoadBalancer struct {
	backends []*backend.Backend
	current  atomic.Uint64
}

func New(backends []*backend.Backend) (*LoadBalancer, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("at least one backend is required")
	}

	return &LoadBalancer{
		backends: backends,
		current:  atomic.Uint64{},
	}, nil
}

func (lb *LoadBalancer) SelectBackend() (*backend.Backend, error) {
	attempts := 0
	totalBackends := len(lb.backends)

	for attempts < totalBackends {
		idx := lb.current.Add(1) - 1
		idx = idx % uint64(totalBackends)

		selectedBackend := lb.backends[idx]
		if selectedBackend.IsAlive() {
			return selectedBackend, nil
		}

		attempts++
	}

	return nil, fmt.Errorf("all backends are offline")
}

// GetHealthyBackends returns only the backends that are currently alive.
func (lb *LoadBalancer) GetHealthyBackends() []*backend.Backend {
	var healthy []*backend.Backend
	for _, b := range lb.backends {
		if b.IsAlive() {
			healthy = append(healthy, b)
		}
	}
	return healthy
}
