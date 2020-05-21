package dnsseed

import (
	"fmt"
	"net"
	"time"

	"github.com/caddyserver/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	"github.com/zcashfoundation/dnsseeder/zcash"
	"github.com/zcashfoundation/dnsseeder/zcash/network"
)

var (
	log            = clog.NewWithPlugin("dnsseed")
	updateInterval = 15 * time.Minute
)

func init() { plugin.Register("dnsseed", setup) }

// setup is the function that gets called when the config parser see the token "dnsseed". Setup is responsible
// for parsing any extra options the example plugin may have. The first token this function sees is "dnsseed".
func setup(c *caddy.Controller) error {
	var rootArg, networkArg, hostArg string

	c.Next() // Ignore "dnsseed" and give us the next token.

	if !c.Args(&rootArg, &networkArg, &hostArg) {
		return plugin.Error("dnsseed", c.ArgErr())
	}

	var magic network.Network
	switch networkArg {
	case "mainnet":
		magic = network.Mainnet
	case "testnet":
		magic = network.Testnet
	default:
		return plugin.Error("dnsseed", c.Errf("Config error: expected {mainnet, testnet}, got %s", networkArg))
	}

	// Automatically configure the responsive zone by network
	zone := fmt.Sprintf("%s.seeder.%s.", networkArg, rootArg)

	address, port, err := net.SplitHostPort(hostArg)
	if err != nil {
		return plugin.Error("dnsseed", c.Errf("Config error: expected 'host:port', got %s", hostArg))
	}

	// XXX: If we wanted to register Prometheus metrics, this would be the place.

	seeder, err := zcash.NewSeeder(magic)
	if err != nil {
		return plugin.Error("dnsseed", err)
	}

	// Connect to the bootstrap peer
	err = seeder.Connect(address, port)
	if err != nil {
		return plugin.Error("dnsseed", err)
	}

	// Send the initial request for more addresses; spawns goroutines to process the responses.
	// Ready() will flip to true once we've received and confirmed at least 10 peers.
	seeder.RequestAddresses()

	go func() {
		for {
			select {
			case <-time.After(updateInterval):
				seeder.RequestAddresses()
				err := seeder.WaitForAddresses(10, 30*time.Second)
				if err != nil {
					log.Errorf("Failed to refresh addresses: %v", err)
				}
				// XXX: If we wanted to crawl independently, this would be the place.
			}
		}
	}()

	// Add the Plugin to CoreDNS, so Servers can use it in their plugin chain.
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		return ZcashSeeder{
			Next:   next,
			Zones:  []string{zone},
			seeder: seeder,
		}
	})

	// All OK, return a nil error.
	return nil
}
