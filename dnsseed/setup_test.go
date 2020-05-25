package dnsseed

import (
	"testing"
	"time"

	"github.com/caddyserver/caddy"
	"github.com/zcashfoundation/dnsseeder/zcash/network"
)

// TestSetup tests the various things that should be parsed by setup.
func TestSetup(t *testing.T) {
	tt := []struct {
		config string
		isCorrect bool
		magic network.Network
		interval string
		bootstrap []string
	}{
		{`dnsseed`, false, 0, "0s", []string{}},
		{`dnsseed mainnet`, false, 0, "0s", []string{}},
		{`dnsseed { }`, false, 0, "0s", []string{}},
		{`dnsseed { network }`, false, 0, "0s", []string{}},
		{`dnsseed { network mainnet }`, false, network.Mainnet, "0s", []string{}},
		{`dnsseed { 
			network testnet
			crawl_interval 15s
			bootstrap_peers
			}`, true, network.Testnet, (time.Duration(15) * time.Second).String(), []string{},
		},
		{`dnsseed { 
			network testnet
			crawl_interval 15s
			bootstrap_peers 127.0.0.1:8233
			}`, true, network.Testnet, (time.Duration(15) * time.Second).String(), []string{"127.0.0.1:8233"},
		},
		{`dnsseed { 
			network mainnet
			crawl_interval 30m
			bootstrap_peers 127.0.0.1:8233 127.0.0.2:8233
			}`, true, network.Mainnet, (time.Duration(30) * time.Minute).String(), []string{"127.0.0.1:8233", "127.0.0.2:8233"},
		},
	}

	for _, test := range tt {
		c := caddy.NewTestController("dns", test.config)
		magic, interval, bootstrap, err := parseConfig(c)
		if err != nil && test.isCorrect {
			t.Errorf("Unexpected error in test case `%s`: %v", test.config, err)
		}
		if magic != test.magic || interval.String() != test.interval || len(test.bootstrap) != len(bootstrap) {
			t.Errorf("Input: %s Results: %v, %s, %s, %v", test.config, magic, interval, bootstrap, err)
		}
	}
}