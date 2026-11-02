package engine

import (
	"bytes"
	"os"
	"strconv"
)

// processAlive is replaced in tests.
var processAlive = procHasWorker

// WorkerAlive reports whether this machine's cluster worker is up.
// vLLM workers are --headless and never open HTTP, so the signal is the process.
// shortcut: llama.cpp worker command is not defined yet; return false until inferoute-node emits rpc-server, then probe that port.
func WorkerAlive(kind Kind, args []string) bool {
	if kind == KindLlamaCpp {
		return false
	}
	if !Headless(args) {
		return false
	}
	return processAlive(args)
}

func procHasWorker(args []string) bool {
	needles := [][]byte{[]byte("--headless")}
	if addr := flagVal(args, "--master-addr"); addr != "" {
		needles = append(needles, []byte(addr))
	}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		raw, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || len(raw) == 0 {
			continue
		}
		ok := true
		for _, n := range needles {
			if !bytes.Contains(raw, n) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
