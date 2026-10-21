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
	return s.maxInflight > 0 && s.inflight.Load() >= s.maxInflight
}

// isActiveSession reports whether the key matches the session of the
// in-flight or last-completed inference — the session whose KV cache is warm.
func (s *Server) isActiveSession(key string) bool {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	return s.activeSessionKey != "" && s.activeSessionKey == key
}

// setActiveSession records which session owns the KV cache now. A request
// without a session key clears it: that request overwrote the cache anyway.
func (s *Server) setActiveSession(key string) {
	s.sessionMu.Lock()
	s.activeSessionKey = key
	s.sessionMu.Unlock()
}

// tryAcquireInference reserves an inference slot. maxInflight <= 0 means unlimited.
func (s *Server) tryAcquireInference() bool {
	for {
		n := s.inflight.Load()
		if s.maxInflight > 0 && n >= s.maxInflight {
			return false
		}
		if s.inflight.CompareAndSwap(n, n+1) {
			s.notifyBusyIfChanged()
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
	s.notifyBusyIfChanged()
}

// notifyBusyIfChanged tells the health reporter when the slot-busy state
// flips, so the central busy flag is seconds — not minutes — stale.
func (s *Server) notifyBusyIfChanged() {
	if s.healthReporter == nil || s.maxInflight <= 0 {
		return
	}
	n := s.inflight.Load()
	// Exactly at the threshold boundary means we just became busy (acquire)
	// or just became free (release). Between boundaries nothing changed.
	if n == s.maxInflight || n == s.maxInflight-1 {
		s.healthReporter.NotifyBusyChange()
	}
}
