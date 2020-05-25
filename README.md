## Zcash Network Crawler

This is a CoreDNS plugin that scrapes addresses of peers from a Zcash network. It's intended as a safer and more scalable replacement for the [zcash-seeder](https://github.com/zcash/zcash-seeder) project.

It's written in Go and uses [btcsuite](https://github.com/btcsuite) for low-level networking.

### Build instructions

TODO

### CoreDNS configuration

A sample Corefile that configures seeders on a domain for each network, backed by two local Zcash nodes:

```
mainnet.seeder.yolo.money {
    dnsseed {
        network mainnet
        crawl_interval 30m
        bootstrap_peers 127.0.0.1:8233
    }
}

testnet.seeder.yolo.money {
    dnsseed {
        network testnet
        crawl_interval 30m
        bootstrap_peers 127.0.0.1:18233
    }
}
```
