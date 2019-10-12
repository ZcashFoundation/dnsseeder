module github.com/gtank/coredns-zcash

go 1.12

require (
	github.com/btcsuite/btcd v0.0.0-20190926002857-ba530c4abb35
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/pkg/errors v0.8.1
)

replace github.com/btcsuite/btcd => github.com/gtank/btcd v0.0.0-20191012142736-b43c61a68604
