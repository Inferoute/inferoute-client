package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/inferscale"
)

// credential keeps a join code pasted with a label or quotes.
func credential(s string) string {
	s = strings.TrimSpace(s)
	for _, f := range strings.Fields(s) {
		f = strings.Trim(f, "`\"'.,:;")
		if strings.HasPrefix(f, "join-") {
			return f
		}
	}
	return strings.Trim(s, "`\"'")
}

func workerWaitText(joined, need int) string {
	j, n := engine.WorkerSlots(joined, need+1)
	if n < 1 {
		return "Waiting for workers to join"
	}
	if j >= n {
		return fmt.Sprintf("Waiting for the model to load (%d/%d workers joined)", j, n)
	}
	return fmt.Sprintf("Waiting for workers to join (%d/%d)", j, n)
}

func waitForWorkers(ctx context.Context, kind engine.Kind, llmURL, join string, nnodes int, exited <-chan error, set func(string)) error {
	if !engine.PortOpen(ctx, "http://"+join) {
		set("Waiting for the master to accept workers")
		if err := engine.WaitListen(ctx, join, 2*time.Second, exited); err != nil {
			return err
		}
	}
	need := nnodes - 1
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		_, joined := inferscale.Tailnet()
		set(workerWaitText(joined, need))
		if engine.Healthy(ctx, kind, llmURL) {
			return nil
		}
		select {
		case err := <-exited:
			if err != nil {
				return fmt.Errorf("process exited: %w", err)
			}
			return fmt.Errorf("process exited before workers joined")
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for workers to join: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

func onlinePeers() int {
	_, n := inferscale.Tailnet()
	return n
}

func watchWorkers(out io.Writer, kind engine.Kind, llmURL, join string, nnodes int, exited <-chan error) error {
	var mu sync.Mutex
	msg := workerWaitText(0, nnodes-1)
	set := func(s string) {
		mu.Lock()
		msg = s
		mu.Unlock()
	}
	ctx, cancel := context.WithTimeout(context.Background(), engine.DefaultDownloadTimeout)
	defer cancel()
	err := SpinWhileLive(out, func() string {
		mu.Lock()
		defer mu.Unlock()
		return msg
	}, func() error {
		return waitForWorkers(ctx, kind, llmURL, join, nnodes, exited, set)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Engine is ready at %s.\n", llmURL)
	return nil
}

// clusterSettings is what setup shows instead of the single-machine model list.
type clusterSettings struct {
	Role        string
	Name        string
	Model       string
	DisplayName string
	ServiceType string
}

// clusterPlan is replaced in tests. Production reads the cluster the key belongs to.
var clusterPlan = clusterPlanReal

func clusterPlanReal(cfg *config.Config) (clusterSettings, error) {
	key := credential(cfg.Provider.APIKey)
	if strings.HasPrefix(key, "join-") {
		info, err := inferscale.PreviewJoin(cfg.Provider.URL, key)
		if err != nil {
			return clusterSettings{}, fmt.Errorf("join code: %w", err)
		}
		if strings.TrimSpace(info.Model) == "" {
			return clusterSettings{}, fmt.Errorf("cluster %s has no model set. Choose one in the dashboard, then re-run setup", firstNonEmpty(info.Name, "this cluster"))
		}
		return clusterSettings{
			Role: "worker", Name: info.Name, Model: info.Model,
			DisplayName: info.ModelDisplayName, ServiceType: info.ServiceType,
		}, nil
	}
	sess, err := inferscale.FetchSession(cfg.Provider.URL, key)
	if err != nil || sess.Mode != "multi" {
		return clusterSettings{Role: "single"}, err
	}
	if strings.TrimSpace(sess.Model) == "" {
		return clusterSettings{}, fmt.Errorf("cluster %s has no model set. Choose one in the dashboard, then re-run setup", firstNonEmpty(sess.Name, "this cluster"))
	}
	return clusterSettings{
		Role: "master", Name: sess.Name, Model: sess.Model,
		DisplayName: sess.ModelDisplayName, ServiceType: sess.ServiceType,
	}, nil
}

// prepareCluster is replaced in tests. Production joins the tailnet when the
// key is a multi-node master key or a join- code, then rewrites the engine spec.
var prepareCluster = prepareClusterReal

func decideCluster(key, mode string, lookupErr error) string {
	if strings.HasPrefix(credential(key), "join-") {
		return "worker"
	}
	if lookupErr != nil || mode != "multi" {
		return "single"
	}
	return "master"
}

func prepareClusterReal(cfg *config.Config, spec *engine.Spec) error {
	key := credential(cfg.Provider.APIKey)
	cfg.Provider.APIKey = key
	worker := strings.HasPrefix(key, "join-")
	var login, auth, token, role string
	if worker {
		res, err := inferscale.Exchange(cfg.Provider.URL, key)
		if err != nil {
			return err
		}
		login, auth, token = res.LoginServer, res.AuthKey, res.NodeToken
		role = "worker"
	} else {
		sess, err := inferscale.FetchSession(cfg.Provider.URL, key)
		role = decideCluster(key, sess.Mode, err)
		if role == "single" {
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not read cluster mode (%v); continuing as a single node\n", err)
			}
			return nil
		}
		login = sess.LoginServer
		if cfg.Provider.NodeToken == "" {
			res, err := inferscale.Enroll(cfg.Provider.URL, key)
			if err != nil {
				return err
			}
			login, auth, token = res.LoginServer, res.AuthKey, res.NodeToken
		}
		token = firstNonEmpty(token, cfg.Provider.NodeToken)
	}
	cfg.Provider.ClusterMode = role
	if token != "" {
		cfg.Provider.NodeToken = token
	}
	ip, nodeID, err := inferscale.BringUp(context.Background(), login, auth)
	if err != nil && !worker && auth == "" {
		// The previous attempt saved a node token before inferscale up ran.
		res, enrollErr := inferscale.Enroll(cfg.Provider.URL, key)
		if enrollErr != nil {
			return err
		}
		login, auth, token = res.LoginServer, res.AuthKey, res.NodeToken
		if token != "" {
			cfg.Provider.NodeToken = token
		}
		ip, nodeID, err = inferscale.BringUp(context.Background(), login, auth)
	}
	if err != nil {
		return err
	}
	bearer := key
	if worker {
		bearer = cfg.Provider.NodeToken
	}
	if err := inferscale.ReportIP(cfg.Provider.URL, bearer, worker, ip, nodeID); err != nil {
		return err
	}
	remote, err := inferscale.FetchEngine(cfg.Provider.URL, bearer, worker)
	if err != nil {
		return err
	}
	next := inferscale.Apply(*spec, remote)
	*spec = next
	cfg.Provider.EngineArgs = next.Args
	cfg.Provider.EngineEnv = next.Env
	cfg.Provider.LLMURL = next.URL
	return nil
}

// loginServer is the URL the daemon dials. The engine route has it once a
// tailnet address is stored; otherwise the master session does.
func loginServer(cfg *config.Config, worker bool) string {
	bearer := cfg.Provider.APIKey
	if worker {
		bearer = cfg.Provider.NodeToken
	}
	if remote, err := inferscale.FetchEngine(cfg.Provider.URL, bearer, worker); err == nil && remote.LoginServer != "" {
		return remote.LoginServer
	}
	if worker {
		return ""
	}
	sess, err := inferscale.FetchSession(cfg.Provider.URL, cfg.Provider.APIKey)
	if err != nil {
		return ""
	}
	return sess.LoginServer
}

// refreshClusterEngine reloads the server command before a later start.
// It also reconnects inferscale when the saved address is offline.
func refreshClusterEngine(ctx context.Context, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	mode := cfg.Provider.ClusterMode
	if mode != "multi" && mode != "worker" && mode != "master" {
		return nil
	}
	worker := mode == "worker"
	bearer := cfg.Provider.APIKey
	if worker {
		bearer = cfg.Provider.NodeToken
	}
	ip, nodeID, err := inferscale.BringUp(ctx, loginServer(cfg, worker), "")
	if err != nil {
		return err
	}
	if err := inferscale.ReportIP(cfg.Provider.URL, bearer, worker, ip, nodeID); err != nil {
		return err
	}
	remote, err := inferscale.FetchEngine(cfg.Provider.URL, bearer, worker)
	if err != nil {
		return err
	}
	kind, ok := engine.ParseKind(cfg.Provider.Engine)
	if !ok {
		return fmt.Errorf("unknown engine %q", cfg.Provider.Engine)
	}
	spec := engine.ServeSpec(kind, firstNonEmpty(cfg.Provider.EngineBin, ""), cfg.Provider.Model, firstNonEmpty(cfg.Provider.HFRepo, cfg.Provider.Model), engine.ServeOpts{
		ToolCallParser:     cfg.Provider.ToolCallParser,
		MaxModelLen:        cfg.Provider.MaxModelLen,
		RopeType:           cfg.Provider.RopeType,
		RopeBaseContextLen: cfg.Provider.RopeBaseContextLen,
		GGUFFile:           cfg.Provider.GGUFFile,
	})
	next := inferscale.Apply(spec, remote)
	cfg.Provider.EngineArgs = next.Args
	cfg.Provider.EngineEnv = next.Env
	cfg.Provider.LLMURL = next.URL
	return nil
}
