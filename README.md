# Zcash DNS seeder

This is a CoreDNS plugin that scrapes addresses of peers from a Zcash network
and returns them on A or AAAA queries. It's intended as a safer, more configurable, and more scalable
replacement for the [zcash-seeder](https://github.com/zcash/zcash-seeder) project.

It's written in Go and uses [btcsuite](https://github.com/btcsuite) for low-level networking.

It works by connecting to a list of bootstrap peers and requesting new peers by sending `getaddr`
messages. The process is then periodically repeated with the current list of known peers.
This plugin accepts both `addr` and `addrv2` messages, though it ignores addresses that are not
IPv4 nor IPv6. It does not answer `getaddr` requests from other peers.

## Build instructions

This code cannot be used independently of CoreDNS. See [coredns-zcash](https://github.com/ZcashFoundation/coredns-zcash) for instructions.

## CoreDNS configuration

A sample Corefile that configures seeders on a domain for each network, using two local Zcash nodes for bootstrap:

```
mainnet.seeder.example.com {
    dnsseed {
        network mainnet
        bootstrap_peers 127.0.0.1:8233
        crawl_interval 30m
        record_ttl 600
    }
}

testnet.seeder.example.com {
    dnsseed {
        network testnet
        bootstrap_peers 127.0.0.1:18233
        crawl_interval 15m
        record_ttl 300
    }
}

# Returns 200 OK on .:8080/health
. {
    health :8080
}
```

## License

The seeder is dual-licensed under the terms of both the MIT license and the Apache License (Version 2.0).

See LICENSE-APACHE and LICENSE-MIT.
