package src

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	// "sync"
	"time"
)

// ensureMemBudget initializes memBudgetBytes by reading cgroup v2 memory.max if available.
func (s *Server) ensureMemBudget() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.memBudgetBytes > 0 {
		return nil
	}
	max, err := readCgroupMemoryMax(s.cfg.CgroupV2Root)
	if err != nil {
		return err
	}
	if max <= 0 {
		// Unlimited; disable gating effectively
		s.memBudgetBytes = 0
		return nil
	}
	s.memBudgetBytes = max
	return nil
}

// acquireMemory attempts to acquire memory for a job with the given estimate
func (s *Server) acquireMemory(ctx context.Context, estimateBytes int64) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		// fast path: try lock + check
		s.mu.Lock()
		max := s.memBudgetBytes
		s.mu.Unlock()

		if max == 0 { // unlimited or not configured
			s.mu.Lock()
			s.memReservedBytes += estimateBytes
			s.mu.Unlock()
			return nil
		}

		cur, err := readCgroupMemoryCurrent(s.cfg.CgroupV2Root)
		if err != nil {
			// if cannot read, be safe and wait a bit
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				continue
			}
		}

		// Attempt reservation atomically
		s.mu.Lock()
		if cur+s.memReservedBytes+estimateBytes <= max {
			s.memReservedBytes += estimateBytes
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// releaseMemory releases the memory reservation for a job
func (s *Server) releaseMemory(estimateBytes int64) {
	s.mu.Lock()
	if estimateBytes > s.memReservedBytes {
		s.memReservedBytes = 0
	} else {
		s.memReservedBytes -= estimateBytes
	}
	s.mu.Unlock()
}

// readCgroupMemoryMax reads the memory.max value from cgroups v2
func readCgroupMemoryMax(root string) (int64, error) {
	if root == "" {
		return 0, fmt.Errorf("cgroup root not set")
	}
	b, err := os.ReadFile(filepath.Join(root, "memory.max"))
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	if s == "max" {
		return 0, nil
	}
	var v int64
	_, err = fmt.Sscanf(s, "%d", &v)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// readCgroupMemoryCurrent reads the memory.current value from cgroups v2
func readCgroupMemoryCurrent(root string) (int64, error) {
	b, err := os.ReadFile(filepath.Join(root, "memory.current"))
	if err != nil {
		return 0, err
	}
	var v int64
	_, err = fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &v)
	if err != nil {
		return 0, err
	}
	return v, nil
}