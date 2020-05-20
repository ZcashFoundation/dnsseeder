package dnsseed

import (
	"strings"
	"testing"

	"github.com/caddyserver/caddy"
)

// TestSetup tests the various things that should be parsed by setup.
func TestSetup(t *testing.T) {
	c := caddy.NewTestController("dns", "dnsseed mainnet 127.0.0.1:8233")
	if err := setup(c); err != nil {
		if strings.Contains(err.Error(), "connection refused") {
			// No local peer running
			// TODO: mock a local peer, which will be easier with an API rework in the zcash package
			t.Skip()
		}
		t.Fatalf("Expected no errors, but got: %v", err)
	}

	c = caddy.NewTestController("dns", "dnsseed boop snoot")
	if err := setup(c); err == nil {
		t.Fatalf("Expected errors, but got: %v", err)
	}
}
