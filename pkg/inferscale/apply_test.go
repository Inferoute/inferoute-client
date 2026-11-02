package inferscale

import (
	"strings"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/engine"
)

func TestStatusAnsweredWithoutIP(t *testing.T) {
	if !statusAnswered([]byte(`{"BackendState":"NeedsLogin","Self":null}`)) {
		t.Fatal("logged-out daemon should count as up")
	}
	if statusAnswered(nil) || statusAnswered([]byte("Logged out.")) {
		t.Fatal("non-json must not count as up")
	}
}

func TestParseStatusOfflineKeepsAddress(t *testing.T) {
	st, err := parseStatus([]byte(`{"BackendState":"Running","Self":{"ID":"n1","Online":false,"TailscaleIPs":["198.18.0.1"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if st.Online || st.IP != "198.18.0.1" || st.ID != "n1" {
		t.Fatalf("state = %+v", st)
	}
	st, err = parseStatus([]byte(`{"BackendState":"Running","Self":{"ID":"n1","Online":true,"TailscaleIPs":["198.18.0.1"]}}`))
	if err != nil || !st.Online {
		t.Fatalf("online = %+v %v", st, err)
	}
}

func TestUpArgsReconnectsWithoutKey(t *testing.T) {
	if strings.Join(upArgs("http://127.0.0.1:8444", ""), " ") != "up --login-server http://127.0.0.1:8444" {
		t.Fatal(upArgs("http://127.0.0.1:8444", ""))
	}
	if strings.Join(upArgs("http://127.0.0.1:8444", "k"), " ") != "up --login-server http://127.0.0.1:8444 --auth-key k" {
		t.Fatal(upArgs("http://127.0.0.1:8444", "k"))
	}
}

func TestRootArgv(t *testing.T) {
	bin, argv := rootArgv(0, "/usr/bin/inferscale", "up", "--auth-key", "k")
	if bin != "/usr/bin/inferscale" || len(argv) != 3 || argv[0] != "up" {
		t.Fatalf("root argv = %s %v", bin, argv)
	}
	bin, argv = rootArgv(1000, "/home/ubuntu/.local/share/inferoute/inferscale/inferscaled")
	if bin != "sudo" || strings.Join(argv, " ") != "-n -- /home/ubuntu/.local/share/inferoute/inferscale/inferscaled" {
		t.Fatalf("user argv = %s %v", bin, argv)
	}
}

func TestApplyKeepsLoopbackOnEmptyRemote(t *testing.T) {
	spec := engine.ServeSpec(engine.KindVLLM, "vllm", "Qwen/Qwen2.5-7B-Instruct", "", engine.ServeOpts{})
	got := Apply(spec, Remote{})
	if got.CommandLine() != spec.CommandLine() {
		t.Fatalf("empty remote changed the command: %s", got.CommandLine())
	}
}

func TestApplyMasterFlags(t *testing.T) {
	spec := engine.ServeSpec(engine.KindVLLM, "vllm", "Qwen/Qwen2.5-7B-Instruct", "", engine.ServeOpts{ToolCallParser: "hermes"})
	got := Apply(spec, Remote{
		Host: "198.18.0.1",
		Port: 8000,
		ExtraArgs: []string{
			"--nnodes", "2", "--node-rank", "0", "--master-addr", "198.18.0.1", "--headless",
		},
		Env: []string{"VLLM_HOST_IP=198.18.0.1"},
	})
	line := got.CommandLine()
	if !strings.Contains(line, "--host 198.18.0.1") || strings.Contains(line, "127.0.0.1") {
		t.Fatalf("host not replaced: %s", line)
	}
	if !strings.Contains(line, "--tool-call-parser hermes") || !strings.Contains(line, "--nnodes 2") {
		t.Fatalf("flags = %s", line)
	}
	if len(got.Env) != 1 || got.URL != "http://198.18.0.1:8000" {
		t.Fatalf("env/url = %v %s", got.Env, got.URL)
	}
}

func TestApplyLlamaHasNoVLLMEnv(t *testing.T) {
	spec := engine.ServeSpec(engine.KindLlamaCpp, "llama-server", "Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-7B-Instruct-GGUF", engine.ServeOpts{GGUFFile: "q.gguf"})
	got := Apply(spec, Remote{Host: "198.18.0.2", Port: 8081})
	if len(got.Env) != 0 || !strings.Contains(got.CommandLine(), "--host 198.18.0.2") {
		t.Fatalf("llama = %s env %v", got.CommandLine(), got.Env)
	}
}
