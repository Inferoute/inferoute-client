package server

import (
	"context"
	"time"
)

// slotAcquirePollInterval is how often a queued same-session request re-tries
// the inference slot. The queue has depth 1 in practice, so polling is fine.
const slotAcquirePollInterval = 100 * time.Millisecond

// isBusy is true when every inference slot is taken.
// nvidia-smi utilization is not an admission check: it is a trailing window
// and stays high for about a second after this process's own forward.
func (s *Server) isBusy() (bool, error) {
	return s.slotsBusy(), nil
}

// slotsBusy reports whether all inference slots are taken.
func (s *Server) slotsBusy() bool {
	max := s.maxInflight.Load()
	return max > 0 && s.inflight.Load() >= max
}

// sessionMemory is how long a conversation still counts as "served here".
const sessionMemory = 15 * time.Minute

func (s *Server) isActiveSession(key string) bool {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	seen, ok := s.sessions[key]
	return ok && time.Since(seen) <= sessionMemory
}

// setActiveSession remembers this conversation. Empty keys are ignored:
// a request without a session does not erase the others still in flight.
func (s *Server) setActiveSession(key string) {
	if key == "" {
		return
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.sessions == nil {
		s.sessions = map[string]time.Time{}
	}
	now := time.Now()
	s.sessions[key] = now
	cutoff := now.Add(-sessionMemory)
	for k, seen := range s.sessions {
		if seen.Before(cutoff) {
			delete(s.sessions, k)
		}
	}
}

// tryAcquireInference reserves an inference slot. maxInflight <= 0 means unlimited.
func (s *Server) tryAcquireInference() bool {
	for {
		n := s.inflight.Load()
		max := s.maxInflight.Load()
		if max > 0 && n >= max {
			return false
		}
		if s.inflight.CompareAndSwap(n, n+1) {
			s.notifyCapacityChange()
			return true
		}
	}
}

// acquireInferenceWait reserves a slot, waiting up to maxWait for one to free.
// Used for same-session turns, which are serial by nature: the previous turn
// is usually still decoding when the next arrives.
func (s *Server) acquireInferenceWait(ctx context.Context, maxWait time.Duration) bool {
	if s.tryAcquireInference() {
		return true
	}

	deadline := time.Now().Add(maxWait)
	ticker := time.NewTicker(slotAcquirePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if s.tryAcquireInference() {
				return true
			}
			if time.Now().After(deadline) {
				return false
			}
		}
	}
}

func (s *Server) releaseInference() {
	s.inflight.Add(-1)
	s.notifyCapacityChange()
}

// notifyCapacityChange asks the capacity sender to push. Non-blocking;
// the sender coalesces bursts into one in-flight POST.
func (s *Server) notifyCapacityChange() {
	if s.onCapacity != nil {
		s.onCapacity()
	}
}
