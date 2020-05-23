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
	zone := fmt.Sprintf("seeder.%s.%s.", networkArg, rootArg)

	address, port, err := net.SplitHostPort(hostArg)
	if err != nil {
		return plugin.Error("dnsseed", c.Errf("Config error: expected 'host:port', got %s", hostArg))
	}

	// XXX: If we wanted to register Prometheus metrics, this would be the place.

	seeder, err := zcash.NewSeeder(magic)
	if err != nil {
		return plugin.Error("dnsseed", err)
	}

	// TODO load from storage if we already know some peers

	// Send the initial request for more addresses; spawns goroutines to process the responses.
	// Ready() will flip to true once we've received and confirmed at least 10 peers.

	log.Infof("Getting addresses from bootstrap peer %s:%s", address, port)

	// Connect to the bootstrap peer
	err = seeder.Connect(address, port)
	if err != nil {
		return plugin.Error("dnsseed", err)
	}

	seeder.RequestAddresses()
	seeder.DisconnectAllPeers()

	// Start the update timer
	go func() {
		log.Infof("Starting update timer. Will crawl every %.0f minutes.", updateInterval.Minutes())
		for {
			select {
			case <-time.After(updateInterval):
				runCrawl(seeder)
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

func runCrawl(seeder *zcash.Seeder) {
	log.Infof("[%s] Beginning crawl", time.Now().Format("2006/01/02 15:04:05"))
	start := time.Now()

	// Slow motion crawl: we'll get them all eventually!

	// 1. Make sure our addresses are still live and leave the
	// connections open (true would disconnect immediately).
	seeder.RefreshAddresses(false)

	// 2. Request addresses from everyone we're connected to,
	// synchronously. This will block a while in an attempt
	// to catch all of the addr responses it can.
	newPeerCount := seeder.RequestAddresses()

	// 3. Disconnect from everyone & leave them alone for a while
	seeder.DisconnectAllPeers()

	elapsed := time.Now().Sub(start).Truncate(time.Second).Seconds()
	log.Infof(
		"[%s] Crawl complete, met %d new peers of %d in %.2f seconds",
		time.Now().Format("2006/01/02 15:04:05"),
		newPeerCount,
		seeder.GetPeerCount(),
		elapsed,
	)
}
