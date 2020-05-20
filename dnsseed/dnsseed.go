package dnsseed

import (
	"context"

	"github.com/coredns/coredns/plugin"
	"github.com/miekg/dns"
	"github.com/zcashfoundation/dnsseeder/zcash"
)

// ZcashSeeder discovers IP addresses by asking Zcash peers for them.
type ZcashSeeder struct {
	Next plugin.Handler

	seeder *zcash.Seeder
}

// Name satisfies the Handler interface.
func (zs ZcashSeeder) Name() string { return "dnsseed" }

// Ready implements the ready.Readiness interface, once this flips to true CoreDNS
// assumes this plugin is ready for queries; it is not checked again.
func (zs ZcashSeeder) Ready() bool {
	// setup() has attempted an initial connection to the backing peer already.
	return zs.seeder.Ready()
}

func (zs ZcashSeeder) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	return plugin.NextOrFailure(zs.Name(), zs.Next, ctx, w, r)
}
