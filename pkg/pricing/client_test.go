package pricing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/logger"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	logger.SetDefaultLogger(&logger.Logger{Logger: zap.NewNop()})
	os.Exit(m.Run())
}

func TestGetModelPrices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"model_prices":[{"model_name":"m","avg_input_price":1.5,"avg_output_price":2.5}]}`))
	}))
	defer ts.Close()

	resp, err := NewClient(ts.URL, "k").GetModelPrices(context.Background(), []string{"m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ModelPrices) != 1 || resp.ModelPrices[0].AvgInputPrice != 1.5 {
		t.Fatalf("unexpected prices: %+v", resp.ModelPrices)
	}
}

func TestRegisterModel(t *testing.T) {
	t.Run("success returns nil", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		if err := NewClient(ts.URL, "k").RegisterModel(context.Background(), "m", "ollama", 1, 2); err != nil {
			t.Fatalf("expected success, got %v", err)
		}
	})

	t.Run("400 already exists maps to ErrModelAlreadyExists", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"model already exists for provider"}`))
		}))
		defer ts.Close()

		err := NewClient(ts.URL, "k").RegisterModel(context.Background(), "m", "ollama", 1, 2)
		if !errors.Is(err, ErrModelAlreadyExists) {
			t.Fatalf("error = %v, want ErrModelAlreadyExists", err)
		}
	})

	t.Run("other 4xx returns structured error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"forbidden"}`))
		}))
		defer ts.Close()

		err := NewClient(ts.URL, "k").RegisterModel(context.Background(), "m", "ollama", 1, 2)
		if err == nil || errors.Is(err, ErrModelAlreadyExists) {
			t.Fatalf("error = %v, want a non-already-exists error", err)
		}
		var errResp *ErrorResponse
		if !errors.As(err, &errResp) || errResp.StatusCode != http.StatusForbidden {
			t.Fatalf("error = %v, want *ErrorResponse with status 403", err)
		}
	})
}
