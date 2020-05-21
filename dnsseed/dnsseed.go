package dnsseed

import (
	"context"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	"github.com/zcashfoundation/dnsseeder/zcash"
)

// ZcashSeeder discovers IP addresses by asking Zcash peers for them.
type ZcashSeeder struct {
	Next   plugin.Handler
	Zones  []string
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
	// Check if it's a question for us
	state := request.Request{W: w, Req: r}
	zone := plugin.Zones(zs.Zones).Matches(state.Name())
	if zone == "" {
		return plugin.NextOrFailure(zs.Name(), zs.Next, ctx, w, r)
	}

	peerIPs := zs.seeder.Addresses(25)

	a := new(dns.Msg)
	a.SetReply(r)
	a.Authoritative = true

	a.Extra = make([]dns.RR, 0, len(peerIPs))

	for i := 0; i < len(peerIPs); i++ {
		var rr dns.RR

		ip := peerIPs[i]

		switch state.QType() {
		case dns.TypeA:
			rr = new(dns.A)
			rr.(*dns.A).Hdr = dns.RR_Header{Name: state.QName(), Rrtype: dns.TypeA, Class: state.QClass()}
			rr.(*dns.A).A = ip.To4()
		case dns.TypeAAAA:
			if ip.To4() != nil {
				rr = new(dns.AAAA)
				rr.(*dns.AAAA).Hdr = dns.RR_Header{Name: state.QName(), Rrtype: dns.TypeAAAA, Class: state.QClass()}
				rr.(*dns.AAAA).AAAA = ip
			}
		default:
			return dns.RcodeNotImplemented, nil
		}

		// TODO: why don't we offer SRV records? Zcash has a configurable port.

		a.Extra = append(a.Extra, rr)
	}

	w.WriteMsg(a)
	return dns.RcodeSuccess, nil
}
