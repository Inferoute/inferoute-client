package capacity

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/nodeapi"
	"go.uber.org/zap"
)

const (
	heartbeat   = 20 * time.Second
	postTimeout = 5 * time.Second
)

// Slots is the client's own counter. Accepting is false during a benchmark.
type Slots func() (inflight, maxInflight int32, accepting bool)

// Sender posts capacity straight to the platform. One POST at a time;
// bursts collapse into the next send.
type Sender struct {
	url    string
	apiKey string
	slots  Slots
	engine llm.Client
	client *http.Client

	instance string
	seq      atomic.Int64
	kick     chan struct{}
}

func NewSender(platformURL, apiKey string, slots Slots, engine llm.Client) *Sender {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return &Sender{
		url:      strings.TrimRight(platformURL, "/"),
		apiKey:   apiKey,
		slots:    slots,
		engine:   engine,
		client:   nodeapi.NewHTTPClient(postTimeout),
		instance: hex.EncodeToString(buf[:]),
		kick:     make(chan struct{}, 1),
	}
}

// Notify requests a send. Non-blocking.
func (s *Sender) Notify() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Loop sends on every kick and on a heartbeat. Failures are logged and dropped.
func (s *Sender) Loop(ctx context.Context) {
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
			s.send(ctx)
		case <-tick.C:
			s.send(ctx)
		}
	}
}

type payload struct {
	Instance      string   `json:"instance"`
	Seq           int64    `json:"seq"`
	Inflight      int32    `json:"inflight"`
	MaxInflight   int32    `json:"max_inflight"`
	EngineWaiting *int32   `json:"engine_waiting,omitempty"`
	KVCacheUsage  *float64 `json:"kv_cache_usage,omitempty"`
	Accepting     bool     `json:"accepting"`
}

func (s *Sender) send(ctx context.Context) {
	if s.slots == nil {
		return
	}
	inflight, maxInflight, accepting := s.slots()
	body := payload{
		Instance:    s.instance,
		Seq:         s.seq.Add(1),
		Inflight:    inflight,
		MaxInflight: maxInflight,
		Accepting:   accepting,
	}
	if src, ok := s.engine.(interface {
		EngineCapacity(context.Context) (llm.EngineCapacity, error)
	}); ok {
		probe, cancel := context.WithTimeout(ctx, 2*time.Second)
		got, err := src.EngineCapacity(probe)
		cancel()
		if err == nil {
			body.EngineWaiting = got.Waiting
			body.KVCacheUsage = got.KVCacheUsage
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, postTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, s.url+"/api/provider/capacity", bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		logger.Debug("capacity post failed", zap.Error(err))
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		logger.Debug("capacity post rejected", zap.Int("status", resp.StatusCode), zap.String("detail", fmt.Sprint(resp.Status)))
	}
}
