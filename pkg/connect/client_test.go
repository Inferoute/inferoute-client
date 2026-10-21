package connect

import "testing"

func TestDialURL(t *testing.T) {
	got, err := DialURL("https://core.inferoute.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://core.inferoute.com/api/connect/ws" {
		t.Fatalf("url %s", got)
	}
	got, err = DialURL("http://localhost:80")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ws://localhost:80/api/connect/ws" {
		t.Fatalf("url %s", got)
	}
}
