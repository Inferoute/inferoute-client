package inferscale

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type enrollResult struct {
	LoginServer string `json:"login_server"`
	AuthKey     string `json:"auth_key"`
	Role        string `json:"role"`
	NodeToken   string `json:"node_token"`
	Network     string `json:"network"`
}

type sessionResult struct {
	Mode             string `json:"cluster_mode"`
	Role             string `json:"role"`
	Name             string `json:"name"`
	Model            string `json:"model"`
	ModelDisplayName string `json:"model_display_name"`
	ServiceType      string `json:"service_type"`
	LoginServer      string `json:"login_server"`
}

func FetchSession(base, apiKey string) (sessionResult, error) {
	var out sessionResult
	err := doJSON(base, http.MethodGet, "/api/provider/network/session", apiKey, nil, &out)
	return out, err
}

func Enroll(base, apiKey string) (enrollResult, error) {
	var out enrollResult
	err := doJSON(base, http.MethodPost, "/api/provider/network/enroll", apiKey, map[string]string{}, &out)
	return out, err
}

func PreviewJoin(base, code string) (sessionResult, error) {
	var out sessionResult
	err := doJSON(base, http.MethodPost, "/api/provider/network/join/preview", "", map[string]string{"code": code}, &out)
	return out, err
}

func Exchange(base, code string) (enrollResult, error) {
	var out enrollResult
	err := doJSON(base, http.MethodPost, "/api/provider/network/join", "", map[string]string{"code": code}, &out)
	return out, err
}

func ReportIP(base, token string, worker bool, ip, nodeID string) error {
	path := "/api/provider/network/ip"
	if worker {
		path = "/api/provider/network/node/ip"
	}
	return doJSON(base, http.MethodPost, path, token, map[string]string{
		"tailnet_ip":        ip,
		"headscale_node_id": nodeID,
	}, nil)
}

func FetchEngine(base, token string, worker bool) (Remote, error) {
	path := "/api/provider/network/engine"
	if worker {
		path = "/api/provider/network/node/engine"
	}
	var out Remote
	err := doJSON(base, http.MethodGet, path, token, nil, &out)
	return out, err
}

func doJSON(base, method, path, token string, body any, dest any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("%s %s: %s", method, path, strings.TrimSpace(string(msg)))
	}
	if dest == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(dest)
}
