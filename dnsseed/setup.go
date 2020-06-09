package dnsseed

import (
	crypto_rand "crypto/rand"
	"math"
	"net"
	"net/url"
	"strconv"
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
	log                          = clog.NewWithPlugin(pluginName)
	defaultUpdateInterval        = 15 * time.Minute
	defaultTTL            uint32 = 3600
)

func init() { plugin.Register(pluginName, setup) }

type options struct {
	networkName    string
	networkMagic   network.Network
	updateInterval time.Duration
	recordTTL      uint32
	bootstrapPeers []string
}

// setup is the function that gets called when the config parser see the token(pluginName. Setup is responsible
// for parsing any extra options the example plugin may have. The first token this function sees is(pluginName.
func setup(c *caddy.Controller) error {
	// Automatically configure responsive zone
	zone, err := url.Parse(c.Key)
	if err != nil {
		return c.Errf("couldn't parse zone from block identifer: %s", c.Key)
	}

	opts, err := parseConfig(c)
	if err != nil {
		return err
	}

	if len(opts.bootstrapPeers) == 0 {
		// TODO alternatively, load from storage if we already know some peers
		return plugin.Error(pluginName, c.Err("config supplied no bootstrap peers"))
	}

	// TODO If we wanted to register Prometheus metrics, this would be the place.

	seeder, err := zcash.NewSeeder(opts.networkMagic)
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	log.Infof("Getting addresses from bootstrap peers %v", opts.bootstrapPeers)

	for _, s := range opts.bootstrapPeers {
		address, port, err := net.SplitHostPort(s)
		if err != nil {
			return plugin.Error(pluginName, c.Errf("config error: expected 'host:port', got %s", s))
		}

		// Connect to the bootstrap peer
		_, err = seeder.Connect(address, port)
		if err != nil {
			return plugin.Error(pluginName, c.Errf("error connecting to %s:%s: %v", address, port, err))
		}
	}

	// Send the initial request for more addresses; spawns goroutines to process the responses.
	// Ready() will flip to true once we've received and confirmed at least 10 peers.
	go func() {
		runCrawl(opts.networkName, seeder)
		log.Infof("Starting update timer on %s. Will crawl every %.1f minutes.", opts.networkName, opts.updateInterval.Minutes())
		randByte := []byte{0}
		for {
			select {
			case <-time.After(opts.updateInterval):
				runCrawl(opts.networkName, seeder)
				crypto_rand.Read(randByte[:])
				if randByte[0] >= byte(192) {
					// About 25% of the time, retry the blacklist.
					// This stops us from losing peers forever due to
					// temporary downtime.
					seeder.RetryBlacklist()
				}
			}
		}
	}()

	err = seeder.WaitForAddresses(1, 30*time.Second)
	if err != nil {
		return plugin.Error(pluginName, c.Err("went 30 second without learning a single address"))
	}

	// Add the Plugin to CoreDNS, so Servers can use it in their plugin chain.
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		return ZcashSeeder{
			Next:   next,
			Zones:  []string{zone.Hostname()},
			seeder: seeder,
			opts:   opts,
		}
	})

	// All OK, return a nil error.
	return nil
}

func parseConfig(c *caddy.Controller) (*options, error) {
	opts := &options{
		updateInterval: defaultUpdateInterval,
		recordTTL:      defaultTTL,
	}
	c.Next() // skip the string "dnsseed"

	if !c.NextBlock() {
		return nil, plugin.Error(pluginName, c.SyntaxErr("expected config block"))
	}

	for loaded := true; loaded; loaded = c.NextBlock() {
		switch c.Val() {
		case "network":
			if !c.NextArg() {
				return nil, plugin.Error(pluginName, c.SyntaxErr("no network specified"))
			}
			switch c.Val() {
			case "mainnet":
				opts.networkName = "mainnet"
				opts.networkMagic = network.Mainnet
			case "testnet":
				opts.networkName = "testnet"
				opts.networkMagic = network.Testnet
			default:
				return nil, plugin.Error(pluginName, c.SyntaxErr("networks are {mainnet, testnet}"))
			}
		case "crawl_interval":
			if !c.NextArg() {
				return nil, plugin.Error(pluginName, c.SyntaxErr("no crawl interval specified"))
			}
			interval, err := time.ParseDuration(c.Val())
			if err != nil || interval == 0 {
				return nil, plugin.Error(pluginName, c.SyntaxErr("bad crawl_interval duration"))
			}
			opts.updateInterval = interval
		case "bootstrap_peers":
			bootstrap := c.RemainingArgs()
			if len(bootstrap) == 0 {
				return nil, plugin.Error(pluginName, c.SyntaxErr("no bootstrap peers specified"))
			}
			opts.bootstrapPeers = bootstrap
		case "record_ttl":
			if !c.NextArg() {
				return nil, plugin.Error(pluginName, c.SyntaxErr("no ttl specified"))
			}
			ttl, err := strconv.Atoi(c.Val())
			if err != nil || ttl <= 0 || ttl > math.MaxUint32 {
				return nil, plugin.Error(pluginName, c.SyntaxErr("bad ttl"))
			}
			opts.recordTTL = uint32(ttl)
		default:
			return nil, plugin.Error(pluginName, c.SyntaxErr("unsupported option"))
		}
	}
	return opts, nil
}

func runCrawl(name string, seeder *zcash.Seeder) {
	start := time.Now()

	// Make sure our addresses are still live and leave the connections open
	// (true would disconnect immediately).
	seeder.RefreshAddresses(false)

	// Request addresses from everyone we're connected to, synchronously. This
	// will block a while in an attempt to catch all of the addr responses it
	// can.
	newPeerCount := seeder.RequestAddresses()

	// Disconnect and leave everyone alone for a while.
	seeder.DisconnectAllPeers()

	elapsed := time.Now().Sub(start).Truncate(time.Second).Seconds()
	log.Infof(
		"[%s] %s crawl complete, met %d new peers of %d in %.0f seconds",
		time.Now().Format("2006/01/02 15:04:05"),
		name,
		newPeerCount,
		seeder.GetPeerCount(),
		elapsed,
	)
}
