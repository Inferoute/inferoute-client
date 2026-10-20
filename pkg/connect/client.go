package connect

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/nodeapi"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/version"
	"go.uber.org/zap"
)

// ErrInvalidAPIKey is returned when the platform rejects the provider key.
var ErrInvalidAPIKey = errors.New("invalid provider API key")

// Client keeps one outbound yamux session to connect-gateway.
type Client struct {
	PlatformURL string
	APIKey      string
	Handler     http.Handler
	// OnConnect runs after each successful upgrade, including reconnects.
	OnConnect func()

	connected atomic.Bool
	up        chan error
}

func New(platformURL, apiKey string, handler http.Handler, onConnect func()) *Client {
	return &Client{
		PlatformURL: platformURL,
		APIKey:      apiKey,
		Handler:     handler,
		OnConnect:   onConnect,
		up:          make(chan error, 1),
	}
}

func (c *Client) Connected() bool {
	return c.connected.Load()
}

// DialURL turns https://host into wss://host/api/connect/ws.
func DialURL(platform string) (string, error) {
	u, err := url.Parse(platform)
	if err != nil {
		return "", fmt.Errorf("platform url: %w", err)
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	case "http", "ws":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported platform url scheme %q", u.Scheme)
	}
	u.Path = "/api/connect/ws"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// Run dials until ctx is cancelled. The first 401 is returned so startup can exit.
// Later disconnects retry with jittered backoff.
func (c *Client) Run(ctx context.Context) error {
	wsURL, err := DialURL(c.PlatformURL)
	if err != nil {
		return err
	}
	backoff := time.Second
	first := true
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = c.session(ctx, wsURL)
		if errors.Is(err, ErrInvalidAPIKey) {
			c.notify(err)
			return err
		}
		c.connected.Store(false)
		if first {
			first = false
			c.notify(nil)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logger.Warn("provider connection ended", zap.Error(err))
		wait := backoff + time.Duration(rand.Int63n(int64(backoff/2)+1))
		if !sleep(ctx, wait) {
			return ctx.Err()
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c *Client) notify(err error) {
	select {
	case c.up <- err:
	default:
	}
}

// Ready waits until the first dial finishes or 401s. A timeout means the
// gateway was unreachable; Run keeps retrying.
func (c *Client) Ready(ctx context.Context) error {
	select {
	case err := <-c.up:
		return err
	case <-ctx.Done():
		return nil
	}
}

func (c *Client) session(ctx context.Context, wsURL string) error {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.APIKey)
	header.Set(nodeapi.HeaderClientVersion, version.Version)
	conn, resp, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	if resp != nil && resp.StatusCode == http.StatusUnauthorized {
		return ErrInvalidAPIKey
	}
	if err != nil {
		return err
	}
	netConn := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.ConnectionWriteTimeout = 10 * time.Second
	session, err := yamux.Server(netConn, cfg)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "yamux")
		return err
	}
	c.connected.Store(true)
	c.notify(nil)
	if c.OnConnect != nil {
		go c.OnConnect()
	}
	err = http.Serve(session, c.Handler)
	c.connected.Store(false)
	_ = session.Close()
	return err
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
