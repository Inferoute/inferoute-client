package nodeapi

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sentnl/inferoute-node/inferoute-client/pkg/version"
)

func TestRoundTripSetsVersionHeaders(t *testing.T) {
	prev := version.Version
	version.Version = "1.2.3"
	t.Cleanup(func() { version.Version = prev })

	var gotUA, gotVer string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotVer = r.Header.Get(HeaderClientVersion)
		w.Header().Set(HeaderLatestClientVersion, "1.2.3")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	resp, err := NewHTTPClient(2 * time.Second).Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if gotVer != "1.2.3" {
		t.Fatalf("version header = %q", gotVer)
	}
	if gotUA == "" || gotUA[:len("inferoute-client/1.2.3")] != "inferoute-client/1.2.3" {
		t.Fatalf("user agent = %q", gotUA)
	}
	if State().Status != UpdateCurrent {
		t.Fatalf("status = %s", State().Status)
	}
}

func TestLatestHeaderMarksAvailable(t *testing.T) {
	prev := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() {
		version.Version = prev
		NoteLatest("")
	})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(HeaderLatestClientVersion, "1.4.0")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	resp, err := NewHTTPClient(2 * time.Second).Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	snap := State()
	if snap.Status != UpdateAvailable || snap.Latest != "1.4.0" {
		t.Fatalf("state = %+v", snap)
	}
}

func TestUpgradeRequiredRestoresBody(t *testing.T) {
	prev := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = prev })

	const body = `{"error":"client_outdated","min_version":"1.2.0","latest_version":"1.4.0","download_url":"https://example.test/dl"}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUpgradeRequired)
		_, _ = w.Write([]byte(body))
	}))
	defer ts.Close()

	resp, err := NewHTTPClient(2 * time.Second).Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	up := ParseUpgrade(resp.StatusCode, raw)
	if up == nil || up.Min != "1.2.0" || up.Latest != "1.4.0" {
		t.Fatalf("upgrade = %+v body=%s", up, raw)
	}
	if State().Status != UpdateRequired {
		t.Fatalf("status = %s", State().Status)
	}
	if !errors.Is(up, up) {
		t.Fatal("typed error should be comparable")
	}
	if HealthInterval(up, time.Minute) != UpgradeRetryInterval {
		t.Fatal("426 should slow the health loop")
	}
	if HealthInterval(nil, time.Minute) != time.Minute {
		t.Fatal("success should use the normal interval")
	}
}

func TestNewer(t *testing.T) {
	if !Newer("1.4.0", "1.2.3") || !Newer("v2.0.0", "1.9.9") {
		t.Fatal("expected newer")
	}
	if Newer("1.2.3", "1.2.3") || Newer("dev", "1.0.0") || Newer("1.0.0", "dev") {
		t.Fatal("expected not newer")
	}
}
