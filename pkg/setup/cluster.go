package setup

import (
	"context"
	"fmt"
	"os"
	"strings"

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

func onlinePeers() int {
	_, n := inferscale.Tailnet()
	return n
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
	if cfg.EnrolledWorker() {
		model := strings.TrimSpace(cfg.Provider.Model)
		if model == "" {
			return clusterSettings{}, fmt.Errorf("this machine is in a cluster but has no saved model. Re-run setup with a new join code")
		}
		return clusterSettings{
			Role: "worker", Model: model, ServiceType: firstNonEmpty(cfg.Provider.Engine, "vllm"),
		}, nil
	}
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

// NodeRemoved is the node API rejecting a token after the owner removed the machine.
func NodeRemoved(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "invalid node token")
}

func removedErr(err error) error {
	return fmt.Errorf("the owner removed this machine from the cluster. Re-run setup and paste a new join code: %w", err)
}

func dropIfRemoved(cfg *config.Config, err error) error {
	if !NodeRemoved(err) {
		return err
	}
	cfg.Provider.NodeToken = ""
	cfg.Provider.ClusterMode = ""
	return removedErr(err)
}

func resumeWorker(cfg *config.Config, spec *engine.Spec) error {
	token := cfg.Provider.NodeToken
	remote, err := inferscale.FetchEngine(cfg.Provider.URL, token, true)
	if err != nil {
		return dropIfRemoved(cfg, err)
	}
	ip, nodeID, err := inferscale.BringUp(context.Background(), remote.LoginServer, "")
	if err != nil {
		return err
	}
	if err := inferscale.ReportIP(cfg.Provider.URL, token, true, ip, nodeID); err != nil {
		return dropIfRemoved(cfg, err)
	}
	next := inferscale.Apply(*spec, remote)
	*spec = next
	cfg.Provider.EngineArgs = next.Args
	cfg.Provider.EngineEnv = next.Env
	cfg.Provider.LLMURL = next.URL
	cfg.Provider.ClusterMode = "worker"
	return nil
}

func prepareClusterReal(cfg *config.Config, spec *engine.Spec) error {
	key := credential(cfg.Provider.APIKey)
	cfg.Provider.APIKey = key
	if cfg.EnrolledWorker() {
		return resumeWorker(cfg, spec)
	}
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

// refreshCluster is replaced in tests.
var refreshCluster = refreshClusterEngine

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
		if _, err := inferscale.FetchEngine(cfg.Provider.URL, bearer, true); NodeRemoved(err) {
			return removedErr(err)
		}
	}
	ip, nodeID, err := inferscale.BringUp(ctx, loginServer(cfg, worker), "")
	if err != nil {
		return err
	}
	if err := inferscale.ReportIP(cfg.Provider.URL, bearer, worker, ip, nodeID); err != nil {
		if NodeRemoved(err) {
			return removedErr(err)
		}
		return err
	}
	remote, err := inferscale.FetchEngine(cfg.Provider.URL, bearer, worker)
	if err != nil {
		if NodeRemoved(err) {
			return removedErr(err)
		}
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
