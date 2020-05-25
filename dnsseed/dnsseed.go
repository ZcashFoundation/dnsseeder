package dnsseed

import (
	"context"
	"net"

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
	opts   *options
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

	var peerIPs []net.IP
	switch state.QType() {
	case dns.TypeA:
		peerIPs = zs.seeder.Addresses(25)
	case dns.TypeAAAA:
		peerIPs = zs.seeder.AddressesV6(25)
	default:
		return dns.RcodeNotImplemented, nil
	}

	a := new(dns.Msg)
	a.SetReply(r)
	a.Authoritative = true
	a.Answer = make([]dns.RR, 0, 25)

	for i := 0; i < len(peerIPs); i++ {
		var rr dns.RR

		if peerIPs[i].To4() == nil {
			rr = new(dns.AAAA)
			rr.(*dns.AAAA).Hdr = dns.RR_Header{Name: state.QName(), Rrtype: dns.TypeAAAA, Ttl: zs.opts.recordTTL, Class: state.QClass()}
			rr.(*dns.AAAA).AAAA = peerIPs[i]
		} else {
			rr = new(dns.A)
			rr.(*dns.A).Hdr = dns.RR_Header{Name: state.QName(), Rrtype: dns.TypeA, Ttl: zs.opts.recordTTL, Class: state.QClass()}
			rr.(*dns.A).A = peerIPs[i]
		}

		a.Answer = append(a.Answer, rr)
	}

	w.WriteMsg(a)
	return dns.RcodeSuccess, nil
}
