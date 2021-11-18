module github.com/zcashfoundation/dnsseeder

go 1.14

require (
	github.com/btcsuite/btcd v0.22.0-beta
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/caddyserver/caddy v1.0.5
	github.com/coredns/coredns v1.6.9
	github.com/miekg/dns v1.1.29
	github.com/pkg/errors v0.9.1
)

// Currently pointing to "main-zfnd" branch
replace github.com/btcsuite/btcd => github.com/ZcashFoundation/btcd v0.22.0-beta.0.20211118133831-ca5d3008dd64
