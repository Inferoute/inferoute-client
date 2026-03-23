package geoloc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookup(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ipapiResponse{
			IP:          "203.0.113.1",
			City:        "London",
			Region:      "England",
			CountryName: "United Kingdom",
		})
	}))
	defer ts.Close()

	t.Setenv("INFEROUTE_GEO_LOOKUP_URL", ts.URL)

	ip, loc, err := Lookup(context.Background(), Client())
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.1" {
		t.Fatalf("ip: got %q", ip)
	}
	if loc != "London, England, United Kingdom" {
		t.Fatalf("loc: got %q", loc)
	}
}
