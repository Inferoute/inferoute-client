package gpu

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})
	os.Exit(m.Run())
}

func testMonitor(ttl time.Duration, query func() (*GPUInfo, error)) *Monitor {
	return &Monitor{cacheTTL: ttl, queryFn: query}
}

func TestGetGPUInfoCachesAndCopies(t *testing.T) {
	var n atomic.Int32
	m := testMonitor(time.Second, func() (*GPUInfo, error) {
		n.Add(1)
		return &GPUInfo{ProductName: "x", Utilization: 11, IsBusy: false}, nil
	})

	a, err := m.GetGPUInfo()
	if err != nil {
		t.Fatalf("GetGPUInfo() first call: %v", err)
	}
	b, err := m.GetGPUInfo()
	if err != nil {
		t.Fatalf("GetGPUInfo() cached call: %v", err)
	}
	if got := n.Load(); got != 1 {
		t.Errorf("queries = %d, want 1", got)
	}
	if a.ProductName != "x" || b.ProductName != "x" {
		t.Errorf("ProductName a=%q b=%q, want x", a.ProductName, b.ProductName)
	}

	a.IsBusy = true
	c, err := m.GetGPUInfo()
	if err != nil {
		t.Fatalf("GetGPUInfo() after mutation: %v", err)
	}
	if c.IsBusy {
		t.Fatal("caller mutation of IsBusy leaked into cache")
	}
}

func TestGetGPUInfoCacheExpires(t *testing.T) {
	var n atomic.Int32
	m := testMonitor(20*time.Millisecond, func() (*GPUInfo, error) {
		n.Add(1)
		return &GPUInfo{ProductName: "x"}, nil
	})

	if _, err := m.GetGPUInfo(); err != nil {
		t.Fatalf("GetGPUInfo() first call: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := m.GetGPUInfo(); err != nil {
		t.Fatalf("GetGPUInfo() after expiry: %v", err)
	}
	if got := n.Load(); got != 2 {
		t.Errorf("queries = %d, want 2 after TTL expiry", got)
	}
}

func TestGetGPUInfoCachesError(t *testing.T) {
	var n atomic.Int32
	queryErr := errors.New("boom")
	m := testMonitor(time.Second, func() (*GPUInfo, error) {
		n.Add(1)
		return nil, queryErr
	})

	_, err1 := m.GetGPUInfo()
	_, err2 := m.GetGPUInfo()
	if err1 == nil || err2 == nil {
		t.Fatalf("GetGPUInfo() errors = %v, %v, want cached error", err1, err2)
	}
	if got := n.Load(); got != 1 {
		t.Errorf("queries = %d, want 1 (error cached)", got)
	}
}

func TestGetGPUInfoSerializesRefresh(t *testing.T) {
	var n atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	m := testMonitor(time.Second, func() (*GPUInfo, error) {
		if n.Add(1) == 1 {
			close(started)
			<-release
		}
		return &GPUInfo{ProductName: "x"}, nil
	})

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.GetGPUInfo(); err != nil {
				errs <- err
			}
		}()
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first query never started")
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("GetGPUInfo() concurrent: %v", err)
	}
	if got := n.Load(); got != 1 {
		t.Errorf("queries = %d, want 1 (mutex serializes refresh)", got)
	}
}

func TestIsBusyUsesCachedGetGPUInfo(t *testing.T) {
	var n atomic.Int32
	m := testMonitor(time.Second, func() (*GPUInfo, error) {
		n.Add(1)
		return &GPUInfo{Utilization: 50, IsBusy: true}, nil
	})

	busy, err := m.IsBusy()
	if err != nil {
		t.Fatalf("IsBusy() = _, %v", err)
	}
	if !busy {
		t.Fatal("IsBusy() = false, want true")
	}
	if _, err := m.IsBusy(); err != nil {
		t.Fatalf("IsBusy() cached: %v", err)
	}
	if got := n.Load(); got != 1 {
		t.Errorf("queries = %d, want 1", got)
	}
}
