package health

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/internal/config"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/llm"
	"github.com/sentnl/inferoute-node/inferoute-client/pkg/verify"
)

func TestAdvertiseModelsKeepsFirstFiveApproved(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i := 1; i <= 7; i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"alias":"m%d","is_active":true}`, i)
	}
	b.WriteString(`]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()

	cat := verify.NewCatalog(srv.URL, "vllm")
	if err := cat.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := NewReporter(&config.Config{}, nil, nil)
	r.SetVerifier(verify.NewVerifier(cat, nil, "vllm", "", ""))

	models := make([]llm.Model, 0, 8)
	models = append(models, llm.Model{ID: "not-approved"})
	for i := 1; i <= 7; i++ {
		models = append(models, llm.Model{ID: fmt.Sprintf("m%d", i)})
	}
	got := r.advertiseModels(models)
	if len(got) != 5 || got[0].ID != "m1" || got[4].ID != "m5" {
		t.Fatalf("got %#v", got)
	}
}
