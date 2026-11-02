package inferscale

import (
	"fmt"
	"strconv"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
)

// Remote is the tailnet bind and the distributed flags from the server.
type Remote struct {
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	ExtraArgs   []string `json:"extra_args"`
	Env         []string `json:"env"`
	Command     string   `json:"command"`
	Role        string   `json:"role"`
	LoginServer string   `json:"login_server"`
}

// Apply merges the server command onto the local serve flags.
// Catalog flags already on spec stay. The host, port, env, and extra args change.
func Apply(spec engine.Spec, remote Remote) engine.Spec {
	if remote.Host != "" {
		spec.Args = setFlag(spec.Args, "--host", remote.Host)
	}
	if remote.Port > 0 {
		spec.Args = setFlag(spec.Args, "--port", strconv.Itoa(remote.Port))
		spec.URL = fmt.Sprintf("http://%s:%d", remote.Host, remote.Port)
	}
	if len(remote.ExtraArgs) > 0 {
		spec.Args = append(spec.Args, remote.ExtraArgs...)
	}
	if len(remote.Env) > 0 {
		spec.Env = append(append([]string{}, spec.Env...), remote.Env...)
	}
	return spec
}

func setFlag(args []string, name, value string) []string {
	out := append([]string(nil), args...)
	for i := 0; i < len(out)-1; i++ {
		if out[i] == name {
			out[i+1] = value
			return out
		}
	}
	return append(out, name, value)
}
