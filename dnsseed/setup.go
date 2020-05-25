package dnsseed

import (
	"net"
	"net/url"
	"time"

	"github.com/caddyserver/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	"github.com/zcashfoundation/dnsseeder/zcash"
	"github.com/zcashfoundation/dnsseeder/zcash/network"
)

const pluginName = "dnsseed"

var (
	log            = clog.NewWithPlugin(pluginName)
	updateInterval = 15 * time.Minute
)

func init() { plugin.Register(pluginName, setup) }

// setup is the function that gets called when the config parser see the token(pluginName. Setup is responsible
// for parsing any extra options the example plugin may have. The first token this function sees is(pluginName.
func setup(c *caddy.Controller) error {
	// Automatically configure responsive zone
	zone, err := url.Parse(c.Key)
	if err != nil {
		return c.Errf("couldn't parse zone from block identifer: %s", c.Key)
	}

	magic, interval, bootstrap, err := parseConfig(c)
	if err != nil {
		return err
	}

	if interval != 0 {
		updateInterval = interval
	}

	// TODO If we wanted to register Prometheus metrics, this would be the place.

	seeder, err := zcash.NewSeeder(magic)
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	// TODO load from storage if we already know some peers

	log.Infof("Getting addresses from bootstrap peers %v", bootstrap)

	for _, s := range bootstrap {
		address, port, err := net.SplitHostPort(s)
		if err != nil {
			return plugin.Error(pluginName, c.Errf("config error: expected 'host:port', got %s", s))
		}

		// Connect to the bootstrap peer
		err = seeder.Connect(address, port)
		if err != nil {
			return plugin.Error(pluginName, c.Errf("error connecting to %s:%s: %v", address, port, err))
		}
	}

	// Send the initial request for more addresses; spawns goroutines to process the responses.
	// Ready() will flip to true once we've received and confirmed at least 10 peers.
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
			Zones:  []string{zone.Hostname()},
			seeder: seeder,
		}
	})

	// All OK, return a nil error.
	return nil
}

func parseConfig(c *caddy.Controller) (magic network.Network, interval time.Duration, bootstrap []string, err error) {
	c.Next() // skip the string "dnsseed"

	for c.NextBlock() {
		switch c.Val() {
		case "network":
			if !c.NextArg() {
				return 0, 0, nil, plugin.Error(pluginName, c.SyntaxErr("no network specified"))
			}
			switch c.Val() {
			case "mainnet":
				magic = network.Mainnet
			case "testnet":
				magic = network.Testnet
			default:
				return 0, 0, nil, plugin.Error(pluginName, c.SyntaxErr("networks are {mainnet, testnet}"))
			}
		case "crawl_interval":
			if !c.NextArg() {
				return 0, 0, nil, plugin.Error(pluginName, c.SyntaxErr("no crawl interval specified"))
			}
			interval, err = time.ParseDuration(c.Val())
			if err != nil || interval == 0 {
				return 0, 0, nil, plugin.Error(pluginName, c.SyntaxErr("bad crawl_interval duration"))
			}
		case "bootstrap_peers":
			bootstrap = c.RemainingArgs()
			if len(bootstrap) == 0 {
				plugin.Error(pluginName, c.SyntaxErr("no bootstrap peers specified"))
			}
		default:
			return 0, 0, nil, plugin.Error(pluginName, c.SyntaxErr("unsupported option"))
		}
	}
	return
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
