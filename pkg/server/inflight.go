package server

// gpuBusy reports NVIDIA utilization busy (Linux/Windows). Always false on macOS
// and when no GPU monitor is available.
func (s *Server) gpuBusy() (bool, error) {
	if s.gpuMonitor == nil {
		return false, nil
	}
	return s.gpuMonitor.IsBusy()
}

// isBusy is true if a slot is already taken or the GPU reports busy.
func (s *Server) isBusy() (bool, error) {
	if s.maxInflight > 0 && s.inflight.Load() >= s.maxInflight {
		return true, nil
	}
	return s.gpuBusy()
}

// tryAcquireInference reserves an inference slot. maxInflight <= 0 means unlimited.
func (s *Server) tryAcquireInference() bool {
	for {
		n := s.inflight.Load()
		if s.maxInflight > 0 && n >= s.maxInflight {
			return false
		}
		if s.inflight.CompareAndSwap(n, n+1) {
			return true
		}
	}
}

func (s *Server) releaseInference() {
	s.inflight.Add(-1)
}
