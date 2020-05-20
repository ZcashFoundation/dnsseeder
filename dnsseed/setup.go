package dnsseed

import (
	"net"

	"github.com/caddyserver/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	"github.com/zcashfoundation/dnsseeder/zcash"
	"github.com/zcashfoundation/dnsseeder/zcash/network"
)

var log = clog.NewWithPlugin("dnsseed")

func init() { plugin.Register("dnsseed", setup) }

// setup is the function that gets called when the config parser see the token "dnsseed". Setup is responsible
// for parsing any extra options the example plugin may have. The first token this function sees is "dnsseed".
func setup(c *caddy.Controller) error {
	var networkArg, hostArg string

	c.Next() // Ignore "dnsseed" and give us the next token.

	if !c.Args(&networkArg, &hostArg) {
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

	address, port, err := net.SplitHostPort(hostArg)
	if err != nil {
		return plugin.Error("dnsseed", c.Errf("Config error: expected 'host:port', got %s", hostArg))
	}

	// XXX: If we wanted to register Prometheus metrics, this would be the place.

	seeder, err := zcash.NewSeeder(magic)
	if err != nil {
		return plugin.Error("dnsseed", err)
	}

	err = seeder.Connect(address, port)
	if err != nil {
		return plugin.Error("dnsseed", err)
	}

	// TODO: make initial addr request
	// TODO: begin update timer

	// Add the Plugin to CoreDNS, so Servers can use it in their plugin chain.
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		return ZcashSeeder{Next: next, seeder: seeder}
	})

	// All OK, return a nil error.
	return nil
}
