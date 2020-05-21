## Zcash Network Crawler

This is a CoreDNS plugin that scrapes addresses of peers from a Zcash network. It's intended as a safer and more scalable replacement for the [zcash-seeder](https://github.com/zcash/zcash-seeder) project.

It's written in Go and uses [btcsuite](https://github.com/btcsuite) for low-level networking.

### Build instructions

TODO

### CoreDNS configuration

A sample Corefile that configures `mainnet.seeder.yolo.money` and `testnet.seeder.yolo.money` using two local Zcash nodes:

```
mainnet.seeder.yolo.money {
    dnsseed yolo.money mainnet 127.0.0.1:8233
}

testnet.seeder.yolo.money {
    dnsseed yolo.money testnet 127.0.0.1:18233
}
```
