package setup

import (
	"errors"
	"testing"
)

func TestDecideCluster(t *testing.T) {
	if got := decideCluster("join-abc", "single", nil); got != "worker" {
		t.Fatalf("join = %s", got)
	}
	if got := decideCluster("sk-key", "multi", nil); got != "master" {
		t.Fatalf("multi = %s", got)
	}
	if got := decideCluster("sk-key", "single", nil); got != "single" {
		t.Fatalf("single = %s", got)
	}
	if got := decideCluster("sk-key", "", errors.New("down")); got != "single" {
		t.Fatalf("lookup error = %s", got)
	}
}
